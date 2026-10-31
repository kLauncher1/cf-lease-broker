package replication

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// GeoReplicationEngine coordinates multi-master asynchronous KV replication and CRDT state synchronization.
type GeoReplicationEngine struct {
	mu             sync.RWMutex
	config         ReplicationConfig
	currentClock   VectorClock
	store          map[string]*ReplicationEntry
	crdtCounters   map[string]*PNCounter
	crdtSets       map[string]*ORSet
	peers          map[string]*PeerNode
	sessionTracker *SessionTracker
	hmacKey        []byte
	stopChan       chan struct{}
	running        atomic.Bool

	// Atomic Telemetry Counters
	syncPacketsSent    int64
	syncPacketsRecv    int64
	conflictsDetected  int64
	conflictsResolved  int64
	crdtOperations     int64
	readYourWritesHits int64
	monotonicReadHits  int64
	lastSyncTime       int64 // UnixNano
}

// NewGeoReplicationEngine instantiates a new geo-replication and eventual consistency engine.
func NewGeoReplicationEngine(cfg ReplicationConfig) *GeoReplicationEngine {
	if cfg.LocalNodeID == "" {
		cfg.LocalNodeID = "node-edge-local"
	}
	if cfg.Region == "" {
		cfg.Region = "ord"
	}
	if cfg.SyncInterval <= 0 {
		cfg.SyncInterval = 5 * time.Second
	}
	if cfg.MaxBatchSize <= 0 {
		cfg.MaxBatchSize = 1000
	}
	if cfg.ClientWaitLimit <= 0 {
		cfg.ClientWaitLimit = 100 * time.Millisecond
	}

	secret := cfg.HMACSecret
	if secret == "" {
		secret = "cf-lease-broker-replication-secret-2026"
	}

	eng := &GeoReplicationEngine{
		config:         cfg,
		currentClock:   NewVectorClock(),
		store:          make(map[string]*ReplicationEntry),
		crdtCounters:   make(map[string]*PNCounter),
		crdtSets:       make(map[string]*ORSet),
		peers:          make(map[string]*PeerNode),
		sessionTracker: NewSessionTracker(),
		hmacKey:        []byte(secret),
		stopChan:       make(chan struct{}),
	}
	eng.currentClock.Increment(cfg.LocalNodeID)
	return eng
}

func compositeKey(namespace, key string) string {
	return fmt.Sprintf("%s:%s", namespace, key)
}

// Start launches the background periodic peer synchronization loop.
func (e *GeoReplicationEngine) Start() {
	if e.running.Swap(true) {
		return
	}
	go e.runSyncLoop()
}

// Stop cleanly terminates the replication synchronization loop.
func (e *GeoReplicationEngine) Stop() {
	if !e.running.Swap(false) {
		return
	}
	close(e.stopChan)
}

func (e *GeoReplicationEngine) runSyncLoop() {
	ticker := time.NewTicker(e.config.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopChan:
			return
		case <-ticker.C:
			e.sessionTracker.PruneIdleSessions(10 * time.Minute)
		}
	}
}

// Put writes or updates a key-value pair, advancing the local vector clock and client session.
func (e *GeoReplicationEngine) Put(namespace, key string, val []byte, clientCtx *ClientSessionContext) (*ReplicationEntry, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.currentClock.Increment(e.config.LocalNodeID)
	clockSnapshot := e.currentClock.Clone()

	now := time.Now().UTC()
	tenantID := ""
	if clientCtx != nil {
		tenantID = clientCtx.TenantID
	}

	cKey := compositeKey(namespace, key)
	entry := &ReplicationEntry{
		Namespace:   namespace,
		Key:         key,
		Value:       append([]byte(nil), val...),
		VectorClock: clockSnapshot,
		Timestamp:   now,
		NodeID:      e.config.LocalNodeID,
		TenantID:    tenantID,
		Deleted:     false,
	}
	e.store[cKey] = entry

	if clientCtx != nil {
		e.sessionTracker.RecordWrite(clientCtx.ClientID, tenantID, clockSnapshot)
	}

	return entry, nil
}

// Get fetches a key with read-your-writes and monotonic-read consistency verification.
func (e *GeoReplicationEngine) Get(namespace, key string, clientCtx *ClientSessionContext) (*ReplicationEntry, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	cKey := compositeKey(namespace, key)
	entry, exists := e.store[cKey]

	// Verify consistency guarantees against the client session
	if clientCtx != nil {
		if err := e.sessionTracker.VerifyConsistency(clientCtx, entry, e.currentClock); err != nil {
			return nil, err
		}
		if clientCtx.RequestedConsistency == ConsistencyReadYourWrites {
			atomic.AddInt64(&e.readYourWritesHits, 1)
		} else if clientCtx.RequestedConsistency == ConsistencyMonotonicRead {
			atomic.AddInt64(&e.monotonicReadHits, 1)
		}
	}

	if !exists || entry.Deleted {
		return nil, ErrKeyNotFound
	}

	if clientCtx != nil {
		e.sessionTracker.RecordRead(clientCtx.ClientID, entry.VectorClock)
	}

	return entry, nil
}

// Delete marks a key as a tombstone, advancing the local vector clock.
func (e *GeoReplicationEngine) Delete(namespace, key string, clientCtx *ClientSessionContext) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.currentClock.Increment(e.config.LocalNodeID)
	clockSnapshot := e.currentClock.Clone()

	cKey := compositeKey(namespace, key)
	entry, exists := e.store[cKey]
	if !exists || entry.Deleted {
		return ErrKeyNotFound
	}

	entry.Deleted = true
	entry.Value = nil
	entry.VectorClock = clockSnapshot
	entry.Timestamp = time.Now().UTC()
	entry.NodeID = e.config.LocalNodeID

	if clientCtx != nil {
		e.sessionTracker.RecordWrite(clientCtx.ClientID, clientCtx.TenantID, clockSnapshot)
	}
	return nil
}

// UpdateCounter mutates a distributed PN-Counter CRDT with delta.
func (e *GeoReplicationEngine) UpdateCounter(namespace, key string, delta int64, clientCtx *ClientSessionContext) (int64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	cKey := compositeKey(namespace, key)
	cnt, ok := e.crdtCounters[cKey]
	if !ok {
		cnt = NewPNCounter()
		e.crdtCounters[cKey] = cnt
	}

	cnt.Inc(e.config.LocalNodeID, delta)
	atomic.AddInt64(&e.crdtOperations, 1)

	e.currentClock.Increment(e.config.LocalNodeID)
	clockSnapshot := e.currentClock.Clone()

	// Reflect state in store entry
	crdtBytes, _ := cnt.Marshal()
	entry := &ReplicationEntry{
		Namespace:   namespace,
		Key:         key,
		VectorClock: clockSnapshot,
		Timestamp:   time.Now().UTC(),
		NodeID:      e.config.LocalNodeID,
		IsCRDT:      true,
		CRDTType:    "pn_counter",
		CRDTState:   crdtBytes,
	}
	e.store[cKey] = entry

	if clientCtx != nil {
		e.sessionTracker.RecordWrite(clientCtx.ClientID, clientCtx.TenantID, clockSnapshot)
	}

	return cnt.Value(), nil
}

// GetCounter retrieves the aggregated value of a PN-Counter CRDT.
func (e *GeoReplicationEngine) GetCounter(namespace, key string) (int64, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	cKey := compositeKey(namespace, key)
	cnt, ok := e.crdtCounters[cKey]
	if !ok {
		return 0, false
	}
	return cnt.Value(), true
}

// UpdateSet performs an Add or Remove operation on an Observed-Removed Set CRDT.
func (e *GeoReplicationEngine) UpdateSet(namespace, key, op, elem string, clientCtx *ClientSessionContext) ([]string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	cKey := compositeKey(namespace, key)
	set, ok := e.crdtSets[cKey]
	if !ok {
		set = NewORSet()
		e.crdtSets[cKey] = set
	}

	switch CRDTOpType(op) {
	case CRDTSetAdd:
		set.Add(elem, "")
	case CRDTSetRemove:
		set.Remove(elem)
	default:
		return nil, ErrInvalidCRDTOperation
	}

	atomic.AddInt64(&e.crdtOperations, 1)
	e.currentClock.Increment(e.config.LocalNodeID)
	clockSnapshot := e.currentClock.Clone()

	crdtBytes, _ := set.Marshal()
	entry := &ReplicationEntry{
		Namespace:   namespace,
		Key:         key,
		VectorClock: clockSnapshot,
		Timestamp:   time.Now().UTC(),
		NodeID:      e.config.LocalNodeID,
		IsCRDT:      true,
		CRDTType:    "or_set",
		CRDTState:   crdtBytes,
	}
	e.store[cKey] = entry

	if clientCtx != nil {
		e.sessionTracker.RecordWrite(clientCtx.ClientID, clientCtx.TenantID, clockSnapshot)
	}

	return set.Elements(), nil
}

// GetSet returns the active elements of an ORSet CRDT.
func (e *GeoReplicationEngine) GetSet(namespace, key string) ([]string, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	cKey := compositeKey(namespace, key)
	set, ok := e.crdtSets[cKey]
	if !ok {
		return nil, false
	}
	return set.Elements(), true
}

// RegisterPeer adds or updates a replica peer node.
func (e *GeoReplicationEngine) RegisterPeer(peer PeerNode) {
	e.mu.Lock()
	defer e.mu.Unlock()
	peer.LastSeen = time.Now().UTC()
	peer.Healthy = true
	e.peers[peer.NodeID] = &peer
}

// DeregisterPeer removes a peer node from the mesh.
func (e *GeoReplicationEngine) DeregisterPeer(nodeID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.peers, nodeID)
}

// ListPeers returns a slice of all registered replica peers.
func (e *GeoReplicationEngine) ListPeers() []PeerNode {
	e.mu.RLock()
	defer e.mu.RUnlock()

	res := make([]PeerNode, 0, len(e.peers))
	for _, p := range e.peers {
		res = append(res, *p)
	}
	sort.Slice(res, func(i, j int) bool {
		return res[i].NodeID < res[j].NodeID
	})
	return res
}

// Stats returns a snapshot of engine telemetry.
func (e *GeoReplicationEngine) Stats() ReplicationStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	lastSync := time.Unix(0, atomic.LoadInt64(&e.lastSyncTime))
	return ReplicationStats{
		LocalNodeID:         e.config.LocalNodeID,
		Region:              e.config.Region,
		CurrentClock:        e.currentClock.Clone(),
		TotalKeys:           int64(len(e.store)),
		SyncPacketsSent:     atomic.LoadInt64(&e.syncPacketsSent),
		SyncPacketsRecv:     atomic.LoadInt64(&e.syncPacketsRecv),
		ConflictsDetected:   atomic.LoadInt64(&e.conflictsDetected),
		ConflictsResolved:   atomic.LoadInt64(&e.conflictsResolved),
		CRDTOperations:      atomic.LoadInt64(&e.crdtOperations),
		ReadYourWritesHits:  atomic.LoadInt64(&e.readYourWritesHits),
		MonotonicReadHits:   atomic.LoadInt64(&e.monotonicReadHits),
		ActivePeerCount:     len(e.peers),
		LastSyncTime:        lastSync,
	}
}

// ResetStore clears the in-memory store for test teardown.
func (e *GeoReplicationEngine) ResetStore() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.store = make(map[string]*ReplicationEntry)
	e.crdtCounters = make(map[string]*PNCounter)
	e.crdtSets = make(map[string]*ORSet)
	e.currentClock = NewVectorClock()
	e.currentClock.Increment(e.config.LocalNodeID)
	atomic.StoreInt64(&e.conflictsDetected, 0)
	atomic.StoreInt64(&e.conflictsResolved, 0)
	atomic.StoreInt64(&e.crdtOperations, 0)
	atomic.StoreInt64(&e.readYourWritesHits, 0)
	atomic.StoreInt64(&e.monotonicReadHits, 0)
}
