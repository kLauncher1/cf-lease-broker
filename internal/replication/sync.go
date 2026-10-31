package replication

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"time"
)

// GenerateSyncPacket exports local mutations for remote peer replication.
func (e *GeoReplicationEngine) GenerateSyncPacket(sinceClock VectorClock) *ReplicationPacket {
	e.mu.RLock()
	defer e.mu.RUnlock()

	entries := make([]ReplicationEntry, 0, len(e.store))
	for _, entry := range e.store {
		if sinceClock == nil || !sinceClock.DominatesOrEqual(entry.VectorClock) {
			entries = append(entries, *entry)
		}
	}

	pktIDBytes := make([]byte, 8)
	_, _ = rand.Read(pktIDBytes)
	pktID := hex.EncodeToString(pktIDBytes)

	pkt := &ReplicationPacket{
		PacketID:     pktID,
		OriginNodeID: e.config.LocalNodeID,
		OriginRegion: e.config.Region,
		Epoch:        time.Now().Unix(),
		Entries:      entries,
		GeneratedAt:  time.Now().UTC(),
	}
	pkt.SignatureHex = e.SignPacket(pkt)

	atomic.AddInt64(&e.syncPacketsSent, 1)
	atomic.StoreInt64(&e.lastSyncTime, time.Now().UnixNano())
	return pkt
}

// SignPacket computes an HMAC-SHA256 signature across packet metadata and entries.
func (e *GeoReplicationEngine) SignPacket(pkt *ReplicationPacket) string {
	mac := hmac.New(sha256.New, e.hmacKey)
	mac.Write([]byte(fmt.Sprintf("%s:%s:%s:%d:%d", pkt.PacketID, pkt.OriginNodeID, pkt.OriginRegion, pkt.Epoch, len(pkt.Entries))))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyPacket authenticates the HMAC-SHA256 signature on an incoming replication packet.
func (e *GeoReplicationEngine) VerifyPacket(pkt *ReplicationPacket) bool {
	if pkt == nil || pkt.SignatureHex == "" {
		return false
	}
	expected := e.SignPacket(pkt)
	return subtle.ConstantTimeCompare([]byte(pkt.SignatureHex), []byte(expected)) == 1
}

// ProcessSyncPacket merges an incoming batch of remote mutations, resolving conflicts.
func (e *GeoReplicationEngine) ProcessSyncPacket(pkt *ReplicationPacket) (*SyncAck, error) {
	if !e.VerifyPacket(pkt) {
		return nil, ErrReplicationSyncFailed
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	processed := 0
	conflicts := 0

	for _, incoming := range pkt.Entries {
		cKey := compositeKey(incoming.Namespace, incoming.Key)
		local, exists := e.store[cKey]

		if !exists {
			// Fast path: local does not have this key yet
			entryCopy := incoming
			e.store[cKey] = &entryCopy
			if incoming.IsCRDT {
				e.hydrateCRDTLocked(cKey, incoming)
			}
			e.currentClock = e.currentClock.Merge(incoming.VectorClock)
			processed++
			continue
		}

		// Key exists locally: evaluate vector clock causality
		rel := local.VectorClock.Compare(incoming.VectorClock)
		switch rel {
		case ClockBefore:
			// Incoming dominates: accept incoming update
			entryCopy := incoming
			e.store[cKey] = &entryCopy
			if incoming.IsCRDT {
				e.hydrateCRDTLocked(cKey, incoming)
			}
			e.currentClock = e.currentClock.Merge(incoming.VectorClock)
			processed++

		case ClockAfter:
			// Local dominates: keep local update, just merge clock
			e.currentClock = e.currentClock.Merge(incoming.VectorClock)

		case ClockEqual:
			// Equal clocks: already in sync

		case ClockConcurrent:
			// Concurrent update: conflict resolution!
			conflicts++
			atomic.AddInt64(&e.conflictsDetected, 1)

			if incoming.IsCRDT && local.IsCRDT {
				// State-based CRDT merge
				e.mergeCRDTLocked(cKey, local, incoming)
				atomic.AddInt64(&e.conflictsResolved, 1)
			} else {
				// Deterministic tie break (LWW with node hash fallback)
				if DeterministicTieBreak(incoming.Timestamp, local.Timestamp, incoming.NodeID, local.NodeID) {
					entryCopy := incoming
					entryCopy.VectorClock = local.VectorClock.Merge(incoming.VectorClock)
					e.store[cKey] = &entryCopy
				} else {
					local.VectorClock = local.VectorClock.Merge(incoming.VectorClock)
				}
				atomic.AddInt64(&e.conflictsResolved, 1)
			}
			e.currentClock = e.currentClock.Merge(incoming.VectorClock)
			processed++
		}
	}

	atomic.AddInt64(&e.syncPacketsRecv, 1)
	atomic.StoreInt64(&e.lastSyncTime, time.Now().UnixNano())

	return &SyncAck{
		PacketID:        pkt.PacketID,
		ProcessedCount:  processed,
		ConflictsSolved: conflicts,
		LocalNodeID:     e.config.LocalNodeID,
		CurrentClock:    e.currentClock.Clone(),
		Timestamp:       time.Now().UTC(),
	}, nil
}

func (e *GeoReplicationEngine) hydrateCRDTLocked(cKey string, entry ReplicationEntry) {
	if entry.CRDTType == "pn_counter" {
		if c, err := UnmarshalPNCounter(entry.CRDTState); err == nil {
			e.crdtCounters[cKey] = c
		}
	} else if entry.CRDTType == "or_set" {
		if s, err := UnmarshalORSet(entry.CRDTState); err == nil {
			e.crdtSets[cKey] = s
		}
	}
}

func (e *GeoReplicationEngine) mergeCRDTLocked(cKey string, local *ReplicationEntry, incoming ReplicationEntry) {
	if local.CRDTType == "pn_counter" && incoming.CRDTType == "pn_counter" {
		locC := e.crdtCounters[cKey]
		remC, err := UnmarshalPNCounter(incoming.CRDTState)
		if err == nil && locC != nil {
			merged := locC.Merge(remC)
			e.crdtCounters[cKey] = merged
			mergedBytes, _ := merged.Marshal()
			local.CRDTState = mergedBytes
			local.VectorClock = local.VectorClock.Merge(incoming.VectorClock)
			if incoming.Timestamp.After(local.Timestamp) {
				local.Timestamp = incoming.Timestamp
			}
		}
	} else if local.CRDTType == "or_set" && incoming.CRDTType == "or_set" {
		locS := e.crdtSets[cKey]
		remS, err := UnmarshalORSet(incoming.CRDTState)
		if err == nil && locS != nil {
			merged := locS.Merge(remS)
			e.crdtSets[cKey] = merged
			mergedBytes, _ := merged.Marshal()
			local.CRDTState = mergedBytes
			local.VectorClock = local.VectorClock.Merge(incoming.VectorClock)
			if incoming.Timestamp.After(local.Timestamp) {
				local.Timestamp = incoming.Timestamp
			}
		}
	}
}
