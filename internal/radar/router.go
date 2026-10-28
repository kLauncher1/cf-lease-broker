package radar

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

const earthRadiusKm = 6371.0

// RadarRouter handles anycast geo-routing, sub-millisecond PoP selection, and BGP migration.
type RadarRouter struct {
	mu              sync.RWMutex
	probeEngine     *ProbeEngine
	pops            []PoPInfo
	tenantColoMap   map[string]string // tenantID -> preferred IATA
	leaseColoMap    map[string]string // leaseID -> assigned IATA
	bgpEvents       []BGPEvent
	migrationEvents []MigrationEvent
	maxHistory      int
}

// NewRadarRouter instantiates the Anycast Geo-Routing and BGP failure mitigation engine.
func NewRadarRouter(pe *ProbeEngine) *RadarRouter {
	pops := DefaultPoPList()
	rr := &RadarRouter{
		probeEngine:     pe,
		pops:            pops,
		tenantColoMap:   make(map[string]string),
		leaseColoMap:    make(map[string]string),
		bgpEvents:       make([]BGPEvent, 0, 100),
		migrationEvents: make([]MigrationEvent, 0, 100),
		maxHistory:      100,
	}

	// Wire probe engine anomaly callback for automated BGP event tracking and pre-emptive migration
	pe.OnAnomaly(func(iata string, oldRTT, newRTT float64, degraded bool) {
		rr.DetectRouteAnomaly(iata, oldRTT, newRTT, degraded)
	})

	return rr
}

// HaversineDistance calculates great-circle distance between two geographic coordinates in kilometers.
func HaversineDistance(p1, p2 GeoPoint) float64 {
	lat1Rad := p1.Lat * math.Pi / 180.0
	lat2Rad := p2.Lat * math.Pi / 180.0
	deltaLat := (p2.Lat - p1.Lat) * math.Pi / 180.0
	deltaLon := (p2.Lon - p1.Lon) * math.Pi / 180.0

	a := math.Sin(deltaLat/2.0)*math.Sin(deltaLat/2.0) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(deltaLon/2.0)*math.Sin(deltaLon/2.0)

	c := 2.0 * math.Atan2(math.Sqrt(a), math.Sqrt(1.0-a))
	return earthRadiusKm * c
}

type candidateScore struct {
	pop         PoPInfo
	stats       ProbeStats
	distanceKm  float64
	score       float64
	estRTT      float64
	failoverDeg bool
}

// ResolveOptimalColo resolves the lowest-latency healthy Cloudflare PoP for given client coordinates.
func (rr *RadarRouter) ResolveOptimalColo(clientLoc GeoPoint, allowDegraded bool) RouteRecommendation {
	start := time.Now()
	rr.mu.RLock()
	allStats := rr.probeEngine.GetAllStats()
	popList := rr.pops
	rr.mu.RUnlock()

	var candidates []candidateScore
	for _, pop := range popList {
		st, hasStats := allStats[pop.IATA]
		if !hasStats {
			st = ProbeStats{Health: HealthHealthy, EMARTTMs: 25.0, JitterMs: 2.0}
		}

		if st.Health == HealthOffline {
			continue // Completely down
		}

		dist := HaversineDistance(clientLoc, pop.Location)
		score := (dist * 0.05) + (st.EMARTTMs * 0.6) + (st.JitterMs * 1.5)
		failover := false

		if st.Health == HealthDegraded {
			if !allowDegraded {
				score += 5000.0 // Heavy penalty to prioritize healthy PoPs
				failover = true
			} else {
				score += 50.0
			}
		}

		candidates = append(candidates, candidateScore{
			pop:         pop,
			stats:       st,
			distanceKm:  dist,
			score:       score,
			estRTT:      st.EMARTTMs,
			failoverDeg: failover,
		})
	}

	if len(candidates) == 0 {
		// Fallback to first PoP if all offline
		def := popList[0]
		return RouteRecommendation{
			Colo:                            def.IATA,
			City:                            def.City,
			Country:                         def.Country,
			Region:                          def.Region,
			DistanceKm:                      HaversineDistance(clientLoc, def.Location),
			EstimatedRTTMs:                  50.0,
			CompositeScore:                  999.0,
			Health:                          HealthOffline,
			ResolutionDurationMicroseconds: time.Since(start).Microseconds(),
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score < candidates[j].score
	})

	best := candidates[0]
	altColo := ""
	if len(candidates) > 1 {
		altColo = candidates[1].pop.IATA
	}

	durUs := time.Since(start).Microseconds()
	return RouteRecommendation{
		Colo:                            best.pop.IATA,
		City:                            best.pop.City,
		Country:                         best.pop.Country,
		Region:                          best.pop.Region,
		DistanceKm:                      math.Round(best.distanceKm*100) / 100,
		EstimatedRTTMs:                  math.Round(best.estRTT*100) / 100,
		CompositeScore:                  math.Round(best.score*100) / 100,
		Health:                          best.stats.Health,
		DegradedFailover:                best.failoverDeg,
		AlternativeColo:                 altColo,
		ResolutionDurationMicroseconds: durUs,
	}
}

// ResolveTenantColo finds the optimal colo for a tenant, automatically failing over if preferred colo is degraded.
func (rr *RadarRouter) ResolveTenantColo(tenantID string, clientLoc GeoPoint) RouteRecommendation {
	rec := rr.ResolveOptimalColo(clientLoc, false)

	rr.mu.Lock()
	rr.tenantColoMap[tenantID] = rec.Colo
	rr.mu.Unlock()

	return rec
}

// AssignLeaseColo binds an active lease to an edge PoP.
func (rr *RadarRouter) AssignLeaseColo(leaseID, tenantID, colo string) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	rr.leaseColoMap[leaseID] = colo
	if tenantID != "" {
		rr.tenantColoMap[tenantID] = colo
	}
}

// GetLeaseColo looks up the edge PoP assigned to a lease.
func (rr *RadarRouter) GetLeaseColo(leaseID string) (string, bool) {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	colo, ok := rr.leaseColoMap[leaseID]
	return colo, ok
}

// DetectRouteAnomaly analyzes telemetry changes, appends BGP event logs, and triggers proactive migration.
func (rr *RadarRouter) DetectRouteAnomaly(iata string, oldRTT, newRTT float64, degraded bool) {
	delta := newRTT - oldRTT
	evType := "latency_spike"
	severity := "warning"
	if degraded {
		evType = "route_flap"
		severity = "critical"
	} else if delta < -10.0 {
		evType = "recovered"
		severity = "info"
	}

	ev := BGPEvent{
		ID:        generateRandomHex(8),
		Timestamp: time.Now().UTC(),
		Colo:      iata,
		EventType: evType,
		OldRTTMs:  math.Round(oldRTT*100) / 100,
		NewRTTMs:  math.Round(newRTT*100) / 100,
		DeltaMs:   math.Round(delta*100) / 100,
		Severity:  severity,
		Message:   fmt.Sprintf("PoP %s anomaly: latency shifted from %.1fms to %.1fms (delta %.1fms)", iata, oldRTT, newRTT, delta),
	}

	rr.mu.Lock()
	rr.bgpEvents = append(rr.bgpEvents, ev)
	if len(rr.bgpEvents) > rr.maxHistory {
		rr.bgpEvents = rr.bgpEvents[len(rr.bgpEvents)-rr.maxHistory:]
	}
	rr.mu.Unlock()

	// Proactively migrate any active leases bound to this colo if degraded
	if degraded {
		rr.MigrateLeasesFromColo(iata, fmt.Sprintf("BGP route degradation (%s, delta +%.1fms)", evType, delta))
	}
}

// MigrateLeasesFromColo dynamically migrates leases assigned to a degraded colo to the nearest healthy alternative.
func (rr *RadarRouter) MigrateLeasesFromColo(fromColo, reason string) []MigrationEvent {
	rr.mu.Lock()
	affectedLeases := make([]string, 0)
	for lid, colo := range rr.leaseColoMap {
		if colo == fromColo {
			affectedLeases = append(affectedLeases, lid)
		}
	}
	rr.mu.Unlock()

	if len(affectedLeases) == 0 {
		return nil
	}

	// Resolve alternative target PoP from source PoP coordinates
	var sourceLoc GeoPoint
	for _, p := range rr.pops {
		if p.IATA == fromColo {
			sourceLoc = p.Location
			break
		}
	}

	rec := rr.ResolveOptimalColo(sourceLoc, false)
	toColo := rec.Colo
	if toColo == fromColo && rec.AlternativeColo != "" {
		toColo = rec.AlternativeColo
	}

	var events []MigrationEvent
	rr.mu.Lock()
	defer rr.mu.Unlock()

	for _, lid := range affectedLeases {
		rr.leaseColoMap[lid] = toColo
		me := MigrationEvent{
			ID:             generateRandomHex(8),
			Timestamp:      time.Now().UTC(),
			LeaseID:        lid,
			FromColo:       fromColo,
			ToColo:         toColo,
			Reason:         reason,
			LatencyDeltaMs: rec.EstimatedRTTMs,
		}
		events = append(events, me)
		rr.migrationEvents = append(rr.migrationEvents, me)
	}

	if len(rr.migrationEvents) > rr.maxHistory {
		rr.migrationEvents = rr.migrationEvents[len(rr.migrationEvents)-rr.maxHistory:]
	}

	return events
}

// GetRecentBGPEvents returns recent route anomaly events.
func (rr *RadarRouter) GetRecentBGPEvents(limit int) []BGPEvent {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	if limit <= 0 || limit > len(rr.bgpEvents) {
		limit = len(rr.bgpEvents)
	}
	res := make([]BGPEvent, limit)
	startIdx := len(rr.bgpEvents) - limit
	copy(res, rr.bgpEvents[startIdx:])
	return res
}

// GetRecentMigrations returns recent dynamic lease migration events.
func (rr *RadarRouter) GetRecentMigrations(limit int) []MigrationEvent {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	if limit <= 0 || limit > len(rr.migrationEvents) {
		limit = len(rr.migrationEvents)
	}
	res := make([]MigrationEvent, limit)
	startIdx := len(rr.migrationEvents) - limit
	copy(res, rr.migrationEvents[startIdx:])
	return res
}

// GetStatusSummary computes a snapshot of overall radar health and PoP distribution.
func (rr *RadarRouter) GetStatusSummary() RadarStatus {
	allStats := rr.probeEngine.GetAllStats()
	var healthy, degraded, offline int
	var totalRTT float64

	for _, st := range allStats {
		switch st.Health {
		case HealthOptimal, HealthHealthy:
			healthy++
		case HealthDegraded:
			degraded++
		case HealthOffline:
			offline++
		}
		totalRTT += st.EMARTTMs
	}

	avgRTT := 0.0
	if len(allStats) > 0 {
		avgRTT = math.Round((totalRTT/float64(len(allStats)))*100) / 100
	}

	rr.mu.RLock()
	bgpCount := len(rr.bgpEvents)
	migCount := len(rr.migrationEvents)
	rr.mu.RUnlock()

	return RadarStatus{
		Active:                true,
		TotalPoPs:             len(rr.pops),
		HealthyPoPs:           healthy,
		DegradedPoPs:          degraded,
		OfflinePoPs:           offline,
		AverageGlobalRTTMs:    avgRTT,
		TotalProbesDispatched: rr.probeEngine.TotalProbes(),
		RecentBGPAlerts:       bgpCount,
		TotalMigrations:       migCount,
		LastProbeRun:          rr.probeEngine.LastProbeRun(),
	}
}

func generateRandomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
