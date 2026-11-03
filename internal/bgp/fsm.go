package bgp

import (
	"sync"
	"sync/atomic"
	"time"
)

// PeerSessionSnapshot holds an immutable view of an upstream BGP peering session.
type PeerSessionSnapshot struct {
	PeerASN           uint32        `json:"peer_asn"`
	PeerIP            string        `json:"peer_ip"`
	LocalASN          uint32        `json:"local_asn"`
	LocalIP           string        `json:"local_ip"`
	State             FSMState      `json:"state"`
	HoldTime          time.Duration `json:"hold_time"`
	KeepaliveInterval time.Duration `json:"keepalive_interval"`
	EstablishedAt     *time.Time    `json:"established_at,omitempty"`
	LastKeepaliveSent *time.Time    `json:"last_keepalive_sent,omitempty"`
	LastKeepaliveRecv *time.Time    `json:"last_keepalive_recv,omitempty"`
	FlapCount         int           `json:"flap_count"`
	PacketsSent       int64         `json:"packets_sent"`
	PacketsRecv       int64         `json:"packets_recv"`
	AnnouncedCount    int           `json:"announced_count"`
	TransitCostPerMb  float64       `json:"transit_cost_per_mb"`
	Tier1Provider     string        `json:"tier1_provider"`
	Description       string        `json:"description,omitempty"`
}

// PeerSession represents an upstream BGP-4 peering connection and state machine.
type PeerSession struct {
	mu                sync.RWMutex
	config            PeerConfig
	state             FSMState
	establishedAt     time.Time
	lastKeepaliveSent time.Time
	lastKeepaliveRecv time.Time
	flapCount         int
	packetsSent       int64
	packetsRecv       int64
	announcedPrefixes map[string]*AnycastPrefix
}

// NewPeerSession instantiates an idle peering session for the given peer config.
func NewPeerSession(cfg PeerConfig) *PeerSession {
	if cfg.HoldTime <= 0 {
		cfg.HoldTime = 90 * time.Second
	}
	if cfg.KeepaliveInterval <= 0 {
		cfg.KeepaliveInterval = 30 * time.Second
	}
	return &PeerSession{
		config:            cfg,
		state:             StateIdle,
		announcedPrefixes: make(map[string]*AnycastPrefix),
	}
}

// GetConfig returns the session's configuration parameters.
func (p *PeerSession) GetConfig() PeerConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.config
}

// GetState returns the current BGP FSM state.
func (p *PeerSession) GetState() FSMState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}

// IsEstablished checks if the session is currently in the ESTABLISHED state.
func (p *PeerSession) IsEstablished() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state == StateEstablished
}

// TriggerConnect initiates connection and advances state from Idle/Active to Connect/OpenSent.
func (p *PeerSession) TriggerConnect() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.state == StateEstablished {
		p.flapCount++
	}
	p.state = StateConnect
	// Transition to OpenSent immediately upon TCP connection establishment
	p.state = StateOpenSent
	atomic.AddInt64(&p.packetsSent, 1)
}

// HandleOpen receives an OPEN message from the remote peer and transitions FSM.
func (p *PeerSession) HandleOpen(version uint8, peerAS uint16, holdTime uint16) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	atomic.AddInt64(&p.packetsRecv, 1)
	if p.state == StateOpenSent {
		p.state = StateOpenConfirm
		now := time.Now()
		p.lastKeepaliveSent = now
		atomic.AddInt64(&p.packetsSent, 1)
		// Auto-advance to Established once OpenConfirm is achieved
		p.state = StateEstablished
		p.establishedAt = now
		p.lastKeepaliveRecv = now
		return nil
	}
	return nil
}

// HandleKeepalive records receipt of remote KEEPALIVE and ensures session stays live.
func (p *PeerSession) HandleKeepalive() {
	p.mu.Lock()
	defer p.mu.Unlock()

	atomic.AddInt64(&p.packetsRecv, 1)
	p.lastKeepaliveRecv = time.Now()
	if p.state == StateOpenConfirm {
		p.state = StateEstablished
		p.establishedAt = time.Now()
	}
}

// SendKeepalive returns the raw bytes for a KEEPALIVE and updates sent timestamp.
func (p *PeerSession) SendKeepalive() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()

	atomic.AddInt64(&p.packetsSent, 1)
	p.lastKeepaliveSent = time.Now()
	return EncodeKeepalive()
}

// HandleNotification processes a remote BGP NOTIFICATION and transitions back to Idle.
func (p *PeerSession) HandleNotification(errorCode, errorSubcode uint8) {
	p.mu.Lock()
	defer p.mu.Unlock()

	atomic.AddInt64(&p.packetsRecv, 1)
	p.flapCount++
	p.state = StateIdle
	p.establishedAt = time.Time{}
}

// ResetSession forces a teardown of the peering session.
func (p *PeerSession) ResetSession() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.state == StateEstablished {
		p.flapCount++
	}
	p.state = StateIdle
	p.establishedAt = time.Time{}
	p.announcedPrefixes = make(map[string]*AnycastPrefix)
}

// AnnouncePrefix records an active prefix on this peering session.
func (p *PeerSession) AnnouncePrefix(prefix *AnycastPrefix) {
	p.mu.Lock()
	defer p.mu.Unlock()

	atomic.AddInt64(&p.packetsSent, 1)
	p.announcedPrefixes[prefix.CIDR] = prefix
}

// WithdrawPrefix removes an announced prefix from this peering session.
func (p *PeerSession) WithdrawPrefix(cidr string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.announcedPrefixes[cidr]; exists {
		delete(p.announcedPrefixes, cidr)
		atomic.AddInt64(&p.packetsSent, 1)
		return true
	}
	return false
}

// ListPrefixes returns all prefixes actively broadcast over this session.
func (p *PeerSession) ListPrefixes() []*AnycastPrefix {
	p.mu.RLock()
	defer p.mu.RUnlock()

	list := make([]*AnycastPrefix, 0, len(p.announcedPrefixes))
	for _, pr := range p.announcedPrefixes {
		list = append(list, pr)
	}
	return list
}

// GetSnapshot produces a thread-safe snapshot of session metrics and state.
func (p *PeerSession) GetSnapshot() PeerSessionSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()

	snap := PeerSessionSnapshot{
		PeerASN:           p.config.PeerASN,
		PeerIP:            p.config.PeerIP,
		LocalASN:          p.config.LocalASN,
		LocalIP:           p.config.LocalIP,
		State:             p.state,
		HoldTime:          p.config.HoldTime,
		KeepaliveInterval: p.config.KeepaliveInterval,
		FlapCount:         p.flapCount,
		PacketsSent:       atomic.LoadInt64(&p.packetsSent),
		PacketsRecv:       atomic.LoadInt64(&p.packetsRecv),
		AnnouncedCount:    len(p.announcedPrefixes),
		TransitCostPerMb:  p.config.TransitCostPerMb,
		Tier1Provider:     p.config.Tier1Provider,
		Description:       p.config.Description,
	}
	if !p.establishedAt.IsZero() {
		est := p.establishedAt
		snap.EstablishedAt = &est
	}
	if !p.lastKeepaliveSent.IsZero() {
		sent := p.lastKeepaliveSent
		snap.LastKeepaliveSent = &sent
	}
	if !p.lastKeepaliveRecv.IsZero() {
		recv := p.lastKeepaliveRecv
		snap.LastKeepaliveRecv = &recv
	}
	return snap
}
