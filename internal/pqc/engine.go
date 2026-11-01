package pqc

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// PQCEngine coordinates post-quantum key encapsulation, digital signatures, and entropy.
type PQCEngine struct {
	mu                  sync.RWMutex
	config              PQCConfig
	sessions            map[string]*KeyExchangeSession
	kemKeyPairs         map[string]*KEMKeyPair
	dsaKeyPairs         map[string]*MLDSAKeyPair
	peerDSAPubKeys      map[string][]byte
	counterTracker      *StateCounterTracker
	harvester           *EntropyHarvester
	localCounter        uint64
	totalKeyExchanges   uint64
	totalEncapsulations uint64
	totalDecapsulations uint64
	totalSignatures     uint64
	totalVerified       uint64
	failedVerifications uint64
	totalHandshakeUs    int64
	stopChan            chan struct{}
	running             bool
}

// NewPQCEngine creates a post-quantum cryptographic engine with default configuration.
func NewPQCEngine(cfg PQCConfig) *PQCEngine {
	e := &PQCEngine{
		config:         cfg,
		sessions:       make(map[string]*KeyExchangeSession),
		kemKeyPairs:    make(map[string]*KEMKeyPair),
		dsaKeyPairs:    make(map[string]*MLDSAKeyPair),
		peerDSAPubKeys: make(map[string][]byte),
		counterTracker: NewStateCounterTracker(),
		harvester:      NewEntropyHarvester(),
		stopChan:       make(chan struct{}),
	}

	e.seedDefaultKeys()
	return e
}

// seedDefaultKeys seeds initial demonstration ML-DSA and ML-KEM keys for edge peering.
func (e *PQCEngine) seedDefaultKeys() {
	seed := e.harvester.GetEntropyPoolSeed("initial-default-mldsa-key")
	kp65, err := GenerateMLDSAKeyPair("broker-edge-pqc-root", AlgorithmMLDSA65, seed)
	if err == nil {
		e.RegisterMLDSAKey(kp65)
	}

	seed87 := e.harvester.GetEntropyPoolSeed("initial-default-mldsa87-key")
	kp87, err := GenerateMLDSAKeyPair("broker-edge-pqc-highsec", AlgorithmMLDSA87, seed87)
	if err == nil {
		e.RegisterMLDSAKey(kp87)
	}
}

// Start launches background entropy harvesting and session maintenance loops.
func (e *PQCEngine) Start() {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return
	}
	e.running = true
	e.stopChan = make(chan struct{})
	e.mu.Unlock()

	go e.runMaintenanceLoop()
}

// Stop terminates background maintenance goroutines.
func (e *PQCEngine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return
	}
	e.running = false
	close(e.stopChan)
}

func (e *PQCEngine) runMaintenanceLoop() {
	ticker := time.NewTicker(e.config.HarvestInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopChan:
			return
		case <-ticker.C:
			// Continuous entropy harvest
			_ = e.harvester.Harvest(32)
			e.PruneExpiredSessions()
		}
	}
}

// PruneExpiredSessions removes expired key encapsulation sessions.
func (e *PQCEngine) PruneExpiredSessions() int {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	pruned := 0
	for id, sess := range e.sessions {
		if now.After(sess.ExpiresAt) {
			delete(e.sessions, id)
			delete(e.kemKeyPairs, id)
			pruned++
		}
	}
	return pruned
}

// CreateKEMSession creates a new server-side KEM keypair and registers an active session.
func (e *PQCEngine) CreateKEMSession(algorithm string, hybrid bool) (*KeyExchangeSession, error) {
	start := time.Now()
	if algorithm == "" {
		algorithm = e.config.DefaultKEMAlgorithm
	}

	seed := e.harvester.GetEntropyPoolSeed("kem-session-keygen")
	kp, err := GenerateKEMKeyPair(algorithm, seed, hybrid)
	if err != nil {
		return nil, fmt.Errorf("failed to generate KEM keypair: %w", err)
	}

	sessionIDBytes := make([]byte, 16)
	_, _ = rand.Read(sessionIDBytes)
	sessionID := hex.EncodeToString(sessionIDBytes)

	now := time.Now()
	sess := &KeyExchangeSession{
		SessionID:          sessionID,
		Algorithm:          algorithm,
		ServerPublicKey:    hex.EncodeToString(kp.PublicKeyBytes),
		HybridActive:       hybrid,
		HandshakeLatencyUs: time.Since(start).Microseconds(),
		CreatedAt:          now,
		ExpiresAt:          now.Add(e.config.SessionTTL),
	}

	e.mu.Lock()
	e.sessions[sessionID] = sess
	e.kemKeyPairs[sessionID] = kp
	atomic.AddUint64(&e.totalKeyExchanges, 1)
	atomic.AddInt64(&e.totalHandshakeUs, sess.HandshakeLatencyUs)
	e.mu.Unlock()

	return sess, nil
}

// EncapsulateAgainstPublicKey encaps shares with a remote peer's public key.
func (e *PQCEngine) EncapsulateAgainstPublicKey(algorithm string, peerPubKeyHex string, hybrid bool) (*KEMEncapsulationResult, int64, error) {
	start := time.Now()
	if algorithm == "" {
		algorithm = e.config.DefaultKEMAlgorithm
	}

	peerPubKey, err := hex.DecodeString(peerPubKeyHex)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid hex peer public key: %w", err)
	}

	seed := e.harvester.Harvest(32)
	res, err := Encapsulate(algorithm, peerPubKey, seed, hybrid)
	if err != nil {
		return nil, 0, err
	}

	latencyUs := time.Since(start).Microseconds()
	atomic.AddUint64(&e.totalEncapsulations, 1)
	return res, latencyUs, nil
}

// DecapsulateSession extracts the shared secret for an existing session.
func (e *PQCEngine) DecapsulateSession(sessionID string, ciphertextHex string) ([]byte, int64, error) {
	start := time.Now()

	e.mu.RLock()
	sess, exists := e.sessions[sessionID]
	kp := e.kemKeyPairs[sessionID]
	e.mu.RUnlock()

	if !exists || kp == nil {
		return nil, 0, ErrSessionNotFound
	}

	ct, err := hex.DecodeString(ciphertextHex)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid ciphertext hex: %w", err)
	}

	secret, err := Decapsulate(sess.Algorithm, kp, ct, sess.HybridActive)
	if err != nil {
		return nil, 0, err
	}

	latencyUs := time.Since(start).Microseconds()

	e.mu.Lock()
	sess.Ciphertext = ciphertextHex
	sess.SharedSecretHex = hex.EncodeToString(secret)
	sess.HandshakeLatencyUs = latencyUs
	atomic.AddUint64(&e.totalDecapsulations, 1)
	e.mu.Unlock()

	return secret, latencyUs, nil
}

// RegisterMLDSAKey stores a local ML-DSA signing key.
func (e *PQCEngine) RegisterMLDSAKey(kp *MLDSAKeyPair) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.dsaKeyPairs[kp.KeyID] = kp
	e.peerDSAPubKeys[kp.KeyID] = kp.PublicKey
}

// RegisterPeerDSAPublicKey stores a trusted peer broker's ML-DSA public key.
func (e *PQCEngine) RegisterPeerDSAPublicKey(keyID string, pubKey []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.peerDSAPubKeys[keyID] = pubKey
}

// SignVoucher signs an inter-broker lease voucher with the designated ML-DSA key.
func (e *PQCEngine) SignVoucher(keyID string, voucherData any) (*VoucherSignature, error) {
	e.mu.RLock()
	kp, exists := e.dsaKeyPairs[keyID]
	e.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrKeyNotFound, keyID)
	}

	counter := atomic.AddUint64(&e.localCounter, 1)
	seed := e.harvester.Harvest(32)

	sig, err := SignLeaseVoucher(kp, voucherData, counter, seed)
	if err != nil {
		return nil, err
	}

	atomic.AddUint64(&e.totalSignatures, 1)
	return sig, nil
}

// VerifyVoucher verifies the ML-DSA signature over an inter-broker voucher.
func (e *PQCEngine) VerifyVoucher(voucherData any, sig *VoucherSignature) bool {
	if sig == nil {
		atomic.AddUint64(&e.failedVerifications, 1)
		return false
	}

	e.mu.RLock()
	pubKey, exists := e.peerDSAPubKeys[sig.KeyID]
	e.mu.RUnlock()

	if !exists {
		atomic.AddUint64(&e.failedVerifications, 1)
		return false
	}

	ok := VerifyLeaseVoucher(pubKey, sig.Algorithm, voucherData, sig, e.counterTracker)
	if ok {
		atomic.AddUint64(&e.totalVerified, 1)
	} else {
		atomic.AddUint64(&e.failedVerifications, 1)
	}
	return ok
}

// HarvestEntropy forces an immediate collection of fresh quantum/jitter entropy.
func (e *PQCEngine) HarvestEntropy(numBytes int) []byte {
	return e.harvester.Harvest(numBytes)
}

// GetEntropyStats returns real-time metrics on the entropy pool.
func (e *PQCEngine) GetEntropyStats() EntropyHarvestStats {
	return e.harvester.GetStats()
}

// GetStats returns aggregate performance metrics.
func (e *PQCEngine) GetStats() PQCStats {
	e.mu.RLock()
	activeCount := len(e.sessions)
	totalKX := atomic.LoadUint64(&e.totalKeyExchanges)
	totalLat := atomic.LoadInt64(&e.totalHandshakeUs)
	e.mu.RUnlock()

	avgUs := 0.0
	if totalKX > 0 {
		avgUs = float64(totalLat) / float64(totalKX)
	}

	return PQCStats{
		TotalKeyExchanges:       totalKX,
		TotalEncapsulations:     atomic.LoadUint64(&e.totalEncapsulations),
		TotalDecapsulations:     atomic.LoadUint64(&e.totalDecapsulations),
		TotalSignaturesIssued:   atomic.LoadUint64(&e.totalSignatures),
		TotalSignaturesVerified: atomic.LoadUint64(&e.totalVerified),
		FailedVerifications:     atomic.LoadUint64(&e.failedVerifications),
		AverageHandshakeUs:      avgUs,
		ActiveSessionsCount:     activeCount,
	}
}

// Reset clears sessions and resets statistics.
func (e *PQCEngine) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.sessions = make(map[string]*KeyExchangeSession)
	e.kemKeyPairs = make(map[string]*KEMKeyPair)
	e.counterTracker = NewStateCounterTracker()
	atomic.StoreUint64(&e.totalKeyExchanges, 0)
	atomic.StoreUint64(&e.totalEncapsulations, 0)
	atomic.StoreUint64(&e.totalDecapsulations, 0)
	atomic.StoreUint64(&e.totalSignatures, 0)
	atomic.StoreUint64(&e.totalVerified, 0)
	atomic.StoreUint64(&e.failedVerifications, 0)
	atomic.StoreInt64(&e.totalHandshakeUs, 0)
}
