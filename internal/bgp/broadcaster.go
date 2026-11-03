package bgp

import (
	"fmt"
	"sync"
	"time"
)

// Broadcaster coordinates Anycast prefix announcements and sub-5ms line-rate withdrawals.
type Broadcaster struct {
	mu                sync.RWMutex
	prefixes          map[string]*AnycastPrefix
	withdrawalHistory []*WithdrawalResult
	maxHistory        int
}

// NewBroadcaster constructs an Anycast prefix broadcaster with history retention.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		prefixes:          make(map[string]*AnycastPrefix),
		withdrawalHistory: make([]*WithdrawalResult, 0, 100),
		maxHistory:        100,
	}
}

// AnnouncePrefix registers and broadcasts an Anycast IP prefix across active peer sessions.
func (b *Broadcaster) AnnouncePrefix(
	cidr string,
	originASN uint32,
	nextHop string,
	communities []string,
	med uint32,
	localPref uint32,
	targetPoPs []string,
	peers []*PeerSession,
) (*AnycastPrefix, error) {
	if _, _, _, err := ParseHeader(EncodeHeader(TypeKeepalive, nil)); err != nil {
		return nil, err
	}
	if _, err := EncodeCIDR(cidr); err != nil {
		return nil, ErrInvalidCIDR
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	activePeers := make([]uint32, 0, len(peers))
	for _, p := range peers {
		if p.IsEstablished() {
			activePeers = append(activePeers, p.GetConfig().PeerASN)
		}
	}

	prefix, exists := b.prefixes[cidr]
	if !exists {
		prefix = &AnycastPrefix{
			CIDR:        cidr,
			OriginASN:   originASN,
			NextHop:     nextHop,
			Communities: communities,
			MED:         med,
			LocalPref:   localPref,
			Status:      PrefixActive,
			AnnouncedAt: time.Now(),
			TargetPoPs:  targetPoPs,
			ActivePeers: activePeers,
		}
		b.prefixes[cidr] = prefix
	} else {
		prefix.OriginASN = originASN
		prefix.NextHop = nextHop
		prefix.Communities = communities
		prefix.MED = med
		prefix.LocalPref = localPref
		prefix.Status = PrefixActive
		prefix.AnnouncedAt = time.Now()
		prefix.WithdrawnAt = nil
		prefix.TargetPoPs = targetPoPs
		prefix.ActivePeers = activePeers
	}

	// Propagate announcement in parallel across active peer sessions
	var wg sync.WaitGroup
	for _, p := range peers {
		if p.IsEstablished() {
			wg.Add(1)
			go func(session *PeerSession) {
				defer wg.Done()
				session.AnnouncePrefix(prefix)
			}(p)
		}
	}
	wg.Wait()

	return prefix, nil
}

// WithdrawPrefix performs sub-5ms line-rate withdrawal of an Anycast prefix across peers.
func (b *Broadcaster) WithdrawPrefix(cidr string, reason string, peers []*PeerSession) (*WithdrawalResult, error) {
	start := time.Now()

	b.mu.Lock()
	prefix, exists := b.prefixes[cidr]
	if !exists {
		b.mu.Unlock()
		return nil, ErrPrefixNotFound
	}

	now := time.Now()
	prefix.Status = PrefixWithdrawn
	prefix.WithdrawnAt = &now
	prefix.WithdrawCount++
	b.mu.Unlock()

	// Execute line-rate withdrawal concurrently across peer sessions
	notifiedCount := 0
	var wg sync.WaitGroup
	var notifyMu sync.Mutex

	for _, p := range peers {
		if p.IsEstablished() {
			wg.Add(1)
			go func(session *PeerSession) {
				defer wg.Done()
				if session.WithdrawPrefix(cidr) {
					notifyMu.Lock()
					notifiedCount++
					notifyMu.Unlock()
				}
			}(p)
		}
	}
	wg.Wait()

	durationUs := time.Since(start).Microseconds()
	withinSLA := durationUs <= 5000 // 5ms line-rate target

	res := &WithdrawalResult{
		CIDR:          cidr,
		Reason:        reason,
		PeersNotified: notifiedCount,
		DurationUs:    durationUs,
		WithinSLA:     withinSLA,
		ExecutedAt:    now,
	}

	b.mu.Lock()
	prefix.LastDurationUs = durationUs
	prefix.ActivePeers = nil
	b.withdrawalHistory = append(b.withdrawalHistory, res)
	if len(b.withdrawalHistory) > b.maxHistory {
		b.withdrawalHistory = b.withdrawalHistory[1:]
	}
	b.mu.Unlock()

	return res, nil
}

// DivertPoPTraffic re-routes Anycast prefixes away from a failing PoP to a standby target PoP.
func (b *Broadcaster) DivertPoPTraffic(
	failedPoP string,
	targetPoP string,
	reason string,
	peers []*PeerSession,
) (*DiversionResult, error) {
	start := time.Now()

	b.mu.RLock()
	var affected []string
	for cidr, p := range b.prefixes {
		for _, pop := range p.TargetPoPs {
			if pop == failedPoP && p.Status == PrefixActive {
				affected = append(affected, cidr)
				break
			}
		}
	}
	b.mu.RUnlock()

	var events []*WithdrawalResult
	for _, cidr := range affected {
		withdrawReason := fmt.Sprintf("PoP partition diversion: %s -> %s (%s)", failedPoP, targetPoP, reason)
		ev, err := b.WithdrawPrefix(cidr, withdrawReason, peers)
		if err == nil && ev != nil {
			ev.DivertedToPoP = targetPoP
			events = append(events, ev)
		}

		// Re-announce over healthy target PoP
		b.mu.Lock()
		if p, ok := b.prefixes[cidr]; ok {
			p.Status = PrefixDiverted
			p.DiversionTarget = targetPoP
			var newPoPs []string
			for _, pop := range p.TargetPoPs {
				if pop != failedPoP {
					newPoPs = append(newPoPs, pop)
				}
			}
			newPoPs = append(newPoPs, targetPoP)
			p.TargetPoPs = newPoPs
		}
		b.mu.Unlock()
	}

	totalDuration := time.Since(start).Microseconds()
	return &DiversionResult{
		FailedPoP:        failedPoP,
		AlternativePoP:   targetPoP,
		AffectedPrefixes: affected,
		WithdrawalEvents: events,
		TotalDurationUs:  totalDuration,
		CompletedAt:      time.Now(),
	}, nil
}

// GetPrefix retrieves an Anycast prefix by CIDR.
func (b *Broadcaster) GetPrefix(cidr string) (*AnycastPrefix, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	pr, ok := b.prefixes[cidr]
	return pr, ok
}

// ListPrefixes returns all registered Anycast prefixes.
func (b *Broadcaster) ListPrefixes() []*AnycastPrefix {
	b.mu.RLock()
	defer b.mu.RUnlock()
	list := make([]*AnycastPrefix, 0, len(b.prefixes))
	for _, pr := range b.prefixes {
		list = append(list, pr)
	}
	return list
}

// ListWithdrawalHistory returns recent prefix withdrawal events.
func (b *Broadcaster) ListWithdrawalHistory() []*WithdrawalResult {
	b.mu.RLock()
	defer b.mu.RUnlock()
	history := make([]*WithdrawalResult, len(b.withdrawalHistory))
	copy(history, b.withdrawalHistory)
	return history
}

// Reset clears all prefixes and withdrawal history.
func (b *Broadcaster) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prefixes = make(map[string]*AnycastPrefix)
	b.withdrawalHistory = make([]*WithdrawalResult, 0, b.maxHistory)
}
