package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kLauncher1/cf-lease-broker/internal/broker"
)

func TestProxyTransparentForwarding(t *testing.T) {
	var receivedPath string
	var receivedMethod string
	var receivedQuery string
	var receivedBody []byte
	var receivedHeader string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedMethod = r.Method
		receivedQuery = r.URL.RawQuery
		receivedHeader = r.Header.Get("X-Client-Trace")
		receivedBody, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream-Echo", "verified")
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"status":"created"}`))
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	defer upstream.Close()

	pm := broker.NewPoolManager()
	le := broker.NewLeaseEngine(pm)
	handler := NewProxyHandler(pm, le)
	handler.TargetURLBuilder = func(service, accountID, subPath string) string {
		return upstream.URL + "/" + service + subPath
	}

	// 1. GET Transparent Forwarding
	req := httptest.NewRequest(http.MethodGet, "/proxy/edge-auth/v1/verify?env=prod&debug=false", nil)
	req.Header.Set("X-Client-Trace", "trace-abc-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-CF-Broker-Account") == "" {
		t.Errorf("expected X-CF-Broker-Account header to be populated")
	}
	if rec.Header().Get("X-CF-Broker-Attempts") != "1" {
		t.Errorf("expected 1 attempt, got %s", rec.Header().Get("X-CF-Broker-Attempts"))
	}
	if rec.Header().Get("X-Upstream-Echo") != "verified" {
		t.Errorf("expected upstream header X-Upstream-Echo to be preserved")
	}
	if receivedPath != "/edge-auth/v1/verify" {
		t.Errorf("expected path /edge-auth/v1/verify, got %s", receivedPath)
	}
	if receivedQuery != "env=prod&debug=false" {
		t.Errorf("expected query params env=prod&debug=false, got %s", receivedQuery)
	}
	if receivedHeader != "trace-abc-123" {
		t.Errorf("expected header trace-abc-123, got %s", receivedHeader)
	}

	// 2. POST with JSON body
	payload := `{"token":"cf_sec_999","user":"developer"}`
	postReq := httptest.NewRequest(http.MethodPost, "/proxy/edge-auth/api/session", bytes.NewBufferString(payload))
	postReq.Header.Set("Content-Type", "application/json")
	postRec := httptest.NewRecorder()

	handler.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", postRec.Code)
	}
	if string(receivedBody) != payload {
		t.Errorf("upstream did not receive expected POST body. Got: %s", string(receivedBody))
	}
	if receivedMethod != http.MethodPost {
		t.Errorf("expected POST method, got %s", receivedMethod)
	}
}

func TestProxyRateLimitFailover(t *testing.T) {
	pm := broker.NewPoolManager()
	le := broker.NewLeaseEngine(pm)

	var attemptedAccounts []string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accID := r.Header.Get("X-Target-Account")
		attemptedAccounts = append(attemptedAccounts, accID)

		// First account hits Cloudflare 429 Too Many Requests
		if accID == "cf-tenant-charlie" {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"10000: Rate limit exceeded for tenant charlie"}`))
			return
		}

		// Subsequent account succeeds
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success_from_backup_account"}`))
	}))
	defer upstream.Close()

	handler := NewProxyHandler(pm, le)
	handler.TargetURLBuilder = func(service, accountID, subPath string) string {
		return upstream.URL + "/" + service + subPath
	}
	// Inject account ID into request header via custom round-trip or target URL builder
	// We can wrap target URL to carry account ID in query or path
	handler.TargetURLBuilder = func(service, accountID, subPath string) string {
		return upstream.URL + subPath + "?acc=" + accountID
	}

	// Overwrite upstream handler to read ?acc=
	upstream.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accID := r.URL.Query().Get("acc")
		attemptedAccounts = append(attemptedAccounts, accID)

		if accID == "cf-tenant-charlie" {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"Rate limit reached"}`))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success"}`))
	})

	req := httptest.NewRequest(http.MethodGet, "/proxy/worker-service/endpoint", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK after failover, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-CF-Broker-Attempts") != "2" {
		t.Errorf("expected 2 attempts, got %s", rec.Header().Get("X-CF-Broker-Attempts"))
	}
	if rec.Header().Get("X-CF-Broker-Account") != "cf-tenant-bravo" {
		t.Errorf("expected failover to cf-tenant-bravo, got %s", rec.Header().Get("X-CF-Broker-Account"))
	}

	// Verify tenant charlie was marked unhealthy
	accounts := pm.ListAccounts()
	for _, acc := range accounts {
		if acc.ID == "cf-tenant-charlie" && acc.IsHealthy {
			t.Errorf("cf-tenant-charlie should be marked unhealthy after triggering failover")
		}
	}
}

func TestProxyPoolExhaustion(t *testing.T) {
	pm := broker.NewPoolManager()
	le := broker.NewLeaseEngine(pm)

	var requestCount int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"Quota exceeded"}`))
	}))
	defer upstream.Close()

	handler := NewProxyHandler(pm, le)
	handler.TargetURLBuilder = func(service, accountID, subPath string) string {
		return upstream.URL + "/" + service + subPath
	}

	req := httptest.NewRequest(http.MethodGet, "/proxy/worker-service/ping", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-CF-Broker-Attempts") != "3" {
		t.Errorf("expected 3 attempts before pool exhaustion, got %s", rec.Header().Get("X-CF-Broker-Attempts"))
	}

	var errResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode JSON error response: %v", err)
	}
	if errResp["attempts"] != float64(3) {
		t.Errorf("expected 3 attempts in JSON response, got %v", errResp["attempts"])
	}

	// All accounts in pool must be unhealthy
	for _, acc := range pm.ListAccounts() {
		if acc.IsHealthy {
			t.Errorf("account %s should be marked unhealthy", acc.ID)
		}
	}
}

func TestProxyHopByHopHeaderFiltering(t *testing.T) {
	var capturedHeaders http.Header

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()

		// Return hop-by-hop headers from upstream as well
		w.Header().Set("Keep-Alive", "timeout=30")
		w.Header().Set("Upgrade", "websocket")
		w.Header().Set("Proxy-Authenticate", "Basic realm=cf")
		w.Header().Set("Te", "deflate")
		w.Header().Set("X-Legit-Header", "allowed-value")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	pm := broker.NewPoolManager()
	le := broker.NewLeaseEngine(pm)
	handler := NewProxyHandler(pm, le)
	handler.TargetURLBuilder = func(service, accountID, subPath string) string {
		return upstream.URL + "/" + service + subPath
	}

	req := httptest.NewRequest(http.MethodGet, "/proxy/worker-service/data", nil)
	req.Header.Set("Connection", "close, X-Custom-Client-Hop")
	req.Header.Set("Keep-Alive", "300")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Proxy-Authorization", "Basic secret")
	req.Header.Set("Proxy-Authenticate", "Basic realm=cf")
	req.Header.Set("TE", "trailers")
	req.Header.Set("Trailers", "X-Trailer")
	req.Header.Set("X-Custom-Client-Hop", "must-strip")
	req.Header.Set("X-Standard-Header", "must-keep")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	// Upstream should NOT see client hop-by-hop headers
	disallowedHeaders := []string{
		"Connection",
		"Keep-Alive",
		"Upgrade",
		"Proxy-Authorization",
		"Proxy-Authenticate",
		"Te",
		"Trailers",
		"X-Custom-Client-Hop",
	}
	for _, h := range disallowedHeaders {
		if capturedHeaders.Get(h) != "" {
			t.Errorf("upstream received forbidden hop-by-hop header: %s = %s", h, capturedHeaders.Get(h))
		}
	}
	if capturedHeaders.Get("X-Standard-Header") != "must-keep" {
		t.Errorf("standard header was unexpectedly removed")
	}

	// Client response should NOT have upstream hop-by-hop headers
	if rec.Header().Get("Upgrade") != "" {
		t.Errorf("client received upstream Upgrade header")
	}
	if rec.Header().Get("Keep-Alive") != "" {
		t.Errorf("client received upstream Keep-Alive header")
	}
	if rec.Header().Get("Proxy-Authenticate") != "" {
		t.Errorf("client received upstream Proxy-Authenticate header")
	}
	if rec.Header().Get("Te") != "" {
		t.Errorf("client received upstream Te header")
	}
	if rec.Header().Get("X-Legit-Header") != "allowed-value" {
		t.Errorf("legitimate upstream header was not forwarded")
	}
}

func TestProxyMalformedRequests(t *testing.T) {
	pm := broker.NewPoolManager()
	le := broker.NewLeaseEngine(pm)
	handler := NewProxyHandler(pm, le)

	testCases := []struct {
		name       string
		path       string
		expectCode int
	}{
		{"Bare proxy without trailing slash", "/proxy", http.StatusBadRequest},
		{"Bare proxy with trailing slash", "/proxy/", http.StatusBadRequest},
		{"Non-proxy route", "/api/not-proxy", http.StatusBadRequest},
		{"Empty service name", "/proxy//subpath", http.StatusBadRequest},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.expectCode {
				t.Errorf("path %s: expected status %d, got %d", tc.path, tc.expectCode, rec.Code)
			}
		})
	}
}

func TestProxyLargeBodyRejection(t *testing.T) {
	pm := broker.NewPoolManager()
	le := broker.NewLeaseEngine(pm)
	handler := NewProxyHandler(pm, le)
	handler.MaxBodyBytes = 100 // Set low limit for testing

	bigPayload := strings.Repeat("A", 200)
	req := httptest.NewRequest(http.MethodPost, "/proxy/worker-service/upload", strings.NewReader(bigPayload))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413 Request Entity Too Large, got %d", rec.Code)
	}
}
