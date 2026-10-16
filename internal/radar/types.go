package radar

import "time"

// PoPHealth represents the operational health status of an edge PoP.
type PoPHealth string

const (
	HealthOptimal  PoPHealth = "optimal"
	HealthHealthy  PoPHealth = "healthy"
	HealthDegraded PoPHealth = "degraded"
	HealthOffline  PoPHealth = "offline"
)

// GeoPoint represents geographic latitude and longitude coordinates.
type GeoPoint struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// PoPInfo defines the static metadata and geographic properties of a Cloudflare edge PoP.
type PoPInfo struct {
	IATA          string   `json:"iata"`
	City          string   `json:"city"`
	Country       string   `json:"country"`
	Region        string   `json:"region"`
	Location      GeoPoint `json:"location"`
	ProbeEndpoint string   `json:"probe_endpoint"`
	Tier          int      `json:"tier"`
}

// ProbeStats records dynamic telemetry and health scores for an edge PoP.
type ProbeStats struct {
	LastRTTMs           int64     `json:"last_rtt_ms"`
	EMARTTMs            float64   `json:"ema_rtt_ms"`
	JitterMs            float64   `json:"jitter_ms"`
	LossRate            float64   `json:"loss_rate"`
	Health              PoPHealth `json:"health"`
	HealthScore         int       `json:"health_score"`
	ProbeCount          int64     `json:"probe_count"`
	SuccessCount        int64     `json:"success_count"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	LastProbeTime       time.Time `json:"last_probe_time"`
}

// RouteRecommendation contains the optimal edge PoP resolved for a client or tenant.
type RouteRecommendation struct {
	Colo                            string    `json:"colo"`
	City                            string    `json:"city"`
	Country                         string    `json:"country"`
	Region                          string    `json:"region"`
	DistanceKm                      float64   `json:"distance_km"`
	EstimatedRTTMs                  float64   `json:"estimated_rtt_ms"`
	CompositeScore                  float64   `json:"composite_score"`
	Health                          PoPHealth `json:"health"`
	DegradedFailover                bool      `json:"degraded_failover"`
	AlternativeColo                 string    `json:"alternative_colo"`
	ResolutionDurationMicroseconds int64     `json:"resolution_duration_us"`
}

// BGPEvent records network route anomalies, latency spikes, or BGP route flaps.
type BGPEvent struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Colo      string    `json:"colo"`
	EventType string    `json:"event_type"` // latency_spike, route_flap, loss_detected, recovered
	OldRTTMs  float64   `json:"old_rtt_ms"`
	NewRTTMs  float64   `json:"new_rtt_ms"`
	DeltaMs   float64   `json:"delta_ms"`
	Severity  string    `json:"severity"` // info, warning, critical
	Message   string    `json:"message"`
}

// MigrationEvent records an autonomous lease migration away from a degraded edge colo.
type MigrationEvent struct {
	ID             string    `json:"id"`
	Timestamp      time.Time `json:"timestamp"`
	TenantID       string    `json:"tenant_id"`
	LeaseID        string    `json:"lease_id"`
	FromColo       string    `json:"from_colo"`
	ToColo         string    `json:"to_colo"`
	Reason         string    `json:"reason"`
	LatencyDeltaMs float64   `json:"latency_delta_ms"`
}

// RadarStatus summarizes overall radar operational telemetry.
type RadarStatus struct {
	Active                 bool      `json:"active"`
	TotalPoPs              int       `json:"total_pops"`
	HealthyPoPs            int       `json:"healthy_pops"`
	DegradedPoPs           int       `json:"degraded_pops"`
	OfflinePoPs            int       `json:"offline_pops"`
	AverageGlobalRTTMs     float64   `json:"average_global_rtt_ms"`
	TotalProbesDispatched  int64     `json:"total_probes_dispatched"`
	RecentBGPAlerts        int       `json:"recent_bgp_alerts"`
	TotalMigrations        int       `json:"total_migrations"`
	LastProbeRun           time.Time `json:"last_probe_run"`
}
