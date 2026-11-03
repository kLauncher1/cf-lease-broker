package proxy

import (
	"net/http"
)

// SetUsageRecorder attaches an optional telemetry callback for usage and billing metering.
func (p *ProxyHandler) SetUsageRecorder(recorder func(tenantID, accountID, service string, cpuMs, bytesIn, bytesOut int64)) {
	p.UsageRecorder = recorder
}

// SetColoResolver attaches an optional resolver to annotate proxy telemetry headers with nearest edge PoP.
func (p *ProxyHandler) SetColoResolver(resolver func(r *http.Request) (colo string, distanceKm float64)) {
	p.ColoResolver = resolver
}

// SetSteeringResolver attaches an optional packet steering resolver to annotate responses with steering telemetry.
func (p *ProxyHandler) SetSteeringResolver(resolver func(r *http.Request) (mode string, latencyUs float64, target string)) {
	p.SteeringResolver = resolver
}

// SetSandboxResolver attaches an optional Wasm sandbox state resolver to annotate proxy telemetry headers.
func (p *ProxyHandler) SetSandboxResolver(resolver func(r *http.Request) (status string, memoryPages int)) {
	p.SandboxResolver = resolver
}

// SetMeshAffinityResolver attaches an optional edge mesh affinity resolver to annotate proxy telemetry headers.
func (p *ProxyHandler) SetMeshAffinityResolver(resolver func(r *http.Request) (nodeID, region, shardKey string, isLocal bool)) {
	p.MeshAffinityResolver = resolver
}

// SetTelemetryResolver attaches an optional eBPF telemetry resolver to annotate proxy telemetry headers.
func (p *ProxyHandler) SetTelemetryResolver(resolver func(r *http.Request) (status string, rttMs float64, retransRate float64, mitigated bool)) {
	p.TelemetryResolver = resolver
}

// SetCircuitChecker attaches an optional circuit breaker checker to trigger instant sub-10ms account failover if tripped.
func (p *ProxyHandler) SetCircuitChecker(checker func(accountID string) bool) {
	p.CircuitChecker = checker
}

// SetTranscoderResolver registers an adaptive compression transcoder for proxy responses.
func (p *ProxyHandler) SetTranscoderResolver(fn func(r *http.Request, respHeader http.Header, body []byte) ([]byte, string, float64)) {
	p.TranscoderResolver = fn
}

// SetReplicationResolver registers a geo-replication consistency resolver for proxy responses.
func (p *ProxyHandler) SetReplicationResolver(fn func(r *http.Request) (string, string)) {
	p.ReplicationResolver = fn
}

// SetFederationResolver registers a multi-cluster federation route resolver for proxy responses.
func (p *ProxyHandler) SetFederationResolver(fn func(r *http.Request) (clusterID, route, sovereigntyZone string, isFederated bool)) {
	p.FederationResolver = fn
}

// SetZKProofResolver attaches an optional zero-knowledge proof verification resolver.
func (p *ProxyHandler) SetZKProofResolver(fn func(r *http.Request) (status, statement string, latencyUs int64)) {
	p.ZKProofResolver = fn
}

// SetPQCResolver registers a post-quantum cryptographic resolver for proxy responses.
func (p *ProxyHandler) SetPQCResolver(fn func(r *http.Request) (kemAlg, dsaAlg, hybridStatus string, latencyUs int64)) {
	p.PQCResolver = fn
}

// SetWasmDedupResolver attaches a Wasm memory deduplication and page sharing resolver.
func (p *ProxyHandler) SetWasmDedupResolver(fn func(r *http.Request) (status string, sharedPages int, savedBytes int64, resurrectUs int64)) {
	p.WasmDedupResolver = fn
}

// SetScrubberResolver registers a line-rate DDoS scrubber resolver for proxy responses.
func (p *ProxyHandler) SetScrubberResolver(fn func(r *http.Request) (status string, score float64, latencyUs int64)) {
	p.ScrubberResolver = fn
}

// SetEnclaveResolver registers a hardware secure enclave attestation resolver for proxy responses.
func (p *ProxyHandler) SetEnclaveResolver(fn func(r *http.Request) (enclaveType, status, quoteID string, latencyUs int64)) {
	p.EnclaveResolver = fn
}

// SetEntropyResolver registers a quantum entropy beacon and hardware RNG resolver for proxy responses.
func (p *ProxyHandler) SetEntropyResolver(fn func(r *http.Request) (source string, minBits float64, beaconRound uint64, latencyUs int64)) {
	p.EntropyResolver = fn
}

