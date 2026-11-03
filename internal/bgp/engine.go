package bgp

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// BGPEngine coordinates Anycast prefix broadcasting, BGP peering sessions, and sub-5ms withdrawals.
type BGPEngine struct {
	mu                  sync.RWMutex
	broadcaster         *Broadcaster
	communityRouter     *CommunityRouter
	peers               map[uint32]*PeerSession
	running             bool
	cancelFn            context.CancelFunc
	totalAnnouncements  int64
	totalWithdrawals    int64
	totalKeepalivesSent int64
	totalPacketsSent    int64
	totalPacketsRecv    int64
	totalWithdrawalUs   int64
	minWithdrawalUs     int64
	maxWithdrawalUs     int64
	slaViolations       int64
	lastWithdrawalUs    int64
}

// NewBGPEngine initializes an engine with standard Tier-1 transit peers and Anycast prefixes.
func NewBGPEngine() *BGPEngine {
	eng := &BGPEngine{
		broadcaster:     NewBroadcaster(),
		communityRouter: NewCommunityRouter(),
		peers:           make(map[uint32]*PeerSession),
		minWithdrawalUs: -1,
	}
	eng.seedDefaultPeers()
	eng.seedDefaultPrefixes()
	return eng
}

// seedDefaultPeers registers baseline Tier-1 transit peering sessions.
func (e *BGPEngine) seedDefaultPeers() {
	defaultPeers := []PeerConfig{
		{
			PeerASN:           3356,
			PeerIP:            "4.69.130.1",
			LocalASN:          64512,
			LocalIP:           "198.51.100.254",
			HoldTime:          90 * time.Second,
			KeepaliveInterval: 30 * time.Second,
			Description:       "Lumen/Level3 Tier-1 Low Latency Transit",
			TransitCostPerMb:  0.035,
			Tier1Provider:     "Lumen",
		},
		{
			PeerASN:           1299,
			PeerIP:            "62.115.140.1",
			LocalASN:          64512,
			LocalIP:           "198.51.100.254",
			HoldTime:          90 * time.Second,
			KeepaliveInterval: 30 * time.Second,
			Description:       "Telia/Arelion Global Balanced Transit",
			TransitCostPerMb:  0.020,
			Tier1Provider:     "Arelion",
		},
		{
			PeerASN:           174,
			PeerIP:            "130.117.1.1",
			LocalASN:          64512,
			LocalIP:           "198.51.100.254",
			HoldTime:          90 * time.Second,
			KeepaliveInterval: 30 * time.Second,
			Description:       "Cogent Bulk Low-Cost Transit",
			TransitCostPerMb:  0.005,
			Tier1Provider:     "Cogent",
		},
		{
			PeerASN:           13335,
			PeerIP:            "172.68.1.1",
			LocalASN:          64512,
			LocalIP:           "198.51.100.254",
			HoldTime:          90 * time.Second,
			KeepaliveInterval: 30 * time.Second,
			Description:       "Cloudflare Edge Direct Interconnect",
			TransitCostPerMb:  0.000,
			Tier1Provider:     "Cloudflare",
		},
	}

	for _, cfg := range defaultPeers {
		session := NewPeerSession(cfg)
		session.TriggerConnect()
		_ = session.HandleOpen(4, uint16(cfg.PeerASN), 90)
		e.peers[cfg.PeerASN] = session
	}
}

// seedDefaultPrefixes announces initial Anycast IP prefixes.
func (e *BGPEngine) seedDefaultPrefixes() {
	activeSessions := e.getActiveSessions()
	_, _ = e.broadcaster.AnnouncePrefix(
		"198.51.100.0/24",
		64512,
		"198.51.100.1",
		[]string{CommunityTransitBalanced, "64512:1000"},
		10,
		150,
		[]string{"SFO", "IAD", "LHR", "NRT"},
		activeSessions,
	)
	atomic.AddInt64(&e.totalAnnouncements, 1)

	_, _ = e.broadcaster.AnnouncePrefix(
		"203.0.113.0/24",
		64512,
		"203.0.113.1",
		[]string{CommunityTransitLowLatency, "64512:2000"},
		5,
		200,
		[]string{"SFO", "FRA", "SIN", "ICN"},
		activeSessions,
	)
	atomic.AddInt64(&e.totalAnnouncements, 1)
}

// Start launches the background keepalive and health ticker.
func (e *BGPEngine) Start(ctx context.Context) {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return
	}
	e.running = true
	runCtx, cancel := context.WithCancel(ctx)
	e.cancelFn = cancel
	e.mu.Unlock()

	go e.runMaintenanceLoop(runCtx)
}

// Stop shuts down the background ticker.
func (e *BGPEngine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.running {
		return
	}
	e.running = false
	if e.cancelFn != nil {
		e.cancelFn()
	}
}

// runMaintenanceLoop periodically triggers BGP keepalives across active peers.
func (e *BGPEngine) runMaintenanceLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.mu.RLock()
			for _, p := range e.peers {
				if p.IsEstablished() {
					_ = p.SendKeepalive()
					atomic.AddInt64(&e.totalKeepalivesSent, 1)
					atomic.AddInt64(&e.totalPacketsSent, 1)
				}
			}
			e.mu.RUnlock()
		}
	}
}

func (e *BGPEngine) getActiveSessions() []*PeerSession {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var list []*PeerSession
	for _, p := range e.peers {
		list = append(list, p)
	}
	return list
}

// RegisterPeer registers and connects a new upstream BGP peering session.
func (e *BGPEngine) RegisterPeer(cfg PeerConfig) (*PeerSession, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	session := NewPeerSession(cfg)
	session.TriggerConnect()
	_ = session.HandleOpen(4, uint16(cfg.PeerASN), 90)
	e.peers[cfg.PeerASN] = session
	return session, nil
}

// GetPeer looks up an active BGP peering session by ASN.
func (e *BGPEngine) GetPeer(asn uint32) (*PeerSession, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	p, ok := e.peers[asn]
	return p, ok
}

// ListPeers returns snapshots of all registered peering sessions.
func (e *BGPEngine) ListPeers() []PeerSessionSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var snaps []PeerSessionSnapshot
	for _, p := range e.peers {
		snaps = append(snaps, p.GetSnapshot())
	}
	return snaps
}

// AnnouncePrefix announces a new Anycast prefix to upstream transit providers.
func (e *BGPEngine) AnnouncePrefix(
	cidr string,
	originASN uint32,
	nextHop string,
	communities []string,
	med uint32,
	localPref uint32,
	pops []string,
) (*AnycastPrefix, error) {
	activeSessions := e.getActiveSessions()
	pr, err := e.broadcaster.AnnouncePrefix(cidr, originASN, nextHop, communities, med, localPref, pops, activeSessions)
	if err == nil {
		atomic.AddInt64(&e.totalAnnouncements, 1)
		atomic.AddInt64(&e.totalPacketsSent, int64(len(activeSessions)))
	}
	return pr, err
}

// WithdrawPrefix withdraws an Anycast prefix across peers within 5 milliseconds SLA.
func (e *BGPEngine) WithdrawPrefix(cidr string, reason string) (*WithdrawalResult, error) {
	activeSessions := e.getActiveSessions()
	res, err := e.broadcaster.WithdrawPrefix(cidr, reason, activeSessions)
	if err == nil && res != nil {
		atomic.AddInt64(&e.totalWithdrawals, 1)
		atomic.AddInt64(&e.totalWithdrawalUs, res.DurationUs)
		atomic.StoreInt64(&e.lastWithdrawalUs, res.DurationUs)
		atomic.AddInt64(&e.totalPacketsSent, int64(res.PeersNotified))

		e.mu.Lock()
		if e.minWithdrawalUs < 0 || res.DurationUs < e.minWithdrawalUs {
			e.minWithdrawalUs = res.DurationUs
		}
		if res.DurationUs > e.maxWithdrawalUs {
			e.maxWithdrawalUs = res.DurationUs
		}
		if !res.WithinSLA {
			e.slaViolations++
		}
		e.mu.Unlock()
	}
	return res, err
}

// DivertPoP re-routes prefixes from a partitioned PoP to a target PoP within sub-5ms.
func (e *BGPEngine) DivertPoP(failedPoP string, targetPoP string, reason string) (*DiversionResult, error) {
	activeSessions := e.getActiveSessions()
	res, err := e.broadcaster.DivertPoPTraffic(failedPoP, targetPoP, reason, activeSessions)
	if err == nil && res != nil {
		atomic.AddInt64(&e.totalWithdrawals, int64(len(res.WithdrawalEvents)))
	}
	return res, err
}

// ResolveRoute dynamically determines the Anycast prefix, peer ASN, and community tags for a request.
func (e *BGPEngine) ResolveRoute(tenantTier string, service string) (cidr string, peerASN uint32, communities []string, latencyUs int64) {
	start := time.Now()
	tags, asn, _ := e.communityRouter.EvaluateRouting(tenantTier, service)

	var selectedCIDR string = "198.51.100.0/24"
	prefixes := e.broadcaster.ListPrefixes()
	for _, p := range prefixes {
		if p.Status == PrefixActive {
			selectedCIDR = p.CIDR
			break
		}
	}

	elapsed := time.Since(start).Microseconds()
	if elapsed <= 0 {
		elapsed = 1
	}
	return selectedCIDR, asn, tags, elapsed
}

// Broadcaster returns the underlying Anycast broadcaster.
func (e *BGPEngine) Broadcaster() *Broadcaster {
	return e.broadcaster
}

// CommunityRouter returns the underlying community router.
func (e *BGPEngine) CommunityRouter() *CommunityRouter {
	return e.communityRouter
}

// GetStats returns aggregated operational telemetry and counters.
func (e *BGPEngine) GetStats() BGPStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	prefixes := e.broadcaster.ListPrefixes()
	activeCount := 0
	withdrawnCount := 0
	for _, p := range prefixes {
		if p.Status == PrefixActive {
			activeCount++
		} else {
			withdrawnCount++
		}
	}

	tw := atomic.LoadInt64(&e.totalWithdrawals)
	avgUs := 0.0
	if tw > 0 {
		avgUs = float64(atomic.LoadInt64(&e.totalWithdrawalUs)) / float64(tw)
	}

	minUs := e.minWithdrawalUs
	if minUs < 0 {
		minUs = 0
	}

	return BGPStats{
		ActivePeersCount:    len(e.peers),
		AnnouncedPrefixes:   activeCount,
		WithdrawnPrefixes:   withdrawnCount,
		TotalAnnouncements:  atomic.LoadInt64(&e.totalAnnouncements),
		TotalWithdrawals:    tw,
		TotalKeepalivesSent: atomic.LoadInt64(&e.totalKeepalivesSent),
		TotalPacketsSent:    atomic.LoadInt64(&e.totalPacketsSent),
		TotalPacketsRecv:    atomic.LoadInt64(&e.totalPacketsRecv),
		AvgWithdrawalUs:     avgUs,
		MinWithdrawalUs:     minUs,
		MaxWithdrawalUs:     e.maxWithdrawalUs,
		SLAViolationCount:   e.slaViolations,
		LastWithdrawalUs:    atomic.LoadInt64(&e.lastWithdrawalUs),
	}
}

// Reset clears state and re-seeds default peers and prefixes.
func (e *BGPEngine) Reset() {
	e.mu.Lock()
	e.peers = make(map[uint32]*PeerSession)
	e.minWithdrawalUs = -1
	e.maxWithdrawalUs = 0
	e.slaViolations = 0
	e.mu.Unlock()

	e.broadcaster.Reset()
	e.seedDefaultPeers()
	e.seedDefaultPrefixes()
}
