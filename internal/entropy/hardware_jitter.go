package entropy

import (
	"crypto/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Zeroize cryptographically scrubs sensitive byte slices from memory.
func Zeroize(buf []byte) {
	for i := range buf {
		buf[i] = 0
	}
	runtime.KeepAlive(buf)
}

// SplitMix64 is a fast, 64-bit stateful mixing algorithm with excellent avalanche characteristics.
func SplitMix64(state *uint64) uint64 {
	*state += 0x9e3779b97f4a7c15
	z := *state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// HardwareJitterHarvester coordinates physical CPU timing jitter and hardware TRNG collection.
type HardwareJitterHarvester struct {
	mu           sync.RWMutex
	totalSamples uint64
	trngActive   bool
}

// NewHardwareJitterHarvester creates a new physical entropy harvester.
func NewHardwareJitterHarvester() *HardwareJitterHarvester {
	return &HardwareJitterHarvester{
		trngActive: true,
	}
}

// SamplePureJitter collects entropy exclusively from execution timing differences and scheduler jitter.
func (h *HardwareJitterHarvester) SamplePureJitter(sampleCount int) []byte {
	if sampleCount <= 0 {
		sampleCount = 512
	}
	raw := make([]byte, sampleCount)
	var state uint64 = uint64(time.Now().UnixNano())

	for i := 0; i < sampleCount; i++ {
		t0 := time.Now().UnixNano()
		// Micro-spin with variable iteration to trigger instruction pipeline and cache timing variances
		spinLimit := (i % 7) + 3
		for j := 0; j < spinLimit; j++ {
			runtime.Gosched()
		}
		t1 := time.Now().UnixNano()
		delta := uint64(t1 - t0)

		// Accumulate CPU timer delta into state and mix via SplitMix64
		state ^= delta + 0x9e3779b97f4a7c15 + (state << 6) + (state >> 2)
		mixed := SplitMix64(&state)

		raw[i] = byte(mixed ^ (delta & 0xFF))
	}

	atomic.AddUint64(&h.totalSamples, uint64(sampleCount))
	return raw
}

// SampleHardwareTRNG collects raw bytes from the underlying operating system and hardware CSPRNG.
func (h *HardwareJitterHarvester) SampleHardwareTRNG(sampleCount int) ([]byte, error) {
	if sampleCount <= 0 {
		sampleCount = 512
	}
	buf := make([]byte, sampleCount)
	_, err := rand.Read(buf)
	h.mu.Lock()
	h.trngActive = (err == nil)
	h.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return buf, nil
}

// SampleHardwareJitter collects and mixes entropy from both CPU jitter and hardware TRNG pipelines.
func (h *HardwareJitterHarvester) SampleHardwareJitter(sampleCount int) (*SampleBatch, error) {
	if sampleCount <= 0 {
		sampleCount = 512
	}

	start := time.Now()

	// 1. Collect hardware TRNG entropy
	trngBuf, trngErr := h.SampleHardwareTRNG(sampleCount)

	// 2. Collect CPU execution timing jitter
	jitterBuf := h.SamplePureJitter(sampleCount)

	mixed := make([]byte, sampleCount)
	var source EntropySource

	if trngErr == nil && len(trngBuf) == sampleCount {
		source = SourceHybrid
		var mixState uint64 = uint64(start.UnixNano())
		for i := 0; i < sampleCount; i++ {
			// XOR hardware TRNG with physical CPU jitter and pass through SplitMix64 whitening
			combined := uint64(trngBuf[i]) ^ uint64(jitterBuf[i])
			mixState ^= combined
			whitened := SplitMix64(&mixState)
			mixed[i] = byte(whitened ^ combined)
		}
	} else {
		// Fallback to pure jitter if hardware TRNG read fails
		source = SourceCPUJitter
		copy(mixed, jitterBuf)
	}

	// 3. Immediately zeroize raw intermediate buffers
	if trngBuf != nil {
		Zeroize(trngBuf)
	}
	Zeroize(jitterBuf)

	durationUs := time.Since(start).Microseconds()
	if durationUs <= 0 {
		durationUs = 1
	}

	return &SampleBatch{
		RawSamples:  mixed,
		Source:      source,
		SampleCount: sampleCount,
		Timestamp:   start,
		DurationUs:  durationUs,
	}, nil
}

// TotalSamples returns the cumulative count of entropy samples collected.
func (h *HardwareJitterHarvester) TotalSamples() uint64 {
	return atomic.LoadUint64(&h.totalSamples)
}

// IsTRNGActive returns true if the OS hardware TRNG is functioning normally.
func (h *HardwareJitterHarvester) IsTRNGActive() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.trngActive
}
