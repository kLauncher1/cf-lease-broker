package quota

import (
	"encoding/json"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/kLauncher1/cf-lease-broker/internal/broker"
)

// State constants for account lifecycle.
const (
	StateHealthy   = "HEALTHY"
	StateProbation = "PROBATION"
	StateExhausted = "EXHAUSTED"
)

// Default probation timing parameters.
const (
	DefaultBaseProbation = 30 * time.Second
	DefaultMaxProbation  = 5 * time.Minute
	DefaultMultiplier    = 2.0
)

// ProbationRecord tracks the health status and probation parameters of an account.
type ProbationRecord struct {
	AccountID       string        `json:"account_id"`
	State           string        `json:"state"`
	Failures        int           `json:"failures"`
	ProbationStart  time.Time     `json:"probation_start,omitempty"`
	ProbationEnd    time.Time     `json:"probation_end,omitempty"`
	RemainingPeriod time.Duration `json:"remaining_period,omitempty"`
	LastReason      string        `json:"last_reason,omitempty"`
}

// ProbationManager coordinates automated rate-limit probation and recovery routines.
type ProbationManager struct {
	mu            sync.RWMutex
	poolMgr       *broker.PoolManager
	records       map[string]*ProbationRecord
	baseDuration  time.Duration
	maxDuration   time.Duration
	multiplier    float64
}

// NewProbationManager initializes a probation manager bound to an account pool.
func NewProbationManager(poolMgr *broker.PoolManager) *ProbationManager {
	pm := &ProbationManager{
		poolMgr:      poolMgr,
		records:      make(map[string]*ProbationRecord),
		baseDuration: DefaultBaseProbation,
		maxDuration:  DefaultMaxProbation,
		multiplier:   DefaultMultiplier,
	}

	// Initialize tracked state for existing pool accounts
	if poolMgr != nil {
		for _, acc := range poolMgr.ListAccounts() {
			pm.records[acc.ID] = &ProbationRecord{
				AccountID: acc.ID,
				State:     StateHealthy,
			}
		}
	}

	return pm
}

// RecordFailure transitions an account into probation with exponential backoff.
func (pm *ProbationManager) RecordFailure(accountID, reason string) time.Duration {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	rec, exists := pm.records[accountID]
	if !exists {
		rec = &ProbationRecord{
			AccountID: accountID,
			State:     StateHealthy,
		}
		pm.records[accountID] = rec
	}

	rec.Failures++
	rec.LastReason = reason
	rec.State = StateProbation

	// Compute exponential backoff duration
	exponent := float64(rec.Failures - 1)
	if exponent < 0 {
		exponent = 0
	}
	factor := math.Pow(pm.multiplier, exponent)
	dur := time.Duration(float64(pm.baseDuration) * factor)
	if dur > pm.maxDuration {
		dur = pm.maxDuration
	}

	now := time.Now().UTC()
	rec.ProbationStart = now
	rec.ProbationEnd = now.Add(dur)
	rec.RemainingPeriod = dur

	if pm.poolMgr != nil {
		_ = pm.poolMgr.SetAccountProbation(accountID, dur)
	}

	return dur
}

// CheckState queries the current state of an account and auto-recovers if probation has elapsed.
func (pm *ProbationManager) CheckState(accountID string) (string, time.Duration) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	rec, exists := pm.records[accountID]
	if !exists {
		return StateHealthy, 0
	}

	if rec.State == StateProbation {
		now := time.Now().UTC()
		if now.After(rec.ProbationEnd) {
			// Auto-recovery: probation timer elapsed
			rec.State = StateHealthy
			rec.Failures = 0
			rec.RemainingPeriod = 0
			rec.LastReason = "auto-recovered after probation cooldown"
			if pm.poolMgr != nil {
				_ = pm.poolMgr.RecoverAccount(accountID)
			}
			return StateHealthy, 0
		}
		remaining := rec.ProbationEnd.Sub(now)
		rec.RemainingPeriod = remaining
		return StateProbation, remaining
	}

	return rec.State, 0
}

// Recover manually clears probation or exhaustion status for an account.
func (pm *ProbationManager) Recover(accountID string) bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	rec, exists := pm.records[accountID]
	if !exists {
		rec = &ProbationRecord{AccountID: accountID}
		pm.records[accountID] = rec
	}

	rec.State = StateHealthy
	rec.Failures = 0
	rec.ProbationStart = time.Time{}
	rec.ProbationEnd = time.Time{}
	rec.RemainingPeriod = 0
	rec.LastReason = "manually restored to service"

	if pm.poolMgr != nil {
		_ = pm.poolMgr.RecoverAccount(accountID)
	}
	return true
}

// MarkExhausted sets an account into permanently exhausted status.
func (pm *ProbationManager) MarkExhausted(accountID, reason string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	rec, exists := pm.records[accountID]
	if !exists {
		rec = &ProbationRecord{AccountID: accountID}
		pm.records[accountID] = rec
	}

	rec.State = StateExhausted
	rec.LastReason = reason
	if pm.poolMgr != nil {
		_ = pm.poolMgr.TriggerFailover(accountID)
	}
}

// SweepExpired checks all accounts on probation and restores those whose timer has elapsed.
func (pm *ProbationManager) SweepExpired() []string {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	now := time.Now().UTC()
	var recovered []string

	for id, rec := range pm.records {
		if rec.State == StateProbation && now.After(rec.ProbationEnd) {
			rec.State = StateHealthy
			rec.Failures = 0
			rec.RemainingPeriod = 0
			rec.LastReason = "auto-recovered during sweep"
			if pm.poolMgr != nil {
				_ = pm.poolMgr.RecoverAccount(id)
			}
			recovered = append(recovered, id)
		}
	}

	return recovered
}

// ListRecords returns snapshots of all tracked account probation records.
func (pm *ProbationManager) ListRecords() []ProbationRecord {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	now := time.Now().UTC()
	res := make([]ProbationRecord, 0, len(pm.records))

	for _, rec := range pm.records {
		cpy := *rec
		if cpy.State == StateProbation {
			if now.After(cpy.ProbationEnd) {
				cpy.State = StateHealthy
				cpy.RemainingPeriod = 0
			} else {
				cpy.RemainingPeriod = cpy.ProbationEnd.Sub(now)
			}
		}
		res = append(res, cpy)
	}

	return res
}

// ServeHTTP provides the REST management API for `/api/quota/probation/*`.
func (pm *ProbationManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch {
	case r.URL.Path == "/api/quota/probation" && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(pm.ListRecords())

	case r.URL.Path == "/api/quota/probation/recover" && r.Method == http.MethodPost:
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, `{"error":"missing id parameter"}`, http.StatusBadRequest)
			return
		}
		pm.Recover(id)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":     "recovered",
			"account_id": id,
		})

	case r.URL.Path == "/api/quota/probation/trigger" && r.Method == http.MethodPost:
		id := r.URL.Query().Get("id")
		if id == "" {
			id = "cf-tenant-alpha"
		}
		reason := r.URL.Query().Get("reason")
		if reason == "" {
			reason = "HTTP 429 Too Many Requests rate limit breach"
		}
		dur := pm.RecordFailure(id, reason)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":             "probation_triggered",
			"account_id":         id,
			"probation_duration": dur.String(),
			"reason":             reason,
		})

	default:
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}
}
