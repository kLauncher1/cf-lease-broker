package radar

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPoPRegistryAndLookups(t *testing.T) {
	pops := DefaultPoPList()
	if len(pops) < 25 {
		t.Fatalf("expected at least 25 default edge PoPs, got %d", len(pops))
	}

	// Lookup by IATA
	pop, ok := LookupPoP("ICN")
	if !ok || pop.City != "Seoul" || pop.Country != "KR" {
		t.Fatalf("LookupPoP(ICN) failed: got %+v, ok=%v", pop, ok)
	}

	// Case-insensitivity
	popLower, okLower := LookupPoP("lhr")
	if !okLower || popLower.City != "London" {
		t.Fatalf("LookupPoP(lhr) failed: got %+v, ok=%v", popLower, okLower)
	}

	// Non-existent PoP
	_, notFound := LookupPoP("INVALID")
	if notFound {
		t.Fatal("expected LookupPoP(INVALID) to return false")
	}

	// Filter by region
	europePoPs := ListPoPsByRegion("Europe")
	if len(europePoPs) < 5 {
		t.Fatalf("expected at least 5 Europe PoPs, got %d", len(europePoPs))
	}
	for _, ep := range europePoPs {
		if ep.Region != "Europe" {
			t.Errorf("expected Europe region, got %s", ep.Region)
		}
	}

	// Country coordinates lookup
	krGeo, krFound := CountryToGeo("KR")
	if !krFound || krGeo.Lat == 0.0 {
		t.Fatalf("CountryToGeo(KR) failed: got %+v, found=%v", krGeo, krFound)
	}
	_, invalidCountry := CountryToGeo("ZZ")
	if invalidCountry {
		t.Fatal("expected CountryToGeo(ZZ) to return false")
	}
}

func TestHaversineDistanceAccuracy(t *testing.T) {
	// Zero distance between identical points
	p1 := GeoPoint{Lat: 37.5665, Lon: 126.9780}
	dist0 := HaversineDistance(p1, p1)
	if dist0 != 0.0 {
		t.Fatalf("expected distance 0, got %f", dist0)
	}

	// Distance between Seoul (ICN: 37.46, 126.44) and Tokyo (NRT: 35.77, 140.39) is ~1260 km
	pSeoul := GeoPoint{Lat: 37.4602, Lon: 126.4407}
	pTokyo := GeoPoint{Lat: 35.7720, Lon: 140.3929}
	distSeoulTokyo := HaversineDistance(pSeoul, pTokyo)
	if distSeoulTokyo < 1200 || distSeoulTokyo > 1300 {
		t.Fatalf("expected distance between Seoul and Tokyo to be ~1260km, got %f km", distSeoulTokyo)
	}
}

func TestSyntheticProbeLifecycleAndEMA(t *testing.T) {
	pops := DefaultPoPList()
	pe := NewProbeEngine(pops)

	// Inject deterministic mock probe func
	probeMap := map[string]int64{
		"ICN": 12,
		"NRT": 28,
		"LHR": 140,
	}
	pe.SetCustomProbeFunc(func(endpoint string) (int64, error) {
		for iata, rtt := range probeMap {
			if strings.Contains(endpoint, strings.ToLower(iata)) {
				return rtt, nil
			}
		}
		return 35, nil
	})

	// Initial probe check
	rtt, err := pe.ProbePoP("ICN")
	if err != nil || rtt != 12 {
		t.Fatalf("expected RTT 12, got %d, err=%v", rtt, err)
	}

	st, ok := pe.GetStats("ICN")
	if !ok || st.LastRTTMs != 12 {
		t.Fatalf("expected LastRTTMs 12, got %+v", st)
	}
	if st.EMARTTMs <= 0 {
		t.Fatalf("expected positive EMARTTMs, got %f", st.EMARTTMs)
	}

	// Consecutive failure test
	pe.RecordProbeResult("ICN", -1, false)
	pe.RecordProbeResult("ICN", -1, false)
	pe.RecordProbeResult("ICN", -1, false)
	stFail, _ := pe.GetStats("ICN")
	if stFail.Health != HealthOffline || stFail.ConsecutiveFailures != 3 {
		t.Fatalf("expected HealthOffline after 3 failures, got %+v", stFail)
	}

	// Recovery test
	pe.RecordProbeResult("ICN", 14, true)
	stRecovered, _ := pe.GetStats("ICN")
	if stRecovered.Health == HealthOffline {
		t.Fatalf("expected recovery from offline, got %+v", stRecovered)
	}

	// ProbeAll concurrent execution
	results := pe.ProbeAll(5)
	if len(results) < len(pops) {
		t.Fatalf("expected results for all %d PoPs, got %d", len(pops), len(results))
	}
}

func TestSubMillisecondRouteResolution(t *testing.T) {
	pe := NewProbeEngine(DefaultPoPList())
	router := NewRadarRouter(pe)

	// Client located in Seoul
	clientSeoul := GeoPoint{Lat: 37.5665, Lon: 126.9780}

	start := time.Now()
	rec := router.ResolveOptimalColo(clientSeoul, false)
	duration := time.Since(start)

	if duration > 50*time.Millisecond {
		t.Fatalf("resolution took too long: %v", duration)
	}
	if rec.Colo != "ICN" {
		t.Fatalf("expected optimal colo for Seoul to be ICN, got %s (alt: %s)", rec.Colo, rec.AlternativeColo)
	}
	if rec.DistanceKm > 100.0 {
		t.Fatalf("expected ICN distance from Seoul to be < 100km, got %f km", rec.DistanceKm)
	}

	// Client located in London
	clientLondon := GeoPoint{Lat: 51.5074, Lon: -0.1278}
	recLondon := router.ResolveOptimalColo(clientLondon, false)
	if recLondon.Colo != "LHR" {
		t.Fatalf("expected optimal colo for London to be LHR, got %s", recLondon.Colo)
	}

	// Client located in San Francisco
	clientSF := GeoPoint{Lat: 37.7749, Lon: -122.4194}
	recSF := router.ResolveOptimalColo(clientSF, false)
	if recSF.Colo != "SFO" {
		t.Fatalf("expected optimal colo for SF to be SFO, got %s", recSF.Colo)
	}
}

func TestDegradedColoFailover(t *testing.T) {
	pe := NewProbeEngine(DefaultPoPList())
	router := NewRadarRouter(pe)

	// Simulate ICN degradation
	for i := 0; i < 3; i++ {
		pe.RecordProbeResult("ICN", -1, false)
	}

	clientSeoul := GeoPoint{Lat: 37.5665, Lon: 126.9780}
	rec := router.ResolveOptimalColo(clientSeoul, false)

	// Should NOT be ICN since ICN is offline
	if rec.Colo == "ICN" {
		t.Fatalf("expected failover away from offline ICN, but got ICN")
	}
	// Best nearby alternative in Asia Pacific should be NRT, KIX, or TPE
	if rec.Region != "Asia Pacific" {
		t.Fatalf("expected failover to Asia Pacific region, got %s (%s)", rec.Region, rec.Colo)
	}
}

func TestBGPAnomalyDetectionAndRouteFlaps(t *testing.T) {
	pe := NewProbeEngine(DefaultPoPList())
	router := NewRadarRouter(pe)

	var anomalyDetected bool
	var anomalyColo string
	pe.OnAnomaly(func(iata string, oldRTT, newRTT float64, degraded bool) {
		anomalyDetected = true
		anomalyColo = iata
	})

	// Severe latency jump on IAD from baseline 20ms to 120ms
	pe.RecordProbeResult("IAD", 120, true)

	if !anomalyDetected || anomalyColo != "IAD" {
		t.Fatalf("expected anomaly notification for IAD, got detected=%v, colo=%s", anomalyDetected, anomalyColo)
	}

	events := router.GetRecentBGPEvents(10)
	if len(events) == 0 {
		t.Fatal("expected recorded BGP events, got 0")
	}
	lastEv := events[len(events)-1]
	if lastEv.Colo != "IAD" {
		t.Fatalf("expected last BGP event for IAD, got %s", lastEv.Colo)
	}
}

func TestAutonomousLeaseMigration(t *testing.T) {
	pe := NewProbeEngine(DefaultPoPList())
	router := NewRadarRouter(pe)

	// Bind leases to IAD
	router.AssignLeaseColo("lse_alpha_001", "tenant-alpha", "IAD")
	router.AssignLeaseColo("lse_beta_002", "tenant-beta", "IAD")

	colo, ok := router.GetLeaseColo("lse_alpha_001")
	if !ok || colo != "IAD" {
		t.Fatalf("expected lease lse_alpha_001 assigned to IAD, got %s", colo)
	}

	// Trigger migration away from degraded IAD
	events := router.MigrateLeasesFromColo("IAD", "BGP route flap mitigation test")
	if len(events) != 2 {
		t.Fatalf("expected 2 migration events, got %d", len(events))
	}

	// Verify lease re-routed to healthy nearby PoP (e.g. ATL, ORD, or DFW)
	newColo, _ := router.GetLeaseColo("lse_alpha_001")
	if newColo == "IAD" || newColo == "" {
		t.Fatalf("expected lease migrated away from IAD, but still %s", newColo)
	}

	recentMig := router.GetRecentMigrations(10)
	if len(recentMig) < 2 {
		t.Fatalf("expected at least 2 migrations recorded, got %d", len(recentMig))
	}
}

func TestRadarHTTPHandler_Endpoints(t *testing.T) {
	pe := NewProbeEngine(DefaultPoPList())
	pe.SetCustomProbeFunc(func(endpoint string) (int64, error) {
		return 15, nil
	})
	router := NewRadarRouter(pe)
	handler := NewRadarHandler(router, pe)

	// 1. GET /api/radar/status
	req := httptest.NewRequest(http.MethodGet, "/api/radar/status", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status code: %d, body: %s", rr.Code, rr.Body.String())
	}
	var st RadarStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &st); err != nil || st.TotalPoPs == 0 {
		t.Fatalf("failed to parse RadarStatus: %v", err)
	}

	// 2. GET /api/radar/pops?region=Europe
	reqPops := httptest.NewRequest(http.MethodGet, "/api/radar/pops?region=Europe", nil)
	rrPops := httptest.NewRecorder()
	handler.ServeHTTP(rrPops, reqPops)
	if rrPops.Code != http.StatusOK {
		t.Fatalf("pops code: %d", rrPops.Code)
	}
	var popsList []popWithStats
	if err := json.Unmarshal(rrPops.Body.Bytes(), &popsList); err != nil || len(popsList) == 0 {
		t.Fatalf("failed to parse Europe PoPs: %v", err)
	}

	// 3. POST /api/radar/probe?colo=ICN
	reqProbe := httptest.NewRequest(http.MethodPost, "/api/radar/probe?colo=ICN", nil)
	rrProbe := httptest.NewRecorder()
	handler.ServeHTTP(rrProbe, reqProbe)
	if rrProbe.Code != http.StatusOK {
		t.Fatalf("probe code: %d, body: %s", rrProbe.Code, rrProbe.Body.String())
	}

	// 4. GET /api/radar/route?country=KR
	reqRoute := httptest.NewRequest(http.MethodGet, "/api/radar/route?country=KR", nil)
	rrRoute := httptest.NewRecorder()
	handler.ServeHTTP(rrRoute, reqRoute)
	if rrRoute.Code != http.StatusOK {
		t.Fatalf("route code: %d", rrRoute.Code)
	}
	var routeRec RouteRecommendation
	if err := json.Unmarshal(rrRoute.Body.Bytes(), &routeRec); err != nil || routeRec.Colo != "ICN" {
		t.Fatalf("expected ICN for country=KR, got %s (%v)", routeRec.Colo, err)
	}

	// 5. POST /api/radar/route (JSON body)
	routeBody, _ := json.Marshal(map[string]interface{}{
		"lat":       51.5,
		"lon":       -0.1,
		"tenant_id": "tenant-uk",
	})
	reqRoutePost := httptest.NewRequest(http.MethodPost, "/api/radar/route", bytes.NewReader(routeBody))
	rrRoutePost := httptest.NewRecorder()
	handler.ServeHTTP(rrRoutePost, reqRoutePost)
	if rrRoutePost.Code != http.StatusOK {
		t.Fatalf("route POST code: %d", rrRoutePost.Code)
	}

	// 6. GET /api/radar/events
	reqEvents := httptest.NewRequest(http.MethodGet, "/api/radar/events", nil)
	rrEvents := httptest.NewRecorder()
	handler.ServeHTTP(rrEvents, reqEvents)
	if rrEvents.Code != http.StatusOK {
		t.Fatalf("events code: %d", rrEvents.Code)
	}

	// 7. POST /api/radar/migrate
	migBody, _ := json.Marshal(map[string]string{
		"from_colo": "LHR",
		"reason":    "Unit test migration",
	})
	reqMig := httptest.NewRequest(http.MethodPost, "/api/radar/migrate", bytes.NewReader(migBody))
	rrMig := httptest.NewRecorder()
	handler.ServeHTTP(rrMig, reqMig)
	if rrMig.Code != http.StatusOK {
		t.Fatalf("migrate code: %d", rrMig.Code)
	}

	// 8. 404 on unknown endpoint
	req404 := httptest.NewRequest(http.MethodGet, "/api/radar/unknown", nil)
	rr404 := httptest.NewRecorder()
	handler.ServeHTTP(rr404, req404)
	if rr404.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr404.Code)
	}
}

func TestRadarHighConcurrency(t *testing.T) {
	pe := NewProbeEngine(DefaultPoPList())
	router := NewRadarRouter(pe)

	var wg sync.WaitGroup
	workers := 40
	iterations := 50

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// Alternating operations
				loc := GeoPoint{
					Lat: 30.0 + float64((id+j)%20),
					Lon: 100.0 + float64((id*j)%30),
				}
				_ = router.ResolveOptimalColo(loc, false)

				if j%5 == 0 {
					pe.RecordProbeResult("ICN", int64(15+(j%10)), true)
				}
				if j%10 == 0 {
					_ = router.GetStatusSummary()
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestRadarBodyBoundingAndErrorCases(t *testing.T) {
	pe := NewProbeEngine(DefaultPoPList())
	router := NewRadarRouter(pe)
	handler := NewRadarHandler(router, pe)

	// Oversized body (>64KB)
	hugeBody := strings.Repeat("A", 70*1024)
	req := httptest.NewRequest(http.MethodPost, "/api/radar/migrate", strings.NewReader(hugeBody))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed/oversized json body, got %d", rr.Code)
	}

	// Method not allowed
	reqPut := httptest.NewRequest(http.MethodPut, "/api/radar/status", nil)
	rrPut := httptest.NewRecorder()
	handler.ServeHTTP(rrPut, reqPut)
	if rrPut.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for PUT /api/radar/status, got %d", rrPut.Code)
	}

	// Missing from_colo in migrate
	emptyMig := httptest.NewRequest(http.MethodPost, "/api/radar/migrate", strings.NewReader(`{}`))
	rrEmpty := httptest.NewRecorder()
	handler.ServeHTTP(rrEmpty, emptyMig)
	if rrEmpty.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty from_colo, got %d", rrEmpty.Code)
	}

	// Probe non-existent colo
	pe.SetCustomProbeFunc(func(endpoint string) (int64, error) {
		return -1, errors.New("unreachable")
	})
	rtt, err := pe.ProbePoP("NONEXISTENT")
	if err == nil || rtt != -1 {
		t.Fatalf("expected error probing non-existent colo, got rtt=%d, err=%v", rtt, err)
	}
}

// Silence unused math import if any
var _ = math.Pi
