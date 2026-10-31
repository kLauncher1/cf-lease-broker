package replication

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// NewVectorClock creates an empty vector clock.
func NewVectorClock() VectorClock {
	return make(VectorClock)
}

// Clone returns an isolated deep copy of the vector clock.
func (vc VectorClock) Clone() VectorClock {
	if vc == nil {
		return make(VectorClock)
	}
	cp := make(VectorClock, len(vc))
	for k, v := range vc {
		cp[k] = v
	}
	return cp
}

// Increment advances the logical clock for the specified node ID by 1.
func (vc VectorClock) Increment(nodeID string) {
	if nodeID == "" {
		return
	}
	vc[nodeID]++
}

// Get returns the logical tick for a specific node ID, or 0 if absent.
func (vc VectorClock) Get(nodeID string) uint64 {
	return vc[nodeID]
}

// Set explicitly sets the logical counter for a node ID.
func (vc VectorClock) Set(nodeID string, val uint64) {
	if nodeID != "" {
		vc[nodeID] = val
	}
}

// Merge combines another vector clock into this clock by taking the component-wise maximum.
func (vc VectorClock) Merge(other VectorClock) VectorClock {
	res := vc.Clone()
	for k, v := range other {
		if cur, exists := res[k]; !exists || v > cur {
			res[k] = v
		}
	}
	return res
}

// Compare evaluates the partial ordering causality between two vector clocks.
func (vc VectorClock) Compare(other VectorClock) ClockRelation {
	if len(vc) == 0 && len(other) == 0 {
		return ClockEqual
	}

	allKeys := make(map[string]struct{})
	for k := range vc {
		allKeys[k] = struct{}{}
	}
	for k := range other {
		allKeys[k] = struct{}{}
	}

	greater := false
	less := false

	for k := range allKeys {
		v1 := vc[k]
		v2 := other[k]
		if v1 > v2 {
			greater = true
		} else if v1 < v2 {
			less = true
		}
	}

	if greater && !less {
		return ClockAfter
	}
	if less && !greater {
		return ClockBefore
	}
	if !greater && !less {
		return ClockEqual
	}
	return ClockConcurrent
}

// DominatesOrEqual returns true if this clock has witnessed every event in the other clock.
func (vc VectorClock) DominatesOrEqual(other VectorClock) bool {
	if len(other) == 0 {
		return true
	}
	for k, v2 := range other {
		if vc[k] < v2 {
			return false
		}
	}
	return true
}

// DeterministicTieBreak resolves concurrent vector clock updates deterministically.
// Returns true if this entry wins, false if other entry wins.
func DeterministicTieBreak(thisTime, otherTime time.Time, thisNode, otherNode string) bool {
	// 1. Compare timestamps (Last-Write-Wins)
	if !thisTime.Equal(otherTime) {
		return thisTime.After(otherTime)
	}

	// 2. In the rare case of sub-nanosecond timestamp collision, break tie using deterministic sha256
	h1 := sha256.Sum256([]byte(thisNode))
	h2 := sha256.Sum256([]byte(otherNode))
	return hex.EncodeToString(h1[:]) > hex.EncodeToString(h2[:])
}

// String returns a sorted canonical representation: "{nodeA:1, nodeB:2}".
func (vc VectorClock) String() string {
	if len(vc) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(vc))
	for k := range vc {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("{")
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(fmt.Sprintf("%s:%d", k, vc[k]))
	}
	sb.WriteString("}")
	return sb.String()
}

// ToJSON serializes the vector clock into JSON bytes.
func (vc VectorClock) ToJSON() ([]byte, error) {
	return json.Marshal(vc)
}

// ParseVectorClock deserializes a vector clock from JSON bytes or string.
func ParseVectorClock(data []byte) (VectorClock, error) {
	vc := make(VectorClock)
	if len(data) == 0 || string(data) == "{}" {
		return vc, nil
	}
	if err := json.Unmarshal(data, &vc); err != nil {
		return nil, err
	}
	return vc, nil
}
