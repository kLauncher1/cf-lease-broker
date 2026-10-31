package federation

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultMaxAuditEntries sets the maximum number of compliance validation audit records retained in memory.
const DefaultMaxAuditEntries = 500

// SovereigntyValidator enforces data residency and regulatory sovereignty constraints.
type SovereigntyValidator struct {
	mu              sync.RWMutex
	rules           map[string]*SovereigntyRule
	auditLog        []ComplianceCheckResult
	maxAuditEntries int
	violationsCount int64
}

// NewSovereigntyValidator creates a new data sovereignty compliance validator.
func NewSovereigntyValidator() *SovereigntyValidator {
	return &SovereigntyValidator{
		rules:           make(map[string]*SovereigntyRule),
		auditLog:        make([]ComplianceCheckResult, 0, DefaultMaxAuditEntries),
		maxAuditEntries: DefaultMaxAuditEntries,
	}
}

// AddRule registers a sovereignty rule.
func (sv *SovereigntyValidator) AddRule(rule *SovereigntyRule) error {
	if rule == nil {
		return fmt.Errorf("cannot add nil sovereignty rule")
	}
	if rule.RuleID == "" {
		return fmt.Errorf("rule ID cannot be empty")
	}
	if rule.TenantID == "" {
		rule.TenantID = "*"
	}
	if rule.Classification == "" {
		rule.Classification = ClassificationGeneral
	}
	if rule.Jurisdiction == "" {
		rule.Jurisdiction = JurisdictionGlobal
	}

	sv.mu.Lock()
	defer sv.mu.Unlock()

	rCopy := *rule
	sv.rules[rule.RuleID] = &rCopy
	return nil
}

// RemoveRule deletes a sovereignty rule by ID.
func (sv *SovereigntyValidator) RemoveRule(ruleID string) error {
	sv.mu.Lock()
	defer sv.mu.Unlock()

	if _, exists := sv.rules[ruleID]; !exists {
		return fmt.Errorf("rule not found: %s", ruleID)
	}
	delete(sv.rules, ruleID)
	return nil
}

// GetRule retrieves a rule by its ID.
func (sv *SovereigntyValidator) GetRule(ruleID string) (*SovereigntyRule, bool) {
	sv.mu.RLock()
	defer sv.mu.RUnlock()

	r, exists := sv.rules[ruleID]
	if !exists {
		return nil, false
	}
	rCopy := *r
	return &rCopy, true
}

// ListRules returns all configured sovereignty rules sorted deterministically by ID.
func (sv *SovereigntyValidator) ListRules() []*SovereigntyRule {
	sv.mu.RLock()
	defer sv.mu.RUnlock()

	result := make([]*SovereigntyRule, 0, len(sv.rules))
	for _, r := range sv.rules {
		rCopy := *r
		result = append(result, &rCopy)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].RuleID < result[j].RuleID
	})
	return result
}

// ValidateCompliance tests whether dispatching data to targetRegion and targetClusterID satisfies sovereignty.
func (sv *SovereigntyValidator) ValidateCompliance(
	tenantID, targetClusterID, targetRegion string,
	classification DataClassification,
) ComplianceCheckResult {
	if classification == "" {
		classification = ClassificationGeneral
	}

	sv.mu.Lock()
	defer sv.mu.Unlock()

	res := ComplianceCheckResult{
		Allowed:         true,
		TenantID:        tenantID,
		TargetClusterID: targetClusterID,
		TargetRegion:    targetRegion,
		Classification:  classification,
		EvaluatedAt:     time.Now(),
	}

	if targetRegion == "" {
		res.Allowed = false
		res.ViolationReason = "target region cannot be empty for federated routing"
		sv.recordAuditLocked(res)
		sv.violationsCount++
		return res
	}

	// Collect matching rules: specific tenant first, then global "*"
	var applicableRules []*SovereigntyRule
	for _, r := range sv.rules {
		if r.TenantID == tenantID {
			applicableRules = append(applicableRules, r)
		}
	}
	if len(applicableRules) == 0 {
		for _, r := range sv.rules {
			if r.TenantID == "*" {
				applicableRules = append(applicableRules, r)
			}
		}
	}

	regionLower := strings.ToLower(targetRegion)

	for _, rule := range applicableRules {
		// Classification filter: rule applies if rule is General (applies to all data), or rule classification matches request classification
		if rule.Classification != ClassificationGeneral && rule.Classification != classification {
			continue
		}

		// 1. Check explicitly blocked regions
		for _, blk := range rule.BlockedRegions {
			if strings.EqualFold(blk, regionLower) || strings.HasPrefix(regionLower, strings.ToLower(blk)) {
				res.Allowed = false
				res.MatchedRuleID = rule.RuleID
				res.ViolationReason = fmt.Sprintf("target region %q is explicitly restricted by sovereignty policy %s", targetRegion, rule.RuleID)
				sv.recordAuditLocked(res)
				sv.violationsCount++
				return res
			}
		}

		// 2. Check allowed regions whitelist (if specified)
		if len(rule.AllowedRegions) > 0 {
			allowed := false
			for _, alw := range rule.AllowedRegions {
				if strings.EqualFold(alw, regionLower) || strings.HasPrefix(regionLower, strings.ToLower(alw)) {
					allowed = true
					break
				}
			}
			if !allowed {
				res.Allowed = false
				res.MatchedRuleID = rule.RuleID
				res.ViolationReason = fmt.Sprintf("target region %q is outside authorized residency whitelist for rule %s", targetRegion, rule.RuleID)
				sv.recordAuditLocked(res)
				sv.violationsCount++
				return res
			}
		}

		// 3. Check legal regulatory jurisdiction boundaries
		if rule.EnforceJurisdiction {
			switch rule.Jurisdiction {
			case JurisdictionGDPR:
				if !isEuropeanRegion(regionLower) {
					res.Allowed = false
					res.MatchedRuleID = rule.RuleID
					res.ViolationReason = fmt.Sprintf("GDPR residency violation: %s is not an authorized European economic zone region", targetRegion)
					sv.recordAuditLocked(res)
					sv.violationsCount++
					return res
				}
			case JurisdictionCCPA:
				if !isUnitedStatesRegion(regionLower) {
					res.Allowed = false
					res.MatchedRuleID = rule.RuleID
					res.ViolationReason = fmt.Sprintf("CCPA residency violation: %s is not a compliant United States domestic region", targetRegion)
					sv.recordAuditLocked(res)
					sv.violationsCount++
					return res
				}
			case JurisdictionAPPI:
				if !isJapanRegion(regionLower) {
					res.Allowed = false
					res.MatchedRuleID = rule.RuleID
					res.ViolationReason = fmt.Sprintf("APPI residency violation: %s is outside Japan sovereign data borders", targetRegion)
					sv.recordAuditLocked(res)
					sv.violationsCount++
					return res
				}
			}
		}
	}

	sv.recordAuditLocked(res)
	return res
}

// GetViolationsCount returns the total number of blocked cross-border compliance violations.
func (sv *SovereigntyValidator) GetViolationsCount() int64 {
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	return sv.violationsCount
}

// ListAuditLogs returns the most recent compliance evaluation records.
func (sv *SovereigntyValidator) ListAuditLogs(limit int) []ComplianceCheckResult {
	sv.mu.RLock()
	defer sv.mu.RUnlock()

	if limit <= 0 || limit > len(sv.auditLog) {
		limit = len(sv.auditLog)
	}

	result := make([]ComplianceCheckResult, limit)
	start := len(sv.auditLog) - limit
	copy(result, sv.auditLog[start:])
	return result
}

// ClearAuditLogs flushes stored compliance evaluation logs.
func (sv *SovereigntyValidator) ClearAuditLogs() {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	sv.auditLog = sv.auditLog[:0]
}

func (sv *SovereigntyValidator) recordAuditLocked(rec ComplianceCheckResult) {
	if len(sv.auditLog) >= sv.maxAuditEntries {
		sv.auditLog = sv.auditLog[1:]
	}
	sv.auditLog = append(sv.auditLog, rec)
}

func isEuropeanRegion(reg string) bool {
	euPrefixes := []string{"eu-", "fra", "cdg", "lhr", "ams", "mad", "arn", "mxp", "zrh", "vie", "hel"}
	for _, p := range euPrefixes {
		if strings.HasPrefix(reg, p) {
			return true
		}
	}
	return false
}

func isUnitedStatesRegion(reg string) bool {
	usPrefixes := []string{"us-", "iad", "sfo", "ord", "dfw", "lax", "sea", "atl", "mia", "den", "phx"}
	for _, p := range usPrefixes {
		if strings.HasPrefix(reg, p) {
			return true
		}
	}
	return false
}

func isJapanRegion(reg string) bool {
	jpPrefixes := []string{"ap-northeast-1", "ap-northeast-3", "nrt", "kix", "tyo", "osa"}
	for _, p := range jpPrefixes {
		if strings.HasPrefix(reg, p) {
			return true
		}
	}
	return false
}
