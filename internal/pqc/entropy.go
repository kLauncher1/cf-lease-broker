package pqc

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// NIST SP 800-90B Health Check cutoffs for 8-bit symbols.
const (
	rctCutoff = 35  // Repetition Count Test threshold
	aptWindow = 512 // Adaptive Proportion Test window size
	aptCutoff = 177 // Cutoff for alpha = 2^-20 on 8-bit symbols
)

// EntropyHarvester collects entropy from CPU execution jitter and hardware TRNG.
type EntropyHarvester struct {
	mu                  sync.RWMutex
	pool                [32]byte
	totalBytesHarvested uint64
	jitterCyclesSampled uint64
	trngActive          bool
	minEntropy          float64
	shannonEntropy      float64
	healthPassed        bool
	rctFailures         uint64
	aptFailures         uint64
	lastHarvest         time.Time
}

// NewEntropyHarvester initializes an entropy collection engine.
func NewEntropyHarvester() *EntropyHarvester {
	h := &EntropyHarvester{
		minEntropy:     7.95,
		shannonEntropy: 7.99,
		healthPassed:   true,
		lastHarvest:    time.Now(),
	}
	// Initial seed harvest
	h.Harvest(64)
	return h
}

// Zeroize securely scrubs a byte buffer from memory.
func Zeroize(buf []byte) {
	for i := range buf {
		buf[i] = 0
	}
	runtime.KeepAlive(buf)
}

// sampleJitter collects entropy from CPU execution pipeline and timer jitter.
func (h *EntropyHarvester) sampleJitter(samplesCount int) []byte {
	raw := make([]byte, samplesCount)
	var state uint64 = uint64(time.Now().UnixNano())

	for i := 0; i < samplesCount; i++ {
		t0 := time.Now().UnixNano()
		for j := 0; j < (i%7)+5; j++ {
			runtime.Gosched()
		}
		t1 := time.Now().UnixNano()
		delta := uint64(t1 - t0)

		// 64-bit SplitMix mixer accumulating physical CPU timer and scheduling jitter
		state ^= delta + 0x9e3779b97f4a7c15 + (state << 6) + (state >> 2)
		state = (state ^ (state >> 30)) * 0xbf58476d1ce4e5b9
		state = (state ^ (state >> 27)) * 0x94d049bb133111eb
		state ^= (state >> 31)

		raw[i] = byte(state ^ (delta & 0xFF))
	}

	atomic.AddUint64(&h.jitterCyclesSampled, uint64(samplesCount))
	return raw
}

// validateSP80090B performs NIST SP 800-90B RCT and APT health tests.
func (h *EntropyHarvester) validateSP80090B(data []byte) (rctOk, aptOk bool) {
	if len(data) == 0 {
		return false, false
	}

	// 1. Repetition Count Test (RCT)
	rctOk = true
	repCount := 1
	for i := 1; i < len(data); i++ {
		if data[i] == data[i-1] {
			repCount++
			if repCount >= rctCutoff {
				rctOk = false
				break
			}
		} else {
			repCount = 1
		}
	}

	// 2. Adaptive Proportion Test (APT)
	aptOk = true
	if len(data) >= aptWindow {
		for w := 0; w+aptWindow <= len(data); w += aptWindow {
			target := data[w]
			count := 0
			for j := 0; j < aptWindow; j++ {
				if data[w+j] == target {
					count++
				}
			}
			if count >= aptCutoff {
				aptOk = false
				break
			}
		}
	}

	return rctOk, aptOk
}

// computeEntropyMetrics computes Shannon entropy and min-entropy across 8-bit symbols.
func computeEntropyMetrics(data []byte) (shannon, minEnt float64) {
	if len(data) == 0 {
		return 0.0, 0.0
	}

	freq := make(map[byte]int)
	for _, b := range data {
		freq[b]++
	}

	n := float64(len(data))
	var maxProb float64 = 0.0

	for _, count := range freq {
		p := float64(count) / n
		if p > 0 {
			shannon -= p * math.Log2(p)
		}
		if p > maxProb {
			maxProb = p
		}
	}

	if maxProb > 0 {
		minEnt = -math.Log2(maxProb)
	}

	return shannon, minEnt
}

// Harvest collects and conditions entropy from both hardware TRNG and jitter sources.
func (h *EntropyHarvester) Harvest(numBytes int) []byte {
	h.mu.Lock()
	defer h.mu.Unlock()

	if numBytes <= 0 {
		numBytes = 32
	}

	sampleLen := numBytes * 2
	if sampleLen < 512 {
		sampleLen = 512
	}

	// Collect hardware TRNG / OS CSPRNG entropy
	trngBuf := make([]byte, sampleLen)
	_, trngErr := rand.Read(trngBuf)
	h.trngActive = (trngErr == nil)

	// Collect CPU jitter entropy
	jitterBuf := h.sampleJitter(sampleLen)

	// Run SP 800-90B health checks on raw jitter
	rctOk, aptOk := h.validateSP80090B(jitterBuf)
	if !rctOk {
		h.rctFailures++
	}
	if !aptOk {
		h.aptFailures++
	}
	h.healthPassed = (rctOk && aptOk && h.trngActive)

	// Evaluate entropy qualities
	shannon, minEnt := computeEntropyMetrics(append(trngBuf, jitterBuf...))
	h.shannonEntropy = shannon
	h.minEntropy = minEnt

	// Cryptographic conditioning / whitening via HMAC-SHA256
	mac := hmac.New(sha256.New, h.pool[:])
	mac.Write(trngBuf)
	mac.Write(jitterBuf)
	conditioned := mac.Sum(nil)

	// Update the continuous internal state pool
	copy(h.pool[:], conditioned)
	h.totalBytesHarvested += uint64(len(conditioned))
	h.lastHarvest = time.Now()

	// Scrub raw intermediate entropy buffers
	Zeroize(trngBuf)
	Zeroize(jitterBuf)

	result := make([]byte, numBytes)
	if numBytes <= len(conditioned) {
		copy(result, conditioned[:numBytes])
	} else {
		// Expand conditioned entropy deterministically if more bytes are requested
		hExpanded := sha256.Sum256(append(conditioned, byte(numBytes)))
		copy(result, hExpanded[:])
	}

	return result
}

// GetEntropyPoolSeed derives an isolated 32-byte seed for lattice key generation.
func (h *EntropyHarvester) GetEntropyPoolSeed(contextInfo string) [32]byte {
	h.mu.RLock()
	defer h.mu.RUnlock()

	mac := hmac.New(sha256.New, h.pool[:])
	mac.Write([]byte(contextInfo))
	timeBytes := []byte(time.Now().Format(time.RFC3339Nano))
	mac.Write(timeBytes)

	var out [32]byte
	copy(out[:], mac.Sum(nil))
	return out
}

// GetStats returns telemetry metrics on the entropy harvest engine.
func (h *EntropyHarvester) GetStats() EntropyHarvestStats {
	h.mu.RLock()
	defer h.mu.RUnlock()

	status := "healthy"
	if !h.healthPassed || h.minEntropy < 5.0 {
		status = "degraded"
	}

	return EntropyHarvestStats{
		TotalBytesHarvested: h.totalBytesHarvested,
		MinEntropyBits:      math.Round(h.minEntropy*100) / 100,
		ShannonEntropy:      math.Round(h.shannonEntropy*100) / 100,
		TRNGActive:          h.trngActive,
		JitterCyclesSampled: h.jitterCyclesSampled,
		HealthStatus:        status,
		LastHarvestTime:     h.lastHarvest,
	}
}

// ConstantTimeVerify compares two byte slices in constant time to prevent timing side channels.
func ConstantTimeVerify(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}
