package scrubber

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ScrubberEngine is the top-level orchestrator for line-rate XDP packet filtering,
// stateful IP reputation tracking, and challenge token verification.
type ScrubberEngine struct {
	mu           sync.RWMutex
	config       ScrubberConfig
	filter       *XDPFilter
	reputation   *IPReputationTracker
	driver       driverInterface
	policies     map[string]TenantScrubberPolicy
	startTime    time.Time
	stopChan     chan struct{}
	running      bool

	// Telemetry atomic counters
	inspectedPackets  int64
	passedPackets     int64
	droppedPackets    int64
	challengesIssued  int64
	challengesSolved  int64
	synFloodsMitigated int64
	udpFloodsMitigated int64
	l7FloodsMitigated int64
	badHdrDropped     int64
	totalLatencyUs    int64
}

type driverInterface interface {
	IsAvailable() bool
	GetMode() string
	AttachInterface(iface string) error
	DetachInterface(iface string) error
	InspectFastPath(pkt PacketMetadata) (Action, bool)
	Close() error
}

// NewScrubberEngine creates and initializes a unified DDoS scrubber engine.
func NewScrubberEngine(cfg ScrubberConfig) *ScrubberEngine {
	if len(cfg.HMACSecret) == 0 {
		cfg = DefaultScrubberConfig()
	}

	filter := NewXDPFilter()
	rep := NewIPReputationTracker(cfg.HMACSecret, cfg.ChallengeTTL, cfg.DefaultBanDuration)
	driver := NewPlatformScrubberDriver()

	eng := &ScrubberEngine{
		config:     cfg,
		filter:     filter,
		reputation: rep,
		driver:     driver,
		policies:   make(map[string]TenantScrubberPolicy),
		startTime:  time.Now(),
		stopChan:   make(chan struct{}),
	}

	// Register default policy
	eng.policies["default"] = cfg.DefaultPolicy

	return eng
}

// Start launches background ticker routines for IP reputation sweeps and ban expirations.
func (e *ScrubberEngine) Start(ctx context.Context) {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return
	}
	e.running = true
	e.mu.Unlock()

	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-e.stopChan:
				return
			case <-ticker.C:
				e.reputation.SweepExpired()
			}
		}
	}()
}

// Stop cleanly terminates background monitoring loops.
func (e *ScrubberEngine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.running {
		return
	}
	e.running = false
	close(e.stopChan)
	_ = e.driver.Close()
}

// SetTenantPolicy registers or updates mitigation bounds for a tenant.
func (e *ScrubberEngine) SetTenantPolicy(policy TenantScrubberPolicy) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if policy.TenantID == "" {
		policy.TenantID = "default"
	}
	e.policies[policy.TenantID] = policy
}

// GetTenantPolicy retrieves policy for a tenant with fallback to default.
func (e *ScrubberEngine) GetTenantPolicy(tenantID string) TenantScrubberPolicy {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if pol, exists := e.policies[tenantID]; exists {
		return pol
	}
	return e.config.DefaultPolicy
}

// ListTenantPolicies lists all registered tenant configurations.
func (e *ScrubberEngine) ListTenantPolicies() []TenantScrubberPolicy {
	e.mu.RLock()
	defer e.mu.RUnlock()
	list := make([]TenantScrubberPolicy, 0, len(e.policies))
	for _, p := range e.policies {
		list = append(list, p)
	}
	return list
}

// InspectPacket evaluates an L3/L4 packet through line-rate XDP filters and stateful IP reputation.
func (e *ScrubberEngine) InspectPacket(pkt PacketMetadata, tenantID string) ScrubDecision {
	start := time.Now()
	atomic.AddInt64(&e.inspectedPackets, 1)

	policy := e.GetTenantPolicy(tenantID)
	clientIP := pkt.SrcIP
	if clientIP == "" {
		clientIP = "0.0.0.0"
	}

	// 1. Check if client IP is currently quarantined in ban list
	if isBanned, reason, _ := e.reputation.IsBanned(clientIP); isBanned {
		latencyUs := time.Since(start).Microseconds()
		atomic.AddInt64(&e.droppedPackets, 1)
		atomic.AddInt64(&e.totalLatencyUs, latencyUs)
		return ScrubDecision{
			Action:          ActionDrop,
			AttackType:      AttackTypeNone,
			ClientIP:        clientIP,
			TenantID:        tenantID,
			ReputationScore: 0.0,
			ReputationLevel: ReputationBanned,
			LatencyUs:       latencyUs,
			Reason:          "IP quarantined: " + reason,
		}
	}

	// 2. Line-rate XDP Filter inspection (SYN Flood, UDP Amplification, Malformed Headers)
	act, attack, reason := e.filter.EvaluatePacket(pkt, policy)
	if act == ActionDrop {
		latencyUs := time.Since(start).Microseconds()
		atomic.AddInt64(&e.droppedPackets, 1)
		atomic.AddInt64(&e.totalLatencyUs, latencyUs)

		// Record penalty
		var penalty float64 = 25.0
		switch attack {
		case AttackTypeSYNFlood:
			penalty = 30.0
			atomic.AddInt64(&e.synFloodsMitigated, 1)
		case AttackTypeUDPAmplification:
			penalty = 35.0
			atomic.AddInt64(&e.udpFloodsMitigated, 1)
		case AttackTypeMalformedHeader:
			penalty = 20.0
			atomic.AddInt64(&e.badHdrDropped, 1)
		}

		rec := e.reputation.RecordEvent(clientIP, attack, penalty, reason, policy)

		return ScrubDecision{
			Action:          ActionDrop,
			AttackType:      attack,
			ClientIP:        clientIP,
			TenantID:        tenantID,
			ReputationScore: rec.Score,
			ReputationLevel: rec.Level,
			LatencyUs:       latencyUs,
			Reason:          reason,
		}
	}

	// 3. Stateful IP Reputation Evaluation
	rec := e.reputation.GetOrCreateRecord(clientIP)
	latencyUs := time.Since(start).Microseconds()
	atomic.AddInt64(&e.totalLatencyUs, latencyUs)

	// Below drop threshold -> drop
	if rec.Score <= policy.DropThreshold {
		atomic.AddInt64(&e.droppedPackets, 1)
		return ScrubDecision{
			Action:          ActionDrop,
			AttackType:      AttackTypeNone,
			ClientIP:        clientIP,
			TenantID:        tenantID,
			ReputationScore: rec.Score,
			ReputationLevel: rec.Level,
			LatencyUs:       latencyUs,
			Reason:          "reputation score below drop threshold",
		}
	}

	// Below challenge threshold -> issue cryptographic challenge
	if rec.Score <= policy.ChallengeThreshold {
		challenge, _ := e.reputation.IssueChallenge(clientIP, tenantID)
		atomic.AddInt64(&e.challengesIssued, 1)
		return ScrubDecision{
			Action:          ActionChallenge,
			AttackType:      AttackTypeNone,
			ClientIP:        clientIP,
			TenantID:        tenantID,
			ReputationScore: rec.Score,
			ReputationLevel: rec.Level,
			LatencyUs:       latencyUs,
			Reason:          "anomalous stream: cryptographic proof-of-work challenge issued",
			ChallengeIssued: challenge,
		}
	}

	// Clean packet -> pass
	e.reputation.RecordGoodBehavior(clientIP)
	atomic.AddInt64(&e.passedPackets, 1)

	return ScrubDecision{
		Action:          ActionPass,
		AttackType:      AttackTypeNone,
		ClientIP:        clientIP,
		TenantID:        tenantID,
		ReputationScore: rec.Score,
		ReputationLevel: rec.Level,
		LatencyUs:       latencyUs,
		Reason:          "packet passed XDP line-rate scrub",
	}
}

// InspectHTTPRequest inspects Layer-7 HTTP requests for volumetric flooding and abusive patterns.
func (e *ScrubberEngine) InspectHTTPRequest(r *http.Request, tenantID string) ScrubDecision {
	clientIP := extractClientIP(r)
	start := time.Now()

	// Check ban
	if isBanned, reason, _ := e.reputation.IsBanned(clientIP); isBanned {
		latencyUs := time.Since(start).Microseconds()
		atomic.AddInt64(&e.droppedPackets, 1)
		return ScrubDecision{
			Action:          ActionDrop,
			AttackType:      AttackTypeHTTPFlood,
			ClientIP:        clientIP,
			TenantID:        tenantID,
			ReputationScore: 0.0,
			ReputationLevel: ReputationBanned,
			LatencyUs:       latencyUs,
			Reason:          "client IP banned: " + reason,
		}
	}

	// Synthetic packet translation for uniform evaluation
	pkt := PacketMetadata{
		SrcIP:       clientIP,
		DstIP:       r.Host,
		SrcPort:     80,
		DstPort:     443,
		Protocol:    ProtocolTCP,
		TCPFlags:    TCPFlags{ACK: true, PSH: true},
		PayloadSize: int(r.ContentLength),
		Timestamp:   time.Now(),
	}

	return e.InspectPacket(pkt, tenantID)
}

// IssueChallenge generates a challenge token for a suspicious client IP.
func (e *ScrubberEngine) IssueChallenge(ip, tenantID string) (*ChallengeToken, error) {
	tok, err := e.reputation.IssueChallenge(ip, tenantID)
	if err == nil {
		atomic.AddInt64(&e.challengesIssued, 1)
	}
	return tok, err
}

// VerifyChallenge checks client solution and restores reputation if valid.
func (e *ScrubberEngine) VerifyChallenge(tokenStr, solution string) (bool, error) {
	ok, err := e.reputation.VerifyChallenge(tokenStr, solution)
	if ok {
		atomic.AddInt64(&e.challengesSolved, 1)
	}
	return ok, err
}

// BanIP manually isolates an IP into the quarantine ban list.
func (e *ScrubberEngine) BanIP(ip string, duration time.Duration, reason string) {
	e.reputation.BanIP(ip, duration, reason)
}

// UnbanIP manually removes an IP from quarantine.
func (e *ScrubberEngine) UnbanIP(ip string) bool {
	return e.reputation.UnbanIP(ip)
}

// GetReputation retrieves active scoring record for an IP.
func (e *ScrubberEngine) GetReputation(ip string) *IPReputationRecord {
	return e.reputation.GetOrCreateRecord(ip)
}

// ListBans returns all active non-expired banned IPs.
func (e *ScrubberEngine) ListBans() map[string]banEntry {
	return e.reputation.ListBans()
}

// GetStats returns aggregate telemetry and mitigation metrics.
func (e *ScrubberEngine) GetStats() ScrubberStats {
	totalInspected := atomic.LoadInt64(&e.inspectedPackets)
	totalLat := atomic.LoadInt64(&e.totalLatencyUs)
	var avgLat float64 = 0.0
	if totalInspected > 0 {
		avgLat = float64(totalLat) / float64(totalInspected)
	}

	bans := e.reputation.ListBans()

	return ScrubberStats{
		TotalPacketsInspected: totalInspected,
		PacketsPassed:         atomic.LoadInt64(&e.passedPackets),
		PacketsDropped:        atomic.LoadInt64(&e.droppedPackets),
		ChallengesIssued:      atomic.LoadInt64(&e.challengesIssued),
		ChallengesVerified:    atomic.LoadInt64(&e.challengesSolved),
		ActiveBans:            len(bans),
		AvgMitigationLatencyUs: avgLat,
		SYNFloodsMitigated:    atomic.LoadInt64(&e.synFloodsMitigated),
		UDPFloodsMitigated:    atomic.LoadInt64(&e.udpFloodsMitigated),
		L7FloodsMitigated:     atomic.LoadInt64(&e.l7FloodsMitigated),
		MalformedPacketsDropped: atomic.LoadInt64(&e.badHdrDropped),
		DriverMode:            e.driver.GetMode(),
		UptimeSeconds:         int64(time.Since(e.startTime).Seconds()),
	}
}

// Reset clears all in-memory trackers, filters, and metrics.
func (e *ScrubberEngine) Reset() {
	e.filter.Reset()
	e.reputation.Reset()
	atomic.StoreInt64(&e.inspectedPackets, 0)
	atomic.StoreInt64(&e.passedPackets, 0)
	atomic.StoreInt64(&e.droppedPackets, 0)
	atomic.StoreInt64(&e.challengesIssued, 0)
	atomic.StoreInt64(&e.challengesSolved, 0)
	atomic.StoreInt64(&e.synFloodsMitigated, 0)
	atomic.StoreInt64(&e.udpFloodsMitigated, 0)
	atomic.StoreInt64(&e.l7FloodsMitigated, 0)
	atomic.StoreInt64(&e.badHdrDropped, 0)
	atomic.StoreInt64(&e.totalLatencyUs, 0)
}

func extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}
	if cfIP := r.Header.Get("CF-Connecting-IP"); cfIP != "" {
		return strings.TrimSpace(cfIP)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}
