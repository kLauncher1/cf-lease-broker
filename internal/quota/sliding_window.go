package quota

import (
	"sync"
	"time"
)

// WindowStats contains telemetry data for a tracked sliding-window key.
type WindowStats struct {
	Key            string        `json:"key"`
	CurrentCount   int64         `json:"current_count"`
	Limit          int64         `json:"limit"`
	BurstRemaining int64         `json:"burst_remaining"`
	Window         time.Duration `json:"window"`
	ResetRemaining time.Duration `json:"reset_remaining"`
}

// LimiterConfig specifies rate limit thresholds and windows.
type LimiterConfig struct {
	WindowDuration time.Duration `json:"window_duration"`
	MaxRequests    int64         `json:"max_requests"`
	BurstAllowance int64         `json:"burst_allowance"`
}

type keyBucket struct {
	timestamps []time.Time
	lastAccess time.Time
}

// SlidingWindowLimiter provides thread-safe sliding-window rate tracking.
type SlidingWindowLimiter struct {
	mu     sync.RWMutex
	config LimiterConfig
	keys   map[string]*keyBucket
}

// NewSlidingWindowLimiter creates a new limiter with given window duration, limit, and burst.
func NewSlidingWindowLimiter(window time.Duration, maxRequests, burst int64) *SlidingWindowLimiter {
	if window <= 0 {
		window = time.Minute
	}
	if maxRequests <= 0 {
		maxRequests = 60
	}
	if burst < 0 {
		burst = 0
	}

	return &SlidingWindowLimiter{
		config: LimiterConfig{
			WindowDuration: window,
			MaxRequests:    maxRequests,
			BurstAllowance: burst,
		},
		keys: make(map[string]*keyBucket),
	}
}

// Allow checks if one request is permitted under the sliding-window limit for the given key.
func (l *SlidingWindowLimiter) Allow(key string) (bool, int64, time.Duration) {
	return l.AllowN(key, 1)
}

// AllowN checks if n requests are permitted under the sliding-window limit.
func (l *SlidingWindowLimiter) AllowN(key string, n int64) (bool, int64, time.Duration) {
	if n <= 0 {
		return true, 0, 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now().UTC()
	cutoff := now.Add(-l.config.WindowDuration)

	bucket, exists := l.keys[key]
	if !exists {
		bucket = &keyBucket{
			timestamps: make([]time.Time, 0, l.config.MaxRequests),
		}
		l.keys[key] = bucket
	}
	bucket.lastAccess = now

	// Prune timestamps older than cutoff
	validIdx := 0
	for i, ts := range bucket.timestamps {
		if ts.After(cutoff) {
			validIdx = i
			break
		}
		if i == len(bucket.timestamps)-1 {
			validIdx = len(bucket.timestamps)
		}
	}
	if validIdx > 0 {
		bucket.timestamps = bucket.timestamps[validIdx:]
	}

	currentCount := int64(len(bucket.timestamps))
	totalCapacity := l.config.MaxRequests + l.config.BurstAllowance

	if currentCount+n > totalCapacity {
		var resetIn time.Duration
		if len(bucket.timestamps) > 0 {
			oldest := bucket.timestamps[0]
			resetIn = oldest.Add(l.config.WindowDuration).Sub(now)
			if resetIn < 0 {
				resetIn = 0
			}
		} else {
			resetIn = l.config.WindowDuration
		}
		return false, currentCount, resetIn
	}

	for i := int64(0); i < n; i++ {
		bucket.timestamps = append(bucket.timestamps, now)
	}

	return true, currentCount + n, l.config.WindowDuration
}

// Record unconditionally tracks an event occurrence for the key.
func (l *SlidingWindowLimiter) Record(key string) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now().UTC()
	cutoff := now.Add(-l.config.WindowDuration)

	bucket, exists := l.keys[key]
	if !exists {
		bucket = &keyBucket{
			timestamps: make([]time.Time, 0, l.config.MaxRequests),
		}
		l.keys[key] = bucket
	}
	bucket.lastAccess = now

	validIdx := 0
	for i, ts := range bucket.timestamps {
		if ts.After(cutoff) {
			validIdx = i
			break
		}
		if i == len(bucket.timestamps)-1 {
			validIdx = len(bucket.timestamps)
		}
	}
	if validIdx > 0 {
		bucket.timestamps = bucket.timestamps[validIdx:]
	}

	bucket.timestamps = append(bucket.timestamps, now)
	return int64(len(bucket.timestamps))
}

// GetStats returns usage statistics for a specific key.
func (l *SlidingWindowLimiter) GetStats(key string) WindowStats {
	l.mu.RLock()
	defer l.mu.RUnlock()

	now := time.Now().UTC()
	cutoff := now.Add(-l.config.WindowDuration)

	stats := WindowStats{
		Key:            key,
		Limit:          l.config.MaxRequests,
		Window:         l.config.WindowDuration,
		BurstRemaining: l.config.BurstAllowance,
	}

	bucket, exists := l.keys[key]
	if !exists {
		return stats
	}

	validCount := int64(0)
	var oldestValid time.Time
	for _, ts := range bucket.timestamps {
		if ts.After(cutoff) {
			if oldestValid.IsZero() {
				oldestValid = ts
			}
			validCount++
		}
	}

	stats.CurrentCount = validCount
	burstUsed := validCount - l.config.MaxRequests
	if burstUsed > 0 {
		stats.BurstRemaining = l.config.BurstAllowance - burstUsed
		if stats.BurstRemaining < 0 {
			stats.BurstRemaining = 0
		}
	}

	if !oldestValid.IsZero() {
		stats.ResetRemaining = oldestValid.Add(l.config.WindowDuration).Sub(now)
		if stats.ResetRemaining < 0 {
			stats.ResetRemaining = 0
		}
	}

	return stats
}

// Reset clears the recorded history for a key.
func (l *SlidingWindowLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.keys, key)
}

// PruneIdle removes keys that haven't been accessed for the specified duration.
func (l *SlidingWindowLimiter) PruneIdle(maxIdle time.Duration) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now().UTC()
	pruned := 0
	for k, bucket := range l.keys {
		if now.Sub(bucket.lastAccess) > maxIdle {
			delete(l.keys, k)
			pruned++
		}
	}
	return pruned
}
