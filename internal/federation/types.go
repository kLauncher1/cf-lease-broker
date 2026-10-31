package federation

import (
	"errors"
	"time"
)

// Common errors returned by the federation subsystem.
var (
	ErrClusterNotFound      = errors.New("cluster node not found")
	ErrClusterAlreadyExists = errors.New("cluster node already exists")
	ErrTunnelNotFound       = errors.New("WAN tunnel not found")
	ErrTunnelAlreadyExists  = errors.New("WAN tunnel already exists")
	ErrRouteNotFound        = errors.New("overlay route not found")
	ErrNoViableRoute        = errors.New("no viable overlay route available")
	ErrSovereigntyViolation = errors.New("data sovereignty policy violation")
	ErrInvalidConfiguration = errors.New("invalid federation configuration")
)

// CloudProvider represents supported multi-cloud infrastructure environments.
type CloudProvider string

const (
	CloudProviderAWS             CloudProvider = "aws"
	CloudProviderGCP             CloudProvider = "gcp"
	CloudProviderAzure           CloudProvider = "azure"
	CloudProviderBareMetal       CloudProvider = "baremetal"
	CloudProviderCloudflareEdge  CloudProvider = "cloudflare-edge"
)

// ClusterStatus represents the operational reachability of a federated cluster node.
type ClusterStatus string

const (
	ClusterStatusActive      ClusterStatus = "active"
	ClusterStatusDegraded    ClusterStatus = "degraded"
	ClusterStatusUnreachable ClusterStatus = "unreachable"
)

// ClusterNode represents a gateway broker instance in a multi-datacenter federation.
type ClusterNode struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Provider       CloudProvider     `json:"provider"`
	Region         string            `json:"region"`
	PublicEndpoint string            `json:"public_endpoint"`
	OverlayIP      string            `json:"overlay_ip"`
	Status         ClusterStatus     `json:"status"`
	LatencyMs      float64           `json:"latency_ms"`
	Capabilities   []string          `json:"capabilities"`
	LastHeartbeat  time.Time         `json:"last_heartbeat"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// TunnelProtocol identifies the overlay WAN transport protocol.
type TunnelProtocol string

const (
	ProtocolWireGuard TunnelProtocol = "wireguard"
	ProtocolTLSMesh   TunnelProtocol = "tls-mesh"
	ProtocolIPsec     TunnelProtocol = "ipsec"
)

// TunnelStatus represents the state of a point-to-point WAN tunnel.
type TunnelStatus string

const (
	TunnelStatusConnected    TunnelStatus = "connected"
	TunnelStatusHandshaking  TunnelStatus = "handshaking"
	TunnelStatusReconnecting TunnelStatus = "reconnecting"
	TunnelStatusDegraded     TunnelStatus = "degraded"
	TunnelStatusClosed       TunnelStatus = "closed"
)

// WANTunnel models an encrypted cross-datacenter overlay communication tunnel.
type WANTunnel struct {
	ID              string         `json:"id"`
	LocalEndpoint   string         `json:"local_endpoint"`
	RemoteEndpoint  string         `json:"remote_endpoint"`
	RemoteClusterID string         `json:"remote_cluster_id"`
	Protocol        TunnelProtocol `json:"protocol"`
	Status          TunnelStatus   `json:"status"`
	MTU             int            `json:"mtu"`
	PeerPublicKey   string         `json:"peer_public_key"`
	KeepaliveSec    int            `json:"keepalive_sec"`
	LatencyMs       float64        `json:"latency_ms"`
	PacketLossPct   float64        `json:"packet_loss_pct"`
	JitterMs        float64        `json:"jitter_ms"`
	BytesSent       int64          `json:"bytes_sent"`
	BytesReceived   int64          `json:"bytes_received"`
	PacketsSent     int64          `json:"packets_sent"`
	PacketsReceived int64          `json:"packets_received"`
	LastHandshake   time.Time      `json:"last_handshake"`
	EstablishedAt   time.Time      `json:"established_at"`
}

// RouteStatus denotes whether an advertised boundary route is actively routable.
type RouteStatus string

const (
	RouteStatusActive    RouteStatus = "active"
	RouteStatusWithdrawn RouteStatus = "withdrawn"
	RouteStatusStale     RouteStatus = "stale"
)

// OverlayRoute represents an advertised boundary route across the WAN overlay mesh.
type OverlayRoute struct {
	ID              string      `json:"id"`
	DestinationCIDR string      `json:"destination_cidr"`
	TargetClusterID string      `json:"target_cluster_id"`
	NextHopTunnelID string      `json:"next_hop_tunnel_id"`
	Metric          int         `json:"metric"`
	ASPath          []string    `json:"as_path"`
	AdvertisedBy    string      `json:"advertised_by"`
	Status          RouteStatus `json:"status"`
	LastAdvertised  time.Time   `json:"last_advertised"`
}

// DataClassification identifies regulatory data privacy classifications.
type DataClassification string

const (
	ClassificationGeneral    DataClassification = "general"
	ClassificationPII        DataClassification = "pii"
	ClassificationFinancial  DataClassification = "financial"
	ClassificationHealthcare DataClassification = "healthcare"
)

// SovereigntyJurisdiction specifies geographic legal regulatory boundaries.
type SovereigntyJurisdiction string

const (
	JurisdictionGlobal SovereigntyJurisdiction = "global"
	JurisdictionGDPR   SovereigntyJurisdiction = "gdpr" // European Union
	JurisdictionCCPA   SovereigntyJurisdiction = "ccpa" // United States / California
	JurisdictionAPPI   SovereigntyJurisdiction = "appi" // Japan
)

// SovereigntyRule enforces jurisdictional boundaries and regional residency.
type SovereigntyRule struct {
	RuleID              string                  `json:"rule_id"`
	TenantID            string                  `json:"tenant_id"`
	Classification      DataClassification      `json:"classification"`
	Jurisdiction        SovereigntyJurisdiction `json:"jurisdiction"`
	AllowedRegions      []string                `json:"allowed_regions"`
	BlockedRegions      []string                `json:"blocked_regions"`
	EnforceJurisdiction bool                    `json:"enforce_jurisdiction"`
	StrictCompliance    bool                    `json:"strict_compliance"`
	Description         string                  `json:"description"`
}

// ComplianceCheckResult records the evaluation outcome of data residency validation.
type ComplianceCheckResult struct {
	Allowed         bool               `json:"allowed"`
	TenantID        string             `json:"tenant_id"`
	TargetClusterID string             `json:"target_cluster_id"`
	TargetRegion    string             `json:"target_region"`
	Classification  DataClassification `json:"classification"`
	MatchedRuleID   string             `json:"matched_rule_id,omitempty"`
	ViolationReason string             `json:"violation_reason,omitempty"`
	EvaluatedAt     time.Time          `json:"evaluated_at"`
}

// FederationDecision encapsulates the complete routing decision for a federated request.
type FederationDecision struct {
	ShouldFederate   bool                  `json:"should_federate"`
	TargetCluster    *ClusterNode          `json:"target_cluster,omitempty"`
	SelectedTunnel   *WANTunnel            `json:"selected_tunnel,omitempty"`
	SelectedRoute    *OverlayRoute         `json:"selected_route,omitempty"`
	SovereigntyCheck ComplianceCheckResult `json:"sovereignty_check"`
	RoutingLatencyMs float64               `json:"routing_latency_ms"`
	Reason           string                `json:"reason"`
}

// FederationConfig defines configuration parameters for the multi-cluster federation engine.
type FederationConfig struct {
	LocalClusterID         string        `json:"local_cluster_id"`
	LocalRegion            string        `json:"local_region"`
	LocalProvider          CloudProvider `json:"local_provider"`
	ListenAddress          string        `json:"listen_address"`
	OverlayCIDR            string        `json:"overlay_cidr"`
	SecretKey              []byte        `json:"-"`
	HeartbeatInterval      time.Duration `json:"heartbeat_interval"`
	TunnelHandshakeTimeout time.Duration `json:"tunnel_handshake_timeout"`
}

// FederationStats aggregates metrics and operational telemetry for the federation subsystem.
type FederationStats struct {
	LocalClusterID              string        `json:"local_cluster_id"`
	LocalRegion                 string        `json:"local_region"`
	TotalClusters               int           `json:"total_clusters"`
	ActiveClusters              int           `json:"active_clusters"`
	TotalTunnels                int           `json:"total_tunnels"`
	ConnectedTunnels            int           `json:"connected_tunnels"`
	TotalRoutes                 int           `json:"total_routes"`
	ActiveRoutes                int           `json:"active_routes"`
	TotalSovereigntyRules       int           `json:"total_sovereignty_rules"`
	ComplianceViolationsBlocked int64         `json:"compliance_violations_blocked"`
	FederatedRequestsForwarded  int64         `json:"federated_requests_forwarded"`
	AverageWANLatencyMs         float64       `json:"average_wan_latency_ms"`
	TotalBytesTransferred       int64         `json:"total_bytes_transferred"`
	LastTopologyUpdate          time.Time     `json:"last_topology_update"`
}
