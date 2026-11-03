package bgp

import (
	"strings"
	"sync"
)

// CommunityRouter matches tenant requests against transit pricing rules to assign dynamic BGP communities.
type CommunityRouter struct {
	mu           sync.RWMutex
	pricingRules []*TransitPricingRule
}

// NewCommunityRouter constructs a dynamic community router pre-populated with standard policies.
func NewCommunityRouter() *CommunityRouter {
	cr := &CommunityRouter{
		pricingRules: make([]*TransitPricingRule, 0),
	}
	cr.seedDefaultRules()
	return cr
}

// seedDefaultRules establishes baseline transit pricing and routing policies.
func (cr *CommunityRouter) seedDefaultRules() {
	cr.pricingRules = []*TransitPricingRule{
		{
			RuleID:          "rule-enterprise-latency",
			TenantTier:      "enterprise",
			ServicePattern:  "*",
			MaxCostPerMb:    0.05,
			PreferredASN:    3356, // Level3 / Lumen Tier-1 Low Latency
			AssignedTags:    []string{CommunityTransitLowLatency, "64512:1001"},
			LocalPreference: 200,
		},
		{
			RuleID:          "rule-pro-balanced",
			TenantTier:      "pro",
			ServicePattern:  "*",
			MaxCostPerMb:    0.02,
			PreferredASN:    1299, // Telia / Arelion Balanced Transit
			AssignedTags:    []string{CommunityTransitBalanced, "64512:1002"},
			LocalPreference: 150,
		},
		{
			RuleID:          "rule-free-cost-optimal",
			TenantTier:      "free",
			ServicePattern:  "*",
			MaxCostPerMb:    0.005,
			PreferredASN:    174, // Cogent Bulk Low Cost
			AssignedTags:    []string{CommunityTransitLowCost, CommunityNoExport},
			LocalPreference: 100,
		},
		{
			RuleID:          "rule-r2-bulk-storage",
			TenantTier:      "*",
			ServicePattern:  "r2",
			MaxCostPerMb:    0.008,
			PreferredASN:    6939, // Hurricane Electric High-Volume
			AssignedTags:    []string{CommunityTransitLowCost, "64512:1003"},
			LocalPreference: 110,
		},
		{
			RuleID:          "rule-workers-fast-api",
			TenantTier:      "*",
			ServicePattern:  "workers",
			MaxCostPerMb:    0.03,
			PreferredASN:    13335, // Cloudflare Edge Direct
			AssignedTags:    []string{CommunityTransitLowLatency, "64512:1004"},
			LocalPreference: 180,
		},
	}
}

// AddRule registers a new transit pricing and community policy rule.
func (cr *CommunityRouter) AddRule(rule *TransitPricingRule) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	for i, r := range cr.pricingRules {
		if r.RuleID == rule.RuleID {
			cr.pricingRules[i] = rule
			return
		}
	}
	cr.pricingRules = append(cr.pricingRules, rule)
}

// RemoveRule deletes a rule by RuleID.
func (cr *CommunityRouter) RemoveRule(ruleID string) bool {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	for i, r := range cr.pricingRules {
		if r.RuleID == ruleID {
			cr.pricingRules = append(cr.pricingRules[:i], cr.pricingRules[i+1:]...)
			return true
		}
	}
	return false
}

// ListRules returns all active transit pricing rules.
func (cr *CommunityRouter) ListRules() []*TransitPricingRule {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	out := make([]*TransitPricingRule, len(cr.pricingRules))
	copy(out, cr.pricingRules)
	return out
}

// EvaluateRouting evaluates tenant tier and target service to return optimal communities and local preference.
func (cr *CommunityRouter) EvaluateRouting(tenantTier, service string) (tags []string, preferredASN uint32, localPref uint32) {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	tierLower := strings.ToLower(tenantTier)
	serviceLower := strings.ToLower(service)

	bestPref := uint32(100)
	var bestTags []string
	var bestASN uint32 = 13335 // Default Cloudflare Edge ASN

	for _, r := range cr.pricingRules {
		tierMatch := r.TenantTier == "*" || strings.ToLower(r.TenantTier) == tierLower
		serviceMatch := r.ServicePattern == "*" || strings.Contains(serviceLower, strings.ToLower(r.ServicePattern))

		if tierMatch && serviceMatch {
			if r.LocalPreference >= bestPref {
				bestPref = r.LocalPreference
				bestTags = r.AssignedTags
				bestASN = r.PreferredASN
			}
		}
	}

	if len(bestTags) == 0 {
		bestTags = []string{CommunityTransitBalanced}
	}
	return bestTags, bestASN, bestPref
}

// FormatCommunityTags merges standard and transit tags into a formatted comma-separated string.
func FormatCommunityTags(tags []string) string {
	if len(tags) == 0 {
		return "none"
	}
	return strings.Join(tags, ",")
}
