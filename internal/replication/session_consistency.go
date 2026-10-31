package replication

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SessionTracker tracks regional client session vector clocks and enforces consistency models.
type SessionTracker struct {
	mu       sync.RWMutex
	sessions map[string]*ClientSessionContext // clientID -> context
}

// NewSessionTracker initializes an empty in-memory session tracker.
func NewSessionTracker() *SessionTracker {
	return &SessionTracker{
		sessions: make(map[string]*ClientSessionContext),
	}
}

// GetOrCreateSession fetches an active session context or creates a new one.
func (st *SessionTracker) GetOrCreateSession(clientID, tenantID string, level ConsistencyLevel) *ClientSessionContext {
	if clientID == "" {
		return nil
	}

	st.mu.Lock()
	defer st.mu.Unlock()

	s, ok := st.sessions[clientID]
	if !ok {
		if level == "" {
			level = ConsistencyEventual
		}
		s = &ClientSessionContext{
			ClientID:             clientID,
			TenantID:             tenantID,
			LastSeenVectorClock:  NewVectorClock(),
			LastWriteTimestamp:   time.Now().UTC(),
			RequestedConsistency: level,
		}
		st.sessions[clientID] = s
	} else {
		if level != "" {
			s.RequestedConsistency = level
		}
		if tenantID != "" {
			s.TenantID = tenantID
		}
	}
	return s
}

// RecordWrite advances the client session's last seen vector clock after a successful write.
func (st *SessionTracker) RecordWrite(clientID, tenantID string, clock VectorClock) {
	if clientID == "" {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()

	s, ok := st.sessions[clientID]
	if !ok {
		s = &ClientSessionContext{
			ClientID:             clientID,
			TenantID:             tenantID,
			LastSeenVectorClock:  clock.Clone(),
			LastWriteTimestamp:   time.Now().UTC(),
			RequestedConsistency: ConsistencyReadYourWrites,
		}
		st.sessions[clientID] = s
		return
	}
	s.LastSeenVectorClock = s.LastSeenVectorClock.Merge(clock)
	s.LastWriteTimestamp = time.Now().UTC()
}

// RecordRead merges the observed entry's clock into the client's session state.
func (st *SessionTracker) RecordRead(clientID string, clock VectorClock) {
	if clientID == "" {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()

	s, ok := st.sessions[clientID]
	if ok {
		s.LastSeenVectorClock = s.LastSeenVectorClock.Merge(clock)
	}
}

// VerifyConsistency verifies whether the target entry satisfies the client's consistency requirements.
func (st *SessionTracker) VerifyConsistency(clientCtx *ClientSessionContext, entry *ReplicationEntry, localClock VectorClock) error {
	if clientCtx == nil || clientCtx.RequestedConsistency == ConsistencyEventual {
		return nil
	}

	// 1. Read-Your-Writes verification
	if clientCtx.RequestedConsistency == ConsistencyReadYourWrites {
		if len(clientCtx.LastSeenVectorClock) > 0 {
			// If entry is present, check if entry clock dominates client clock for this key
			if entry != nil {
				if !entry.VectorClock.DominatesOrEqual(clientCtx.LastSeenVectorClock) &&
					!localClock.DominatesOrEqual(clientCtx.LastSeenVectorClock) {
					return ErrStaleReadViolation
				}
			} else {
				// Key does not exist locally: check if local clock has caught up with the client write
				if !localClock.DominatesOrEqual(clientCtx.LastSeenVectorClock) {
					return ErrStaleReadViolation
				}
			}
		}
		return nil
	}

	// 2. Monotonic-Read verification
	if clientCtx.RequestedConsistency == ConsistencyMonotonicRead {
		if len(clientCtx.LastSeenVectorClock) > 0 && entry != nil {
			rel := entry.VectorClock.Compare(clientCtx.LastSeenVectorClock)
			if rel == ClockBefore {
				return ErrStaleReadViolation
			}
		}
		return nil
	}

	return nil
}

// PruneIdleSessions purges sessions that have been inactive longer than maxAge.
func (st *SessionTracker) PruneIdleSessions(maxAge time.Duration) int {
	st.mu.Lock()
	defer st.mu.Unlock()

	threshold := time.Now().UTC().Add(-maxAge)
	pruned := 0
	for id, s := range st.sessions {
		if s.LastWriteTimestamp.Before(threshold) {
			delete(st.sessions, id)
			pruned++
		}
	}
	return pruned
}

// ExtractSessionContext parses consistency headers from an HTTP request.
func ExtractSessionContext(r *http.Request) *ClientSessionContext {
	clientID := r.Header.Get("X-CF-Broker-Client-ID")
	if clientID == "" {
		clientID = r.Header.Get("X-Client-ID")
	}
	if clientID == "" {
		return nil
	}

	tenantID := r.Header.Get("X-CF-Broker-Tenant")
	levelStr := strings.ToLower(strings.TrimSpace(r.Header.Get("X-CF-Broker-Consistency")))
	level := ConsistencyEventual
	switch ConsistencyLevel(levelStr) {
	case ConsistencyReadYourWrites:
		level = ConsistencyReadYourWrites
	case ConsistencyMonotonicRead:
		level = ConsistencyMonotonicRead
	case ConsistencyStrongQuorum:
		level = ConsistencyStrongQuorum
	}

	ctx := &ClientSessionContext{
		ClientID:             clientID,
		TenantID:             tenantID,
		LastSeenVectorClock:  NewVectorClock(),
		LastWriteTimestamp:   time.Now().UTC(),
		RequestedConsistency: level,
	}

	clockHeader := r.Header.Get("X-CF-Broker-VectorClock")
	if clockHeader != "" {
		if vc, err := ParseVectorClock([]byte(clockHeader)); err == nil {
			ctx.LastSeenVectorClock = vc
		}
	}

	return ctx
}

// AttachConsistencyHeaders annotates outgoing HTTP responses with session vector clocks.
func AttachConsistencyHeaders(w http.ResponseWriter, clientCtx *ClientSessionContext, clock VectorClock) {
	if clock != nil {
		clockBytes, err := json.Marshal(clock)
		if err == nil {
			w.Header().Set("X-CF-Broker-VectorClock", string(clockBytes))
		}
	}
	if clientCtx != nil && clientCtx.RequestedConsistency != "" {
		w.Header().Set("X-CF-Broker-Consistency", string(clientCtx.RequestedConsistency))
	}
}
