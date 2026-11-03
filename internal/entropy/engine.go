package entropy

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// EntropyEngine unifies physical entropy harvesting, NIST continuous health testing,
// uniform randomness extraction, and quantum beacon round advancement.
type EntropyEngine struct {
	mu           sync.RWMutex
	config       EntropyConfig
	harvester    *HardwareJitterHarvester
	evaluator    *NISTHealthEvaluator
	extractor    *CryptographicExtractor
	beacon       *QuantumEntropyBeacon
	entropyPool  []byte
	lastHealth   NISTHealthReport
	startTime    time.Time
	stopChan     chan struct{}
	wg           sync.WaitGroup
	running      bool

	// Monotonic telemetry counters
	totalHarvestedBytes  uint64
	totalTokensGenerated uint64
	rctFailures          uint64
	aptFailures          uint64
}

// NewEntropyEngine initializes and seeds a new unified entropy subsystem.
func NewEntropyEngine(cfg EntropyConfig) *EntropyEngine {
	harvester := NewHardwareJitterHarvester()
	evaluator := NewNISTHealthEvaluator(cfg)
	extractor := NewCryptographicExtractor(DefaultExtractorSalt)
	beacon := NewQuantumEntropyBeacon(cfg.AttestationKey, cfg.MaxClockSkew)

	e := &EntropyEngine{
		config:      cfg,
		harvester:   harvester,
		evaluator:   evaluator,
		extractor:   extractor,
		beacon:      beacon,
		entropyPool: make([]byte, 0, cfg.PoolCapacity),
		startTime:   time.Now(),
		stopChan:    make(chan struct{}),
	}

	// Initial warm-up harvest and genesis beacon step
	_ = e.replenishAndStep()
	return e
}

// replenishAndStep harvests a physical batch, validates health, extracts entropy, and steps the beacon.
func (e *EntropyEngine) replenishAndStep() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	sampleCount := e.config.JitterSampleCount
	if sampleCount <= 0 {
		sampleCount = 512
	}

	batch, err := e.harvester.SampleHardwareJitter(sampleCount)
	if err != nil {
		return fmt.Errorf("entropy harvest error: %w", err)
	}

	// 1. NIST SP 800-90B continuous health testing
	report := e.evaluator.Evaluate(batch.RawSamples)
	e.lastHealth = report
	if !report.RCTPassed {
		atomic.AddUint64(&e.rctFailures, 1)
	}
	if !report.APTPassed {
		atomic.AddUint64(&e.aptFailures, 1)
	}

	// 2. Randomness extraction (HKDF Leftover Hash Lemma)
	extracted, err := e.extractor.Extract(batch.RawSamples, "beacon-replenish", 64)
	if err != nil {
		return fmt.Errorf("entropy extraction error: %w", err)
	}
	defer Zeroize(extracted)

	// 3. Replenish bounded entropy pool
	atomic.AddUint64(&e.totalHarvestedBytes, uint64(len(batch.RawSamples)))
	availableCap := e.config.PoolCapacity - len(e.entropyPool)
	if availableCap > 0 {
		toAdd := len(extracted)
		if toAdd > availableCap {
			toAdd = availableCap
		}
		e.entropyPool = append(e.entropyPool, extracted[:toAdd]...)
	}

	// 4. Advance quantum entropy beacon round
	minBits := report.MinEntropy
	if minBits <= 0 {
		minBits = 7.92
	}
	_, err = e.beacon.NextRound(extracted, batch.Source, minBits)
	return err
}

// Start launches the background replenishment loop and periodic beacon ticker.
func (e *EntropyEngine) Start(ctx context.Context) error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return errors.New("entropy engine: already running")
	}
	e.running = true
	e.stopChan = make(chan struct{})
	e.mu.Unlock()

	interval := e.config.BeaconInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-e.stopChan:
				return
			case <-ticker.C:
				_ = e.replenishAndStep()
			}
		}
	}()

	return nil
}

// Stop cleanly terminates background replenishment routines.
func (e *EntropyEngine) Stop() error {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return nil
	}
	e.running = false
	close(e.stopChan)
	e.mu.Unlock()

	e.wg.Wait()
	return nil
}

// GenerateToken extracts cryptographic uniform random bytes for tenant leases or API tokens.
func (e *EntropyEngine) GenerateToken(byteLength int, purpose string) ([]byte, error) {
	if byteLength <= 0 {
		byteLength = 32
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// If pool is shallow, perform synchronous on-demand harvest
	if len(e.entropyPool) < byteLength {
		sampleBatch, err := e.harvester.SampleHardwareJitter(e.config.JitterSampleCount)
		if err == nil && len(sampleBatch.RawSamples) > 0 {
			fresh, extractErr := e.extractor.Extract(sampleBatch.RawSamples, purpose, byteLength*2)
			if extractErr == nil {
				e.entropyPool = append(e.entropyPool, fresh...)
				Zeroize(fresh)
			}
		}
	}

	var raw []byte
	if len(e.entropyPool) >= byteLength {
		raw = append([]byte(nil), e.entropyPool[:byteLength]...)
		e.entropyPool = e.entropyPool[byteLength:]
	} else {
		// Fallback sampling if pool was empty
		jitter := e.harvester.SamplePureJitter(byteLength * 2)
		raw = append([]byte(nil), jitter[:byteLength]...)
		Zeroize(jitter)
	}

	// Condition token via HKDF-Expand with tenant purpose info string
	token, err := e.extractor.Extract(raw, purpose, byteLength)
	Zeroize(raw)
	if err != nil {
		return nil, fmt.Errorf("entropy token generation failed: %w", err)
	}

	atomic.AddUint64(&e.totalTokensGenerated, 1)
	return token, nil
}

// GetBeaconRound returns the current quantum beacon round state.
func (e *EntropyEngine) GetBeaconRound() *BeaconRound {
	return e.beacon.GetCurrentRound()
}

// GetBeaconHistory retrieves recent beacon rounds.
func (e *EntropyEngine) GetBeaconHistory(limit int) []BeaconRound {
	return e.beacon.GetHistory(limit)
}

// VerifyBeaconRound verifies a candidate beacon round against the engine's beacon state.
func (e *EntropyEngine) VerifyBeaconRound(round *BeaconRound, expectedPrevHash string, verificationKey []byte) (bool, error) {
	return e.beacon.VerifyRound(round, expectedPrevHash, verificationKey)
}

// GetHealthReport returns the latest SP 800-90B health assessment.
func (e *EntropyEngine) GetHealthReport() NISTHealthReport {
	e.mu.RLock()
	report := e.lastHealth
	e.mu.RUnlock()

	if !report.EvaluatedAt.IsZero() {
		return report
	}

	jitter := e.harvester.SamplePureJitter(e.config.JitterSampleCount)
	defer Zeroize(jitter)
	return e.evaluator.Evaluate(jitter)
}

// GetStats returns snapshot operational statistics.
func (e *EntropyEngine) GetStats() EntropyStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	curRound := e.beacon.GetCurrentRound()
	var roundNum uint64
	var lastHash string
	if curRound != nil {
		roundNum = curRound.Round
		lastHash = curRound.OutputHash
	}

	uptime := int64(time.Since(e.startTime).Seconds())
	if uptime <= 0 {
		uptime = 1
	}

	return EntropyStats{
		TotalHarvestedBytes:  atomic.LoadUint64(&e.totalHarvestedBytes),
		TotalTokensGenerated: atomic.LoadUint64(&e.totalTokensGenerated),
		CurrentPoolSize:      len(e.entropyPool),
		BeaconCurrentRound:   roundNum,
		BeaconLastOutputHash: lastHash,
		ActiveSource:         SourceHybrid,
		LastHealthReport:     e.lastHealth,
		UptimeSeconds:        uptime,
		RCTFailureCount:      atomic.LoadUint64(&e.rctFailures),
		APTFailureCount:      atomic.LoadUint64(&e.aptFailures),
	}
}

// Reset clears accumulated stats and re-seeds the entropy pool from fresh hardware jitter.
func (e *EntropyEngine) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()

	Zeroize(e.entropyPool)
	e.entropyPool = e.entropyPool[:0]
	atomic.StoreUint64(&e.totalHarvestedBytes, 0)
	atomic.StoreUint64(&e.totalTokensGenerated, 0)
	atomic.StoreUint64(&e.rctFailures, 0)
	atomic.StoreUint64(&e.aptFailures, 0)
	e.startTime = time.Now()

	// Re-harvest initial seed
	batch, _ := e.harvester.SampleHardwareJitter(e.config.JitterSampleCount)
	if batch != nil && len(batch.RawSamples) > 0 {
		extracted, err := e.extractor.Extract(batch.RawSamples, "reset-seed", 64)
		if err == nil {
			e.entropyPool = append(e.entropyPool, extracted...)
			Zeroize(extracted)
		}
	}
}
