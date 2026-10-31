package federation

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// FederationEngine orchestrates cross-cloud gateway federation, WAN tunnels, overlay routing, and data sovereignty.
type FederationEngine struct {
	mu                   sync.RWMutex
	config               FederationConfig
	clusters             map[string]*ClusterNode
	tunnelManager        *TunnelManager
	overlayRouter        *OverlayRouter
	sovereigntyValidator *SovereigntyValidator
	forwardedRequests    int64
	stopChan             chan struct{}
	running              bool
}

// NewFederationEngine instantiates an enterprise-grade multi-cluster gateway federation engine.
func NewFederationEngine(cfg FederationConfig) *FederationEngine {
	if cfg.LocalClusterID == "" {
		cfg.LocalClusterID = "cluster-edge-local"
	}
	if cfg.LocalRegion == "" {
		cfg.LocalRegion = "us-east-1"
	}
	if cfg.LocalProvider == "" {
		cfg.LocalProvider = CloudProviderCloudflareEdge
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 30 * time.Second
	}
	if cfg.TunnelHandshakeTimeout <= 0 {
		cfg.TunnelHandshakeTimeout = 10 * time.Second
	}

	tm := NewTunnelManager(cfg.LocalClusterID)
	router := NewOverlayRouter(tm)
	sov := NewSovereigntyValidator()

	engine := &FederationEngine{
		config:               cfg,
		clusters:             make(map[string]*ClusterNode),
		tunnelManager:        tm,
		overlayRouter:        router,
		sovereigntyValidator: sov,
		stopChan:             make(chan struct{}),
	}

	engine.SeedDefaultTopology()
	return engine
}

// Start launches the background peer heartbeat and tunnel health monitor.
func (fe *FederationEngine) Start() {
	fe.mu.Lock()
	if fe.running {
		fe.mu.Unlock()
		return
	}
	fe.running = true
	fe.stopChan = make(chan struct{})
	fe.mu.Unlock()

	go fe.runMaintenanceLoop()
}

// Stop gracefully shuts down the federation background maintenance loops.
func (fe *FederationEngine) Stop() {
	fe.mu.Lock()
	defer fe.mu.Unlock()

	if !fe.running {
		return
	}
	fe.running = false
	close(fe.stopChan)
}

// RegisterCluster inserts or updates a cluster node in the federated topology.
func (fe *FederationEngine) RegisterCluster(c *ClusterNode) error {
	if c == nil {
		return fmt.Errorf("cannot register nil cluster node")
	}
	if c.ID == "" {
		return fmt.Errorf("cluster ID cannot be empty")
	}

	fe.mu.Lock()
	defer fe.mu.Unlock()

	if c.Status == "" {
		c.Status = ClusterStatusActive
	}
	if c.LastHeartbeat.IsZero() {
		c.LastHeartbeat = time.Now()
	}

	cCopy := *c
	fe.clusters[c.ID] = &cCopy
	return nil
}

// DeregisterCluster removes a cluster node and withdraws all its advertised routes.
func (fe *FederationEngine) DeregisterCluster(clusterID string) error {
	fe.mu.Lock()
	defer fe.mu.Unlock()

	if _, exists := fe.clusters[clusterID]; !exists {
		return ErrClusterNotFound
	}
	delete(fe.clusters, clusterID)
	fe.overlayRouter.WithdrawByCluster(clusterID)
	return nil
}

// GetCluster returns a specific cluster node by ID.
func (fe *FederationEngine) GetCluster(clusterID string) (*ClusterNode, bool) {
	fe.mu.RLock()
	defer fe.mu.RUnlock()

	c, exists := fe.clusters[clusterID]
	if !exists {
		return nil, false
	}
	cCopy := *c
	return &cCopy, true
}

// ListClusters returns all registered cluster nodes sorted deterministically.
func (fe *FederationEngine) ListClusters() []*ClusterNode {
	fe.mu.RLock()
	defer fe.mu.RUnlock()

	result := make([]*ClusterNode, 0, len(fe.clusters))
	for _, c := range fe.clusters {
		cCopy := *c
		result = append(result, &cCopy)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}

// TunnelManager exposes the underlying WAN tunnel manager.
func (fe *FederationEngine) TunnelManager() *TunnelManager {
	return fe.tunnelManager
}

// OverlayRouter exposes the dynamic overlay route manager.
func (fe *FederationEngine) OverlayRouter() *OverlayRouter {
	return fe.overlayRouter
}

// SovereigntyValidator exposes the data sovereignty compliance validator.
func (fe *FederationEngine) SovereigntyValidator() *SovereigntyValidator {
	return fe.sovereigntyValidator
}

// ResolveFederationRoute computes the end-to-end routing decision enforcing data residency.
func (fe *FederationEngine) ResolveFederationRoute(
	tenantID, destination, clientRegion string,
	classification DataClassification,
) (*FederationDecision, error) {
	if destination == "" {
		return nil, fmt.Errorf("destination cannot be empty")
	}

	route, err := fe.overlayRouter.ResolveRoute(destination)
	if err != nil {
		return nil, err
	}

	cluster, found := fe.GetCluster(route.TargetClusterID)
	if !found {
		return nil, fmt.Errorf("cluster not found for target %s", route.TargetClusterID)
	}

	// Validate data sovereignty compliance
	targetRegion := cluster.Region
	sovCheck := fe.sovereigntyValidator.ValidateCompliance(tenantID, cluster.ID, targetRegion, classification)
	if !sovCheck.Allowed {
		return &FederationDecision{
			ShouldFederate:   false,
			TargetCluster:    cluster,
			SelectedRoute:    route,
			SovereigntyCheck: sovCheck,
			Reason:           "blocked by data sovereignty compliance policy: " + sovCheck.ViolationReason,
		}, ErrSovereigntyViolation
	}

	// Find active WAN overlay tunnel
	tunnel, _ := fe.tunnelManager.GetTunnel(route.NextHopTunnelID)
	if tunnel == nil {
		// Fallback to searching direct tunnel to target cluster
		tunnel, _ = fe.tunnelManager.FindTunnelByRemoteCluster(cluster.ID)
	}

	var routingLatency float64
	if tunnel != nil {
		routingLatency = tunnel.LatencyMs + cluster.LatencyMs
	} else {
		routingLatency = cluster.LatencyMs
	}

	return &FederationDecision{
		ShouldFederate:   true,
		TargetCluster:    cluster,
		SelectedTunnel:   tunnel,
		SelectedRoute:    route,
		SovereigntyCheck: sovCheck,
		RoutingLatencyMs: routingLatency,
		Reason:           "optimal overlay WAN mesh route resolved",
	}, nil
}

// DispatchFederatedLease records a request forwarding event across the multi-cluster federation mesh.
func (fe *FederationEngine) DispatchFederatedLease(
	tenantID, targetService string,
	classification DataClassification,
) (*FederationDecision, error) {
	dec, err := fe.ResolveFederationRoute(tenantID, targetService, fe.config.LocalRegion, classification)
	if err != nil {
		return dec, err
	}

	atomic.AddInt64(&fe.forwardedRequests, 1)
	if dec.SelectedTunnel != nil {
		_ = fe.tunnelManager.RecordTraffic(dec.SelectedTunnel.ID, 1024, 2048, 1, 2)
	}
	return dec, nil
}

// GetStats compiles real-time telemetry across clusters, WAN tunnels, routes, and sovereignty.
func (fe *FederationEngine) GetStats() FederationStats {
	fe.mu.RLock()
	totalClusters := len(fe.clusters)
	activeClusters := 0
	for _, c := range fe.clusters {
		if c.Status == ClusterStatusActive {
			activeClusters++
		}
	}
	fe.mu.RUnlock()

	avgLatency, totalBytes, connectedTunnels := fe.tunnelManager.CalculateAggregateMetrics()
	allTunnels := fe.tunnelManager.ListTunnels()
	allRoutes := fe.overlayRouter.ListRoutes()
	activeRoutes := fe.overlayRouter.ListActiveRoutes()

	return FederationStats{
		LocalClusterID:              fe.config.LocalClusterID,
		LocalRegion:                 fe.config.LocalRegion,
		TotalClusters:               totalClusters,
		ActiveClusters:              activeClusters,
		TotalTunnels:                len(allTunnels),
		ConnectedTunnels:            connectedTunnels,
		TotalRoutes:                 len(allRoutes),
		ActiveRoutes:                len(activeRoutes),
		TotalSovereigntyRules:       len(fe.sovereigntyValidator.ListRules()),
		ComplianceViolationsBlocked: fe.sovereigntyValidator.GetViolationsCount(),
		FederatedRequestsForwarded:  atomic.LoadInt64(&fe.forwardedRequests),
		AverageWANLatencyMs:         avgLatency,
		TotalBytesTransferred:       totalBytes,
		LastTopologyUpdate:          time.Now(),
	}
}

// SeedDefaultTopology bootstraps production-grade multi-cluster topologies, WireGuard tunnels, and sovereignty policies.
func (fe *FederationEngine) SeedDefaultTopology() {
	// 1. Multi-Cloud Federated Clusters
	_ = fe.RegisterCluster(&ClusterNode{
		ID:             "cluster-aws-us-east-1",
		Name:           "AWS North America Primary (IAD)",
		Provider:       CloudProviderAWS,
		Region:         "us-east-1",
		PublicEndpoint: "198.51.100.12:9443",
		OverlayIP:      "10.240.1.10",
		Status:         ClusterStatusActive,
		LatencyMs:      14.2,
		Capabilities:   []string{"workers", "kv", "r2", "inference"},
	})
	_ = fe.RegisterCluster(&ClusterNode{
		ID:             "cluster-gcp-eu-central-1",
		Name:           "GCP Europe Central Gateway (FRA)",
		Provider:       CloudProviderGCP,
		Region:         "eu-central-1",
		PublicEndpoint: "198.51.100.45:9443",
		OverlayIP:      "10.240.2.10",
		Status:         ClusterStatusActive,
		LatencyMs:      82.4,
		Capabilities:   []string{"workers", "kv", "r2"},
	})
	_ = fe.RegisterCluster(&ClusterNode{
		ID:             "cluster-azure-ap-northeast-1",
		Name:           "Azure Asia-Pacific Gateway (NRT)",
		Provider:       CloudProviderAzure,
		Region:         "ap-northeast-1",
		PublicEndpoint: "198.51.100.89:9443",
		OverlayIP:      "10.240.3.10",
		Status:         ClusterStatusActive,
		LatencyMs:      146.8,
		Capabilities:   []string{"workers", "kv"},
	})

	// 2. Encrypted Point-to-Point WAN Overlay Tunnels
	_ = fe.tunnelManager.RegisterTunnel(&WANTunnel{
		ID:              "tun-wg-iad-fra-01",
		LocalEndpoint:   "10.240.0.1:51820",
		RemoteEndpoint:  "198.51.100.45:51820",
		RemoteClusterID: "cluster-gcp-eu-central-1",
		Protocol:        ProtocolWireGuard,
		Status:          TunnelStatusConnected,
		MTU:             1420,
		PeerPublicKey:   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		KeepaliveSec:    25,
		LatencyMs:       81.5,
		PacketLossPct:   0.01,
		JitterMs:        1.2,
		BytesSent:       4852900,
		BytesReceived:   9841200,
		PacketsSent:     3420,
		PacketsReceived: 7120,
	})
	_ = fe.tunnelManager.RegisterTunnel(&WANTunnel{
		ID:              "tun-wg-iad-nrt-01",
		LocalEndpoint:   "10.240.0.1:51820",
		RemoteEndpoint:  "198.51.100.89:51820",
		RemoteClusterID: "cluster-azure-ap-northeast-1",
		Protocol:        ProtocolWireGuard,
		Status:          TunnelStatusConnected,
		MTU:             1420,
		PeerPublicKey:   "ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb",
		KeepaliveSec:    25,
		LatencyMs:       145.2,
		PacketLossPct:   0.04,
		JitterMs:        3.4,
		BytesSent:       1240800,
		BytesReceived:   3120400,
		PacketsSent:     1120,
		PacketsReceived: 2480,
	})

	// 3. Dynamic Boundary Overlay Routes
	_ = fe.overlayRouter.AdvertiseRoute(&OverlayRoute{
		ID:              "rt-eu-services",
		DestinationCIDR: "service-*-eu",
		TargetClusterID: "cluster-gcp-eu-central-1",
		NextHopTunnelID: "tun-wg-iad-fra-01",
		Metric:          10,
		ASPath:          []string{"64512", "64513"},
		AdvertisedBy:    "cluster-gcp-eu-central-1",
		Status:          RouteStatusActive,
	})
	_ = fe.overlayRouter.AdvertiseRoute(&OverlayRoute{
		ID:              "rt-ap-services",
		DestinationCIDR: "service-*-ap",
		TargetClusterID: "cluster-azure-ap-northeast-1",
		NextHopTunnelID: "tun-wg-iad-nrt-01",
		Metric:          20,
		ASPath:          []string{"64512", "64514"},
		AdvertisedBy:    "cluster-azure-ap-northeast-1",
		Status:          RouteStatusActive,
	})
	_ = fe.overlayRouter.AdvertiseRoute(&OverlayRoute{
		ID:              "rt-global-default",
		DestinationCIDR: "*",
		TargetClusterID: "cluster-aws-us-east-1",
		NextHopTunnelID: "tun-wg-iad-fra-01",
		Metric:          50,
		ASPath:          []string{"64512"},
		AdvertisedBy:    "cluster-aws-us-east-1",
		Status:          RouteStatusActive,
	})

	// 4. Data Sovereignty & Jurisdiction Compliance Policies
	_ = fe.sovereigntyValidator.AddRule(&SovereigntyRule{
		RuleID:              "sov-gdpr-eu-mandate",
		TenantID:            "*",
		Classification:      ClassificationPII,
		Jurisdiction:        JurisdictionGDPR,
		AllowedRegions:      []string{"eu-central-1", "eu-west-1", "fra", "lhr"},
		EnforceJurisdiction: true,
		StrictCompliance:    true,
		Description:         "Enforce strict European GDPR data residency for all customer PII",
	})
	_ = fe.sovereigntyValidator.AddRule(&SovereigntyRule{
		RuleID:              "sov-ccpa-us-financial",
		TenantID:            "tenant-us-retail",
		Classification:      ClassificationFinancial,
		Jurisdiction:        JurisdictionCCPA,
		AllowedRegions:      []string{"us-east-1", "us-west-2", "iad", "ord"},
		EnforceJurisdiction: true,
		StrictCompliance:    true,
		Description:         "CCPA financial ledger boundary retention for North American retail tenant",
	})
}

func (fe *FederationEngine) runMaintenanceLoop() {
	ticker := time.NewTicker(fe.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-fe.stopChan:
			return
		case <-ticker.C:
			fe.mu.Lock()
			now := time.Now()
			for _, c := range fe.clusters {
				if c.Status == ClusterStatusActive && now.Sub(c.LastHeartbeat) > 3*fe.config.HeartbeatInterval {
					c.Status = ClusterStatusDegraded
				}
			}
			fe.mu.Unlock()
		}
	}
}
