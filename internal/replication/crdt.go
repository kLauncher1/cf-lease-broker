package replication

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"
	"time"
)

// PNCounter implements a state-based Positive-Negative distributed counter CRDT.
// Operations are commutative, associative, and idempotent.
type PNCounter struct {
	mu sync.RWMutex
	P  map[string]int64 `json:"p"`
	N  map[string]int64 `json:"n"`
}

// NewPNCounter constructs an initialized PN-Counter.
func NewPNCounter() *PNCounter {
	return &PNCounter{
		P: make(map[string]int64),
		N: make(map[string]int64),
	}
}

// Inc increases the counter for the given node ID by delta.
func (c *PNCounter) Inc(nodeID string, delta int64) {
	if delta == 0 || nodeID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if delta > 0 {
		c.P[nodeID] += delta
	} else {
		c.N[nodeID] += -delta
	}
}

// Dec decreases the counter for the given node ID by delta.
func (c *PNCounter) Dec(nodeID string, delta int64) {
	c.Inc(nodeID, -delta)
}

// Value returns the aggregated value: sum(P) - sum(N).
func (c *PNCounter) Value() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var pSum, nSum int64
	for _, v := range c.P {
		pSum += v
	}
	for _, v := range c.N {
		nSum += v
	}
	return pSum - nSum
}

// Clone returns an isolated deep copy of the PN-Counter.
func (c *PNCounter) Clone() *PNCounter {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cp := NewPNCounter()
	for k, v := range c.P {
		cp.P[k] = v
	}
	for k, v := range c.N {
		cp.N[k] = v
	}
	return cp
}

// Merge combines another PN-Counter using the join-semilattice maximum.
func (c *PNCounter) Merge(other *PNCounter) *PNCounter {
	if other == nil {
		return c.Clone()
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	other.mu.RLock()
	defer other.mu.RUnlock()

	for k, v := range other.P {
		if cur, ok := c.P[k]; !ok || v > cur {
			c.P[k] = v
		}
	}
	for k, v := range other.N {
		if cur, ok := c.N[k]; !ok || v > cur {
			c.N[k] = v
		}
	}

	cp := NewPNCounter()
	for k, v := range c.P {
		cp.P[k] = v
	}
	for k, v := range c.N {
		cp.N[k] = v
	}
	return cp
}

// Marshal serializes the counter to JSON bytes.
func (c *PNCounter) Marshal() ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return json.Marshal(c)
}

// UnmarshalPNCounter deserializes JSON bytes into a PN-Counter.
func UnmarshalPNCounter(b []byte) (*PNCounter, error) {
	c := NewPNCounter()
	if len(b) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, err
	}
	return c, nil
}

// ORSet implements an Observed-Removed Set (Add-Wins Set) CRDT.
// Supports concurrent additions and removals with deterministic convergence.
type ORSet struct {
	mu         sync.RWMutex
	Entries    map[string]map[string]time.Time `json:"entries"`    // elem -> tag -> timestamp
	Tombstones map[string]map[string]time.Time `json:"tombstones"` // elem -> tag -> timestamp
}

// NewORSet constructs an initialized Observed-Removed Set.
func NewORSet() *ORSet {
	return &ORSet{
		Entries:    make(map[string]map[string]time.Time),
		Tombstones: make(map[string]map[string]time.Time),
	}
}

func generateTag() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Add inserts an element into the set with an optional unique tag.
func (s *ORSet) Add(elem string, tag string) string {
	if elem == "" {
		return ""
	}
	if tag == "" {
		tag = generateTag()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tags, ok := s.Entries[elem]
	if !ok {
		tags = make(map[string]time.Time)
		s.Entries[elem] = tags
	}
	tags[tag] = time.Now().UTC()
	return tag
}

// Remove removes all currently observed instances of the element.
func (s *ORSet) Remove(elem string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	tags, ok := s.Entries[elem]
	if !ok || len(tags) == 0 {
		return false
	}

	tb, okTb := s.Tombstones[elem]
	if !okTb {
		tb = make(map[string]time.Time)
		s.Tombstones[elem] = tb
	}

	for tag, t := range tags {
		tb[tag] = t
	}
	return true
}

// Contains checks if the element is currently active in the set.
func (s *ORSet) Contains(elem string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tags, ok := s.Entries[elem]
	if !ok || len(tags) == 0 {
		return false
	}

	tb := s.Tombstones[elem]
	for tag := range tags {
		if tb == nil || !hasTag(tb, tag) {
			return true
		}
	}
	return false
}

func hasTag(m map[string]time.Time, tag string) bool {
	_, ok := m[tag]
	return ok
}

// Elements returns a sorted slice of all active elements in the set.
func (s *ORSet) Elements() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]string, 0)
	for elem, tags := range s.Entries {
		tb := s.Tombstones[elem]
		active := false
		for tag := range tags {
			if tb == nil || !hasTag(tb, tag) {
				active = true
				break
			}
		}
		if active {
			result = append(result, elem)
		}
	}
	sort.Strings(result)
	return result
}

// Clone returns an isolated deep copy of the ORSet.
func (s *ORSet) Clone() *ORSet {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cp := NewORSet()
	for elem, tags := range s.Entries {
		m := make(map[string]time.Time, len(tags))
		for tag, t := range tags {
			m[tag] = t
		}
		cp.Entries[elem] = m
	}
	for elem, tbs := range s.Tombstones {
		m := make(map[string]time.Time, len(tbs))
		for tag, t := range tbs {
			m[tag] = t
		}
		cp.Tombstones[elem] = m
	}
	return cp
}

// Merge combines another ORSet by taking the union of entries and tombstones.
func (s *ORSet) Merge(other *ORSet) *ORSet {
	if other == nil {
		return s.Clone()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	other.mu.RLock()
	defer other.mu.RUnlock()

	for elem, tags := range other.Entries {
		cur, ok := s.Entries[elem]
		if !ok {
			cur = make(map[string]time.Time)
			s.Entries[elem] = cur
		}
		for tag, t := range tags {
			cur[tag] = t
		}
	}

	for elem, tbs := range other.Tombstones {
		cur, ok := s.Tombstones[elem]
		if !ok {
			cur = make(map[string]time.Time)
			s.Tombstones[elem] = cur
		}
		for tag, t := range tbs {
			cur[tag] = t
		}
	}

	// Build clone to return
	cp := NewORSet()
	for elem, tags := range s.Entries {
		m := make(map[string]time.Time, len(tags))
		for tag, t := range tags {
			m[tag] = t
		}
		cp.Entries[elem] = m
	}
	for elem, tbs := range s.Tombstones {
		m := make(map[string]time.Time, len(tbs))
		for tag, t := range tbs {
			m[tag] = t
		}
		cp.Tombstones[elem] = m
	}
	return cp
}

// Marshal serializes the set to JSON bytes.
func (s *ORSet) Marshal() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return json.Marshal(s)
}

// UnmarshalORSet deserializes JSON bytes into an ORSet.
func UnmarshalORSet(b []byte) (*ORSet, error) {
	s := NewORSet()
	if len(b) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, err
	}
	return s, nil
}

// LWWRegister implements a Last-Write-Wins Register with deterministic tie breaking.
type LWWRegister struct {
	mu        sync.RWMutex
	Value     string      `json:"value"`
	Timestamp time.Time   `json:"timestamp"`
	Clock     VectorClock `json:"clock"`
	NodeID    string      `json:"node_id"`
}

// NewLWWRegister constructs an initial LWW register.
func NewLWWRegister(val string, nodeID string) *LWWRegister {
	vc := NewVectorClock()
	vc.Increment(nodeID)
	return &LWWRegister{
		Value:     val,
		Timestamp: time.Now().UTC(),
		Clock:     vc,
		NodeID:    nodeID,
	}
}

// Set updates the value of the register with a new timestamp and advanced clock.
func (r *LWWRegister) Set(val string, nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.Value = val
	r.Timestamp = time.Now().UTC()
	r.NodeID = nodeID
	r.Clock.Increment(nodeID)
}

// Merge combines another register according to vector clock causality and deterministic tie breaking.
func (r *LWWRegister) Merge(other *LWWRegister) {
	if other == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	other.mu.RLock()
	defer other.mu.RUnlock()

	rel := r.Clock.Compare(other.Clock)
	switch rel {
	case ClockBefore:
		// other dominates this
		r.Value = other.Value
		r.Timestamp = other.Timestamp
		r.Clock = r.Clock.Merge(other.Clock)
		r.NodeID = other.NodeID
	case ClockAfter:
		// this dominates other; keep this value, merge clock
		r.Clock = r.Clock.Merge(other.Clock)
	case ClockConcurrent:
		// concurrent: deterministic tie break
		if DeterministicTieBreak(other.Timestamp, r.Timestamp, other.NodeID, r.NodeID) {
			r.Value = other.Value
			r.Timestamp = other.Timestamp
			r.NodeID = other.NodeID
		}
		r.Clock = r.Clock.Merge(other.Clock)
	case ClockEqual:
		// equal, nothing to change
	}
}

// Get returns the current value and metadata.
func (r *LWWRegister) Get() (string, time.Time, VectorClock) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.Value, r.Timestamp, r.Clock.Clone()
}
