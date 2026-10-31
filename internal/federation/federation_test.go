package federation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClusterRegistrationAndListing(t *testing.T) {
	cfg := FederationConfig{
		LocalClusterID: "test-cluster-local",
		LocalRegion:    "us-east-1",
		LocalProvider:  CloudProviderCloudflareEdge,
	}
	engine := NewFederationEngine(cfg)

	// Clean out seeded clusters for targeted test
	for _, c := range engine.ListClusters() {
		_ = engine.DeregisterCluster(c.ID)
	}

	c1 := &ClusterNode{
		ID:             "cluster-alpha",
		Name:           "Alpha Cluster",
		Provider:       CloudProviderAWS,
		Region:         "us-west-2",
		PublicEndpoint: "198.51.100.1:9443",
		OverlayIP:      "10.240.10.1",
		Status:         ClusterStatusActive,
		LatencyMs:      22.5,
		Capabilities:   []string{"workers", "kv"},
	}

	if err := engine.RegisterCluster(c1); err != nil {
		t.Fatalf("failed to register cluster: %v", err)
	}

	// Retrieve cluster
	retrieved, found := engine.GetCluster("cluster-alpha")
	if !found || retrieved.ID != "cluster-alpha" || retrieved.Region != "us-west-2" {
		t.Fatalf("unexpected cluster retrieved: %+v", retrieved)
	}

	// Register second cluster
	c2 := &ClusterNode{
		ID:             "cluster-beta",
		Name:           "Beta Cluster",
		Provider:       CloudProviderGCP,
		Region:         "eu-west-1",
		PublicEndpoint: "198.51.100.2:9443",
		OverlayIP:      "10.240.20.1",
		Status:         ClusterStatusActive,
		LatencyMs:      78.1,
		Capabilities:   []string{"workers", "r2"},
	}
	_ = engine.RegisterCluster(c2)

	list := engine.ListClusters()
	if len(list) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(list))
	}
	if list[0].ID != "cluster-alpha" || list[1].ID != "cluster-beta" {
		t.Errorf("clusters not sorted deterministically: %s, %s", list[0].ID, list[1].ID)
	}

	// Deregister
	if err := engine.DeregisterCluster("cluster-alpha"); err != nil {
		t.Fatalf("deregister failed: %v", err)
	}
	if _, found := engine.GetCluster("cluster-alpha"); found {
		t.Errorf("cluster-alpha still present after deregistration")
	}
}

func TestWANTunnelLifecycleAndProbing(t *testing.T) {
	tm := NewTunnelManager("local-broker")

	tun := &WANTunnel{
		ID:              "tun-test-01",
		LocalEndpoint:   "10.240.0.1:51820",
		RemoteEndpoint:  "198.51.100.2:51820",
		RemoteClusterID: "cluster-remote-01",
		Protocol:        ProtocolWireGuard,
		Status:          TunnelStatusHandshaking,
	}

	if err := tm.RegisterTunnel(tun); err != nil {
		t.Fatalf("failed to register tunnel: %v", err)
	}

	// Duplicate registration check
	if err := tm.RegisterTunnel(tun); err != ErrTunnelAlreadyExists {
		t.Fatalf("expected ErrTunnelAlreadyExists, got %v", err)
	}

	// Check default values
	got, found := tm.GetTunnel("tun-test-01")
	if !found {
		t.Fatalf("tunnel not found")
	}
	if got.MTU != 1420 {
		t.Errorf("expected default MTU 1420, got %d", got.MTU)
	}
	if got.KeepaliveSec != 25 {
		t.Errorf("expected default keepalive 25, got %d", got.KeepaliveSec)
	}

	// Simulate high latency / loss probe leading to degradation
	probed, err := tm.SimulateProbe("tun-test-01", 380.0, 25.0, 5.0)
	if err != nil {
		t.Fatalf("probe simulation failed: %v", err)
	}
	if probed.Status != TunnelStatusDegraded {
		t.Errorf("expected status degraded due to high loss/latency, got %s", probed.Status)
	}

	// Recover with healthy probe
	probed, _ = tm.SimulateProbe("tun-test-01", 45.0, 0.0, 1.0)
	if probed.Status != TunnelStatusConnected {
		t.Errorf("expected status connected after healthy probe, got %s", probed.Status)
	}

	// Record traffic
	_ = tm.RecordTraffic("tun-test-01", 1024, 2048, 2, 4)
	got, _ = tm.GetTunnel("tun-test-01")
	if got.BytesSent != 1024 || got.BytesReceived != 2048 {
		t.Errorf("traffic counters mismatch: sent=%d recv=%d", got.BytesSent, got.BytesReceived)
	}

	// Metrics aggregation
	avgLat, totalBytes, connected := tm.CalculateAggregateMetrics()
	if connected != 1 {
		t.Errorf("expected 1 connected tunnel, got %d", connected)
	}
	if avgLat != 45.0 {
		t.Errorf("expected avg latency 45.0, got %f", avgLat)
	}
	if totalBytes != 3072 {
		t.Errorf("expected total bytes 3072, got %d", totalBytes)
	}
}

func TestTunnelNoiseHandshake(t *testing.T) {
	tm := NewTunnelManager("local-broker")

	_ = tm.RegisterTunnel(&WANTunnel{
		ID:              "tun-wg-handshake",
		LocalEndpoint:   "10.240.0.1:51820",
		RemoteEndpoint:  "198.51.100.5:51820",
		RemoteClusterID: "cluster-remote-02",
		Protocol:        ProtocolWireGuard,
		Status:          TunnelStatusHandshaking,
	})

	if err := tm.PerformHandshake("tun-wg-handshake", "peer_pub_key_hex_1234"); err != nil {
		t.Fatalf("handshake failed: %v", err)
	}

	tun, _ := tm.GetTunnel("tun-wg-handshake")
	if tun.Status != TunnelStatusConnected {
		t.Errorf("expected status connected, got %s", tun.Status)
	}
	if tun.PeerPublicKey != "peer_pub_key_hex_1234" {
		t.Errorf("peer public key not updated: %s", tun.PeerPublicKey)
	}
	if time.Since(tun.LastHandshake) > 5*time.Second {
		t.Errorf("last handshake not updated: %v", tun.LastHandshake)
	}
}

func TestOverlayRoutingMetricAndASPath(t *testing.T) {
	tm := NewTunnelManager("local-broker")
	_ = tm.RegisterTunnel(&WANTunnel{
		ID:              "tun-1",
		RemoteClusterID: "cluster-eu",
		Status:          TunnelStatusConnected,
	})
	_ = tm.RegisterTunnel(&WANTunnel{
		ID:              "tun-2",
		RemoteClusterID: "cluster-eu-alt",
		Status:          TunnelStatusConnected,
	})

	router := NewOverlayRouter(tm)

	// Advertise two routes to service-storage: one with higher metric, one with lower metric
	_ = router.AdvertiseRoute(&OverlayRoute{
		ID:              "rt-storage-backup",
		DestinationCIDR: "service-storage-*",
		TargetClusterID: "cluster-eu-alt",
		NextHopTunnelID: "tun-2",
		Metric:          50,
		ASPath:          []string{"64512", "64515"},
		Status:          RouteStatusActive,
	})
	_ = router.AdvertiseRoute(&OverlayRoute{
		ID:              "rt-storage-primary",
		DestinationCIDR: "service-storage-*",
		TargetClusterID: "cluster-eu",
		NextHopTunnelID: "tun-1",
		Metric:          10,
		ASPath:          []string{"64512", "64513"},
		Status:          RouteStatusActive,
	})

	resolved, err := router.ResolveRoute("service-storage-blob")
	if err != nil {
		t.Fatalf("failed to resolve route: %v", err)
	}
	if resolved.ID != "rt-storage-primary" {
		t.Errorf("expected rt-storage-primary with lower metric, got %s", resolved.ID)
	}
}

func TestOverlayRouteWithdrawalAndFailover(t *testing.T) {
	tm := NewTunnelManager("local-broker")
	_ = tm.RegisterTunnel(&WANTunnel{ID: "tun-active", Status: TunnelStatusConnected})
	router := NewOverlayRouter(tm)

	_ = router.AdvertiseRoute(&OverlayRoute{
		ID:              "rt-to-withdraw",
		DestinationCIDR: "10.240.50.0/24",
		TargetClusterID: "cluster-dying",
		NextHopTunnelID: "tun-active",
		Metric:          10,
		Status:          RouteStatusActive,
	})

	if _, err := router.ResolveRoute("10.240.50.12"); err != nil {
		t.Fatalf("route should resolve before withdrawal: %v", err)
	}

	if err := router.WithdrawRoute("rt-to-withdraw"); err != nil {
		t.Fatalf("failed to withdraw route: %v", err)
	}

	// Should now fail to resolve
	if _, err := router.ResolveRoute("10.240.50.12"); err != ErrNoViableRoute {
		t.Errorf("expected ErrNoViableRoute after withdrawal, got %v", err)
	}

	// Bulk withdraw by cluster
	_ = router.AdvertiseRoute(&OverlayRoute{
		ID:              "rt-cluster-a",
		DestinationCIDR: "service-a",
		TargetClusterID: "cluster-target-x",
		NextHopTunnelID: "tun-active",
		Status:          RouteStatusActive,
	})
	withdrawnCount := router.WithdrawByCluster("cluster-target-x")
	if withdrawnCount != 1 {
		t.Errorf("expected 1 route withdrawn by cluster, got %d", withdrawnCount)
	}
}

func TestSovereigntyGDPRCompliance(t *testing.T) {
	sov := NewSovereigntyValidator()

	_ = sov.AddRule(&SovereigntyRule{
		RuleID:              "rule-gdpr-strict",
		TenantID:            "*",
		Classification:      ClassificationPII,
		Jurisdiction:        JurisdictionGDPR,
		AllowedRegions:      []string{"eu-central-1", "eu-west-1"},
		EnforceJurisdiction: true,
		StrictCompliance:    true,
	})

	// 1. EU region routing should succeed
	resEU := sov.ValidateCompliance("tenant-corp", "cluster-fra", "eu-central-1", ClassificationPII)
	if !resEU.Allowed {
		t.Errorf("expected EU region to be allowed under GDPR, got violation: %s", resEU.ViolationReason)
	}

	// 2. US region routing for PII should be BLOCKED by GDPR
	resUS := sov.ValidateCompliance("tenant-corp", "cluster-iad", "us-east-1", ClassificationPII)
	if resUS.Allowed {
		t.Errorf("expected US region to be blocked for PII under GDPR")
	}
	if !strings.Contains(resUS.ViolationReason, "outside authorized residency") && !strings.Contains(resUS.ViolationReason, "GDPR") {
		t.Errorf("unexpected violation reason: %s", resUS.ViolationReason)
	}

	// 3. General non-PII classification should NOT be blocked by PII-specific rule
	resGeneral := sov.ValidateCompliance("tenant-corp", "cluster-iad", "us-east-1", ClassificationGeneral)
	if !resGeneral.Allowed {
		t.Errorf("expected General data classification to pass: %s", resGeneral.ViolationReason)
	}

	// Verify audit log has recorded entries
	logs := sov.ListAuditLogs(10)
	if len(logs) < 3 {
		t.Errorf("expected at least 3 audit logs, got %d", len(logs))
	}
}

func TestSovereigntyCCPAAndAPPICompliance(t *testing.T) {
	sov := NewSovereigntyValidator()

	_ = sov.AddRule(&SovereigntyRule{
		RuleID:              "rule-ccpa",
		TenantID:            "tenant-us-only",
		Classification:      ClassificationFinancial,
		Jurisdiction:        JurisdictionCCPA,
		AllowedRegions:      []string{"us-east-1", "us-west-2"},
		EnforceJurisdiction: true,
	})

	_ = sov.AddRule(&SovereigntyRule{
		RuleID:              "rule-appi",
		TenantID:            "tenant-jp-only",
		Classification:      ClassificationHealthcare,
		Jurisdiction:        JurisdictionAPPI,
		AllowedRegions:      []string{"ap-northeast-1", "ap-northeast-3"},
		EnforceJurisdiction: true,
	})

	// US CCPA test
	resCCPAOk := sov.ValidateCompliance("tenant-us-only", "cluster-iad", "us-east-1", ClassificationFinancial)
	if !resCCPAOk.Allowed {
		t.Errorf("CCPA allowed region failed: %s", resCCPAOk.ViolationReason)
	}
	resCCPANotOk := sov.ValidateCompliance("tenant-us-only", "cluster-fra", "eu-central-1", ClassificationFinancial)
	if resCCPANotOk.Allowed {
		t.Errorf("CCPA should reject EU region")
	}

	// Japan APPI test
	resAPPIOk := sov.ValidateCompliance("tenant-jp-only", "cluster-nrt", "ap-northeast-1", ClassificationHealthcare)
	if !resAPPIOk.Allowed {
		t.Errorf("APPI allowed region failed: %s", resAPPIOk.ViolationReason)
	}
	resAPPINotOk := sov.ValidateCompliance("tenant-jp-only", "cluster-iad", "us-east-1", ClassificationHealthcare)
	if resAPPINotOk.Allowed {
		t.Errorf("APPI should reject US region")
	}
}

func TestSovereigntyTenantOverride(t *testing.T) {
	sov := NewSovereigntyValidator()

	// Global default allows only US
	_ = sov.AddRule(&SovereigntyRule{
		RuleID:         "global-us",
		TenantID:       "*",
		Classification: ClassificationGeneral,
		AllowedRegions: []string{"us-east-1"},
	})

	// Specific European tenant overrides to allow EU
	_ = sov.AddRule(&SovereigntyRule{
		RuleID:         "tenant-eu-override",
		TenantID:       "tenant-multinational",
		Classification: ClassificationGeneral,
		AllowedRegions: []string{"eu-central-1"},
	})

	// Standard tenant blocked in EU
	resStandard := sov.ValidateCompliance("tenant-standard", "cluster-fra", "eu-central-1", ClassificationGeneral)
	if resStandard.Allowed {
		t.Errorf("expected standard tenant to be blocked in EU by global rule")
	}

	// Multinational tenant allowed in EU
	resMulti := sov.ValidateCompliance("tenant-multinational", "cluster-fra", "eu-central-1", ClassificationGeneral)
	if !resMulti.Allowed {
		t.Errorf("expected multinational tenant to be allowed in EU by specific override rule")
	}
}

func TestFederationRouteResolutionEndToEnd(t *testing.T) {
	cfg := FederationConfig{
		LocalClusterID: "cluster-local",
		LocalRegion:    "us-east-1",
	}
	engine := NewFederationEngine(cfg) // Seeds default topology

	// 1. Resolve European service route
	dec, err := engine.ResolveFederationRoute("tenant-eu-client", "service-billing-eu", "us-east-1", ClassificationGeneral)
	if err != nil {
		t.Fatalf("failed to resolve EU route: %v", err)
	}
	if !dec.ShouldFederate {
		t.Errorf("expected ShouldFederate true")
	}
	if dec.TargetCluster.ID != "cluster-gcp-eu-central-1" {
		t.Errorf("expected target cluster cluster-gcp-eu-central-1, got %s", dec.TargetCluster.ID)
	}
	if dec.SelectedTunnel == nil || dec.SelectedTunnel.ID != "tun-wg-iad-fra-01" {
		t.Errorf("expected tunnel tun-wg-iad-fra-01, got %v", dec.SelectedTunnel)
	}

	// 2. Resolve Asia-Pacific service route
	decAP, err := engine.ResolveFederationRoute("tenant-ap-client", "service-analytics-ap", "us-east-1", ClassificationGeneral)
	if err != nil {
		t.Fatalf("failed to resolve AP route: %v", err)
	}
	if decAP.TargetCluster.ID != "cluster-azure-ap-northeast-1" {
		t.Errorf("expected target cluster cluster-azure-ap-northeast-1, got %s", decAP.TargetCluster.ID)
	}

	// 3. Dispatch federated lease
	dispDec, err := engine.DispatchFederatedLease("tenant-client", "service-orders-eu", ClassificationGeneral)
	if err != nil {
		t.Fatalf("dispatch lease failed: %v", err)
	}
	if dispDec.TargetCluster == nil {
		t.Fatalf("expected non-nil target cluster in dispatch decision")
	}
	stats := engine.GetStats()
	if stats.FederatedRequestsForwarded != 1 {
		t.Errorf("expected 1 forwarded request, got %d", stats.FederatedRequestsForwarded)
	}
}

func TestFederationEngineLifecycle(t *testing.T) {
	cfg := FederationConfig{
		LocalClusterID:    "cluster-lifecycle-test",
		HeartbeatInterval: 10 * time.Millisecond,
	}
	engine := NewFederationEngine(cfg)
	engine.Start()

	// Double start should be a no-op
	engine.Start()

	time.Sleep(25 * time.Millisecond)
	engine.Stop()

	// Double stop should be a safe no-op
	engine.Stop()
}

func TestFederationHTTPHandlerEndpoints(t *testing.T) {
	cfg := FederationConfig{LocalClusterID: "cluster-http-test"}
	engine := NewFederationEngine(cfg)
	handler := NewFederationHandler(engine)
	server := httptest.NewServer(handler)
	defer server.Close()

	client := server.Client()

	// 1. GET /status
	resp, err := client.Get(server.URL + "/api/federation/status")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /status failed: err=%v code=%d", err, resp.StatusCode)
	}
	var stats FederationStats
	_ = json.NewDecoder(resp.Body).Decode(&stats)
	_ = resp.Body.Close()
	if stats.TotalClusters < 3 {
		t.Errorf("expected at least 3 clusters in stats, got %d", stats.TotalClusters)
	}

	// 2. GET /clusters
	resp, err = client.Get(server.URL + "/api/federation/clusters")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /clusters failed: code=%d", resp.StatusCode)
	}
	var clusters []ClusterNode
	_ = json.NewDecoder(resp.Body).Decode(&clusters)
	_ = resp.Body.Close()
	if len(clusters) < 3 {
		t.Errorf("expected at least 3 clusters, got %d", len(clusters))
	}

	// 3. POST /tunnels/probe
	probeReq := `{"tunnel_id":"tun-wg-iad-fra-01","latency_ms":79.5,"loss_pct":0.0,"jitter_ms":1.1}`
	resp, err = client.Post(server.URL+"/api/federation/tunnels/probe", "application/json", strings.NewReader(probeReq))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /tunnels/probe failed: code=%d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 4. POST /tunnels/handshake
	hsReq := `{"tunnel_id":"tun-wg-iad-fra-01","peer_public_key":"aabbccddeeff00112233"}`
	resp, err = client.Post(server.URL+"/api/federation/tunnels/handshake", "application/json", strings.NewReader(hsReq))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /tunnels/handshake failed: code=%d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 5. POST /routes/resolve
	resolveReq := `{"tenant_id":"tenant-test","destination":"service-kv-eu","client_region":"us-east-1","classification":"general"}`
	resp, err = client.Post(server.URL+"/api/federation/routes/resolve", "application/json", strings.NewReader(resolveReq))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /routes/resolve failed: code=%d", resp.StatusCode)
	}
	var dec FederationDecision
	_ = json.NewDecoder(resp.Body).Decode(&dec)
	_ = resp.Body.Close()
	if !dec.ShouldFederate {
		t.Errorf("expected resolution to succeed")
	}

	// 6. POST /sovereignty/validate
	sovReq := `{"tenant_id":"tenant-eu","target_cluster_id":"cluster-fra","target_region":"eu-central-1","classification":"pii"}`
	resp, err = client.Post(server.URL+"/api/federation/sovereignty/validate", "application/json", strings.NewReader(sovReq))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /sovereignty/validate failed: code=%d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 7. GET /sovereignty/audit
	resp, err = client.Get(server.URL + "/api/federation/sovereignty/audit")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /sovereignty/audit failed: code=%d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 8. POST /reset
	resp, err = client.Post(server.URL+"/api/federation/reset", "application/json", nil)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /reset failed: code=%d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestFederationRequestSizeBounding(t *testing.T) {
	cfg := FederationConfig{LocalClusterID: "cluster-dos-test"}
	engine := NewFederationEngine(cfg)
	handler := NewFederationHandler(engine)
	server := httptest.NewServer(handler)
	defer server.Close()

	// Create oversized payload (> 64KB)
	oversized := make([]byte, 128*1024)
	for i := range oversized {
		oversized[i] = 'A'
	}

	resp, err := server.Client().Post(server.URL+"/api/federation/routes/resolve", "application/json", bytes.NewReader(oversized))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	// Should fail with 400 Bad Request due to incomplete JSON after 64KB truncation
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for >64KB truncated JSON payload, got %d", resp.StatusCode)
	}
}

func TestFederationConcurrentAccess(t *testing.T) {
	cfg := FederationConfig{LocalClusterID: "cluster-concurrency-test"}
	engine := NewFederationEngine(cfg)

	var wg sync.WaitGroup
	workers := 40

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			tenant := fmt.Sprintf("tenant-%d", id)
			dest := "service-test-eu"
			if id%2 == 0 {
				dest = "service-test-ap"
			}

			// Route resolution
			_, _ = engine.ResolveFederationRoute(tenant, dest, "us-east-1", ClassificationGeneral)

			// Probe simulation
			_, _ = engine.TunnelManager().SimulateProbe("tun-wg-iad-fra-01", 80.0+float64(id), 0.01, 1.2)

			// Compliance check
			_ = engine.SovereigntyValidator().ValidateCompliance(tenant, "cluster-test", "eu-central-1", ClassificationPII)

			// Stats read
			_ = engine.GetStats()
		}(i)
	}

	wg.Wait()
}
