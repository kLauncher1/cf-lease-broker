package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kLauncher1/cf-lease-broker/internal/broker"
)

// DefaultMaxBodyBytes limits incoming request bodies buffered for failover retries to 10MB.
const DefaultMaxBodyBytes = 10 * 1024 * 1024

// DefaultMaxRetries is the maximum number of accounts attempted before failing.
const DefaultMaxRetries = 3

// TargetURLBuilderFunc allows custom upstream URL construction (e.g. for testing with httptest).
type TargetURLBuilderFunc func(service string, accountID string, subPath string) string

// ProxyHandler implements transparent reverse proxying with automatic 429 interception and failover.
type ProxyHandler struct {
	poolManager      *broker.PoolManager
	leaseEngine      *broker.LeaseEngine
	httpClient       *http.Client
	TargetURLBuilder TargetURLBuilderFunc
	CertVerifier     func(r *http.Request) error
	UsageRecorder    func(tenantID, accountID, service string, cpuMs, bytesIn, bytesOut int64)
	ColoResolver     func(r *http.Request) (colo string, distanceKm float64)
	SteeringResolver func(r *http.Request) (mode string, latencyUs float64, target string)
	SandboxResolver  func(r *http.Request) (status string, memoryPages int)
	MeshAffinityResolver func(r *http.Request) (nodeID, region, shardKey string, isLocal bool)
	TelemetryResolver func(r *http.Request) (status string, rttMs float64, retransRate float64, mitigated bool)
	CircuitChecker    func(accountID string) bool
	TranscoderResolver func(r *http.Request, respHeader http.Header, body []byte) (compressed []byte, encoding string, savings float64)
	ReplicationResolver func(r *http.Request) (status string, clock string)
	MaxRetries       int
	MaxBodyBytes     int64
}

// NewProxyHandler constructs an active reverse proxy backed by account pools and lease metadata.
func NewProxyHandler(poolMgr *broker.PoolManager, leaseEng *broker.LeaseEngine) *ProxyHandler {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &ProxyHandler{
		poolManager:  poolMgr,
		leaseEngine:  leaseEng,
		httpClient:   client,
		MaxRetries:   DefaultMaxRetries,
		MaxBodyBytes: DefaultMaxBodyBytes,
	}
}

// ServeHTTP transparently handles requests to /proxy/{service}/* with automated failover.
func (p *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	service, subPath, err := parseProxyPath(r.URL.Path)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":   err.Error(),
			"example": "/proxy/{service}/sub-path",
		})
		return
	}

	// Validate client mTLS certificate if an upstream policy verifier is attached
	if p.CertVerifier != nil {
		if err := p.CertVerifier(r); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":   "mtls_client_verification_failed",
				"message": err.Error(),
			})
			return
		}
	}

	// Buffer request body in memory to support transparent retries across accounts
	bodyBytes, err := p.bufferRequestBody(r)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if errors.Is(err, errBodyTooLarge) {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "request body exceeds maximum allowed buffer size"})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to buffer request body"})
		return
	}

	maxAttempts := p.MaxRetries
	if maxAttempts <= 0 {
		maxAttempts = DefaultMaxRetries
	}

	startTime := time.Now()
	attempts := 0

	for attempts < maxAttempts {
		if r.Context().Err() != nil {
			return
		}

		account, err := p.poolManager.SelectOptimalAccount()
		if err != nil {
			p.writePoolExhaustedError(w, attempts, "all pooled accounts exhausted or unhealthy")
			return
		}

		// Immediate sub-10ms bypass if circuit breaker is tripped for this account
		if p.CircuitChecker != nil && p.CircuitChecker(account.ID) {
			_ = p.poolManager.TriggerFailover(account.ID)
			continue
		}

		attempts++

		targetURL := p.buildTargetURL(service, account.ID, subPath, r.URL.RawQuery)
		upstreamReq, err := p.createUpstreamRequest(r, targetURL, bodyBytes)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to create upstream request"})
			return
		}

		resp, err := p.httpClient.Do(upstreamReq)
		if err != nil {
			// Network timeout, connection refused, or TLS failure triggers failover
			_ = p.poolManager.TriggerFailover(account.ID)
			continue
		}

		// Intercept 429 Too Many Requests or Gateway errors for failover
		if isFailoverStatusCode(resp.StatusCode) {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()

			_ = p.poolManager.TriggerFailover(account.ID)
			continue
		}

		// Successful or non-transient status: stream upstream response back to caller
		p.forwardResponse(w, r, resp, account.ID, attempts)
		if p.UsageRecorder != nil {
			tenantID := r.Header.Get("X-Tenant-ID")
			if tenantID == "" {
				tenantID = "default"
			}
			durationMs := time.Since(startTime).Milliseconds()
			bytesIn := int64(len(bodyBytes))
			p.UsageRecorder(tenantID, account.ID, service, durationMs, bytesIn, 0)
		}
		return
	}

	p.writePoolExhaustedError(w, attempts, "retry attempts exhausted across all accounts")
}

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

var errBodyTooLarge = errors.New("request body too large")

func (p *ProxyHandler) bufferRequestBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer func() { _ = r.Body.Close() }()

	maxBytes := p.MaxBodyBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBodyBytes
	}

	limitedReader := io.LimitReader(r.Body, maxBytes+1)
	buf, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > maxBytes {
		return nil, errBodyTooLarge
	}
	return buf, nil
}

func (p *ProxyHandler) buildTargetURL(service, accountID, subPath, rawQuery string) string {
	var dest string
	if p.TargetURLBuilder != nil {
		dest = p.TargetURLBuilder(service, accountID, subPath)
	} else {
		trimmed := strings.TrimPrefix(subPath, "/")
		if trimmed != "" {
			dest = fmt.Sprintf("https://%s.%s.workers.dev/%s", service, accountID, trimmed)
		} else {
			dest = fmt.Sprintf("https://%s.%s.workers.dev", service, accountID)
		}
	}

	if rawQuery != "" {
		if strings.Contains(dest, "?") {
			dest += "&" + rawQuery
		} else {
			dest += "?" + rawQuery
		}
	}
	return dest
}

func (p *ProxyHandler) createUpstreamRequest(r *http.Request, targetURL string, body []byte) (*http.Request, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bodyReader)
	if err != nil {
		return nil, err
	}

	// Dynamic hop-by-hop header removal based on RFC 7230
	connectionTokens := make(map[string]bool)
	for _, val := range r.Header["Connection"] {
		for _, token := range strings.Split(val, ",") {
			trimmed := strings.TrimSpace(token)
			if trimmed != "" {
				connectionTokens[http.CanonicalHeaderKey(trimmed)] = true
			}
		}
	}

	for key, values := range r.Header {
		canonical := http.CanonicalHeaderKey(key)
		if isHopByHop(canonical) || connectionTokens[canonical] {
			continue
		}
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}

	// Preserve / append X-Forwarded-For
	if clientIP, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		prior := req.Header.Get("X-Forwarded-For")
		if prior != "" {
			clientIP = prior + ", " + clientIP
		}
		req.Header.Set("X-Forwarded-For", clientIP)
	}

	return req, nil
}

func (p *ProxyHandler) forwardResponse(w http.ResponseWriter, r *http.Request, resp *http.Response, accountID string, attempts int) {
	defer func() { _ = resp.Body.Close() }()

	// Strip hop-by-hop headers from upstream response
	respConnectionTokens := make(map[string]bool)
	for _, val := range resp.Header["Connection"] {
		for _, token := range strings.Split(val, ",") {
			trimmed := strings.TrimSpace(token)
			if trimmed != "" {
				respConnectionTokens[http.CanonicalHeaderKey(trimmed)] = true
			}
		}
	}

	for key, values := range resp.Header {
		canonical := http.CanonicalHeaderKey(key)
		if isHopByHop(canonical) || respConnectionTokens[canonical] {
			continue
		}
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}

	// Broker telemetry headers
	w.Header().Set("X-CF-Broker-Account", accountID)
	w.Header().Set("X-CF-Broker-Attempts", strconv.Itoa(attempts))
	if p.ColoResolver != nil && r != nil {
		if colo, dist := p.ColoResolver(r); colo != "" {
			w.Header().Set("X-CF-Broker-Colo", colo)
			w.Header().Set("X-CF-Broker-Distance-Km", fmt.Sprintf("%.1f", dist))
		}
	}
	if p.SteeringResolver != nil && r != nil {
		if mode, latencyUs, target := p.SteeringResolver(r); mode != "" {
			w.Header().Set("X-CF-Broker-Steering-Mode", mode)
			w.Header().Set("X-CF-Broker-Steering-Latency-Us", fmt.Sprintf("%.2f", latencyUs))
			if target != "" {
				w.Header().Set("X-CF-Broker-Steering-Target", target)
			}
		}
	}
	if p.SandboxResolver != nil && r != nil {
		if status, pages := p.SandboxResolver(r); status != "" {
			w.Header().Set("X-CF-Broker-Sandbox-Status", status)
			w.Header().Set("X-CF-Broker-Sandbox-Pages", strconv.Itoa(pages))
		}
	}
	if p.MeshAffinityResolver != nil && r != nil {
		if nodeID, region, shardKey, isLocal := p.MeshAffinityResolver(r); nodeID != "" {
			w.Header().Set("X-CF-Broker-Mesh-Node", nodeID)
			w.Header().Set("X-CF-Broker-Mesh-Region", region)
			w.Header().Set("X-CF-Broker-Mesh-Shard", shardKey)
			if isLocal {
				w.Header().Set("X-CF-Broker-Mesh-Affinity", "local")
			} else {
				w.Header().Set("X-CF-Broker-Mesh-Affinity", "steered")
			}
		}
	}
	if p.TelemetryResolver != nil && r != nil {
		if status, rttMs, retransRate, mitigated := p.TelemetryResolver(r); status != "" {
			w.Header().Set("X-CF-Broker-Telemetry-Status", status)
			w.Header().Set("X-CF-Broker-Telemetry-RTT-Ms", fmt.Sprintf("%.2f", rttMs))
			w.Header().Set("X-CF-Broker-Telemetry-Retrans-Rate", fmt.Sprintf("%.4f", retransRate))
			if mitigated {
				w.Header().Set("X-CF-Broker-Telemetry-Mitigated", "true")
			}
		}
	}

	if p.ReplicationResolver != nil && r != nil {
		status, clock := p.ReplicationResolver(r)
		if status != "" {
			w.Header().Set("X-CF-Broker-Consistency", status)
		}
		if clock != "" {
			w.Header().Set("X-CF-Broker-VectorClock", clock)
		}
	}

	if p.TranscoderResolver != nil && r != nil {
		rawBody, err := io.ReadAll(resp.Body)
		if err == nil {
			comp, encoding, savings := p.TranscoderResolver(r, resp.Header, rawBody)
			if encoding != "" && len(comp) > 0 {
				w.Header().Set("Content-Encoding", encoding)
				w.Header().Set("X-CF-Broker-Compression", encoding)
				w.Header().Set("X-CF-Broker-Savings-Percent", fmt.Sprintf("%.2f%%", savings))
				w.Header().Del("Content-Length")
				w.WriteHeader(resp.StatusCode)
				_, _ = w.Write(comp)
				return
			}
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(rawBody)
			return
		}
	}

	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// SetTranscoderResolver registers an adaptive compression transcoder for proxy responses.
func (p *ProxyHandler) SetTranscoderResolver(fn func(r *http.Request, respHeader http.Header, body []byte) ([]byte, string, float64)) {
	p.TranscoderResolver = fn
}

// SetReplicationResolver registers a geo-replication consistency resolver for proxy responses.
func (p *ProxyHandler) SetReplicationResolver(fn func(r *http.Request) (string, string)) {
	p.ReplicationResolver = fn
}

func (p *ProxyHandler) writePoolExhaustedError(w http.ResponseWriter, attempts int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-CF-Broker-Attempts", strconv.Itoa(attempts))
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":    message,
		"attempts": attempts,
	})
}

func parseProxyPath(path string) (service, subPath string, err error) {
	if !strings.HasPrefix(path, "/proxy/") && path != "/proxy" {
		return "", "", errors.New("invalid route: path must start with /proxy/")
	}

	remainder := strings.TrimPrefix(path, "/proxy")
	remainder = strings.TrimPrefix(remainder, "/")
	if remainder == "" {
		return "", "", errors.New("missing service identifier in proxy path")
	}

	parts := strings.SplitN(remainder, "/", 2)
	service = parts[0]
	if service == "" {
		return "", "", errors.New("service identifier cannot be empty")
	}

	if len(parts) > 1 {
		subPath = "/" + parts[1]
	}
	return service, subPath, nil
}

func isFailoverStatusCode(status int) bool {
	return status == http.StatusTooManyRequests || // 429
		status == http.StatusBadGateway ||          // 502
		status == http.StatusServiceUnavailable ||  // 503
		status == http.StatusGatewayTimeout         // 504
}

func isHopByHop(canonicalKey string) bool {
	switch canonicalKey {
	case "Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Te",
		"Trailers",
		"Transfer-Encoding",
		"Upgrade":
		return true
	default:
		return false
	}
}
