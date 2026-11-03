package entropy

import (
	"fmt"
	"math"
	"time"
)

// NISTHealthEvaluator performs continuous online health testing according to NIST SP 800-90B.
type NISTHealthEvaluator struct {
	rctCutoff           int
	aptWindow           int
	aptCutoff           int
	minEntropyThreshold float64
}

// NewNISTHealthEvaluator initializes a new evaluator with specified SP 800-90B parameters.
func NewNISTHealthEvaluator(cfg EntropyConfig) *NISTHealthEvaluator {
	rctCutoff := cfg.RCTCutoff
	if rctCutoff <= 0 {
		rctCutoff = 35
	}
	aptWindow := cfg.APTWindow
	if aptWindow <= 0 {
		aptWindow = 512
	}
	aptCutoff := cfg.APTCutoff
	if aptCutoff <= 0 {
		aptCutoff = 177
	}
	minEntThreshold := cfg.MinEntropyThreshold
	if minEntThreshold <= 0 {
		minEntThreshold = 6.0
	}

	return &NISTHealthEvaluator{
		rctCutoff:           rctCutoff,
		aptWindow:           aptWindow,
		aptCutoff:           aptCutoff,
		minEntropyThreshold: minEntThreshold,
	}
}

// RunRepetitionCountTest evaluates NIST SP 800-90B Section 4.4.1 Repetition Count Test (RCT).
// It verifies that no single symbol repeats consecutively for cutoff or more times.
func RunRepetitionCountTest(data []byte, cutoff int) (passed bool, maxRep int) {
	if len(data) == 0 {
		return true, 0
	}
	if cutoff <= 0 {
		cutoff = 35
	}

	maxRep = 1
	currentRep := 1
	for i := 1; i < len(data); i++ {
		if data[i] == data[i-1] {
			currentRep++
			if currentRep > maxRep {
				maxRep = currentRep
			}
			if currentRep >= cutoff {
				return false, maxRep
			}
		} else {
			currentRep = 1
		}
	}

	return true, maxRep
}

// RunAdaptiveProportionTest evaluates NIST SP 800-90B Section 4.4.2 Adaptive Proportion Test (APT).
// Within each window of W samples, it checks if the first sample appears >= cutoff times.
func RunAdaptiveProportionTest(data []byte, windowSize, cutoff int) (passed bool, maxCount int) {
	if len(data) == 0 {
		return true, 0
	}
	if windowSize <= 0 {
		windowSize = 512
	}
	if cutoff <= 0 {
		cutoff = 177
	}

	// If data length is less than windowSize, check proportion within available length
	actualWindow := windowSize
	if len(data) < actualWindow {
		actualWindow = len(data)
	}

	maxCount = 0
	for offset := 0; offset+actualWindow <= len(data); offset += actualWindow {
		target := data[offset]
		count := 1 // target itself
		for j := 1; j < actualWindow; j++ {
			if data[offset+j] == target {
				count++
			}
		}
		if count > maxCount {
			maxCount = count
		}
		if count >= cutoff {
			return false, maxCount
		}
	}

	return true, maxCount
}

// EstimateShannonEntropy computes the Shannon entropy across an 8-bit symbol alphabet (0.0 to 8.0 bits).
func EstimateShannonEntropy(data []byte) float64 {
	if len(data) == 0 {
		return 0.0
	}

	freq := make(map[byte]int)
	for _, b := range data {
		freq[b]++
	}

	n := float64(len(data))
	var shannon float64

	for _, count := range freq {
		p := float64(count) / n
		if p > 0 {
			shannon -= p * math.Log2(p)
		}
	}

	return math.Round(shannon*1000) / 1000
}

// EstimateMinEntropy computes the min-entropy (H_min = -log2(p_max)) per NIST SP 800-90B Section 3.1.3.
func EstimateMinEntropy(data []byte) float64 {
	if len(data) == 0 {
		return 0.0
	}

	freq := make(map[byte]int)
	maxCount := 0
	for _, b := range data {
		freq[b]++
		if freq[b] > maxCount {
			maxCount = freq[b]
		}
	}

	pMax := float64(maxCount) / float64(len(data))
	if pMax <= 0 {
		return 0.0
	}
	if pMax >= 1.0 {
		return 0.0
	}

	minEnt := -math.Log2(pMax)
	return math.Round(minEnt*1000) / 1000
}

// Evaluate runs the full suite of SP 800-90B online health tests and entropy estimation.
func (e *NISTHealthEvaluator) Evaluate(data []byte) NISTHealthReport {
	now := time.Now().UTC()
	if len(data) == 0 {
		return NISTHealthReport{
			RCTPassed:   false,
			APTPassed:   false,
			Status:      HealthStatusFailed,
			EvaluatedAt: now,
			Error:       "empty sample buffer",
		}
	}

	rctOk, maxRep := RunRepetitionCountTest(data, e.rctCutoff)
	aptOk, maxProp := RunAdaptiveProportionTest(data, e.aptWindow, e.aptCutoff)
	shannon := EstimateShannonEntropy(data)
	minEnt := EstimateMinEntropy(data)

	status := HealthStatusOptimal
	var errMsg string

	if !rctOk || !aptOk {
		status = HealthStatusFailed
		errMsg = fmt.Sprintf("SP 800-90B health failure: RCT=%t (max=%d/%d), APT=%t (max=%d/%d)",
			rctOk, maxRep, e.rctCutoff, aptOk, maxProp, e.aptCutoff)
	} else if minEnt < e.minEntropyThreshold {
		status = HealthStatusDegraded
		errMsg = fmt.Sprintf("min-entropy %0.2f bits below required threshold %0.2f bits",
			minEnt, e.minEntropyThreshold)
	}

	return NISTHealthReport{
		RCTPassed:       rctOk,
		APTPassed:       aptOk,
		RepetitionCount: maxRep,
		MaxProportion:   maxProp,
		WindowSize:      e.aptWindow,
		ShannonEntropy:  shannon,
		MinEntropy:      minEnt,
		Status:          status,
		EvaluatedAt:     now,
		Error:           errMsg,
	}
}
