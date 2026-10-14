package compiler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestJavaScriptMinifier(t *testing.T) {
	rawJS := `
		// Cloudflare Edge Worker Handler
		/* Multi-line header comment
		   describing microservice router */
		export default {
			async fetch(request, env, ctx) {
				const url = "https://api.cloudflare.com/client/v4";
				// Inline trailing comment
				const greeting = 'Hello, \'World\'!';
				let counter = 1 + 2 * 3;
				return new Response(greeting, { status: 200 });
			}
		};
	`

	minified, hasExport := MinifyJavaScript([]byte(rawJS))
	if !hasExport {
		t.Errorf("expected hasExport to be true, got false")
	}

	str := string(minified)
	if strings.Contains(str, "Cloudflare Edge Worker Handler") || strings.Contains(str, "Multi-line header comment") || strings.Contains(str, "Inline trailing comment") || strings.Contains(str, "/*") {
		t.Errorf("minified output still contains comments: %s", str)
	}

	if !strings.Contains(str, "https://api.cloudflare.com/client/v4") {
		t.Errorf("url string was corrupted during minification: %s", str)
	}

	if !strings.Contains(str, "Hello, \\'World\\'!") {
		t.Errorf("escaped quotes were corrupted: %s", str)
	}

	// Verify size reduction
	if len(minified) >= len(rawJS) {
		t.Errorf("expected minified length (%d) < original length (%d)", len(minified), len(rawJS))
	}
}

func TestJavaScriptMinifierEmptyAndNoExport(t *testing.T) {
	emptyMin, hasExport := MinifyJavaScript([]byte(""))
	if len(emptyMin) != 0 || hasExport {
		t.Errorf("expected empty result for empty script")
	}

	noExportJS := `const a = 10; const b = 20; console.log(a + b);`
	min, exp := MinifyJavaScript([]byte(noExportJS))
	if exp {
		t.Errorf("expected no export for simple script, got true")
	}
	if !strings.Contains(string(min), "console.log(a+b)") && !strings.Contains(string(min), "console.log(a + b)") {
		t.Errorf("unexpected minification: %s", string(min))
	}
}

func TestWasmValidator(t *testing.T) {
	validWasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x05}
	if err := ValidateWasmModule(validWasm); err != nil {
		t.Fatalf("expected valid wasm module, got error: %v", err)
	}

	shortWasm := []byte{0x00, 0x61}
	if err := ValidateWasmModule(shortWasm); !errors.Is(err, ErrInvalidWasmHeader) {
		t.Errorf("expected ErrInvalidWasmHeader for short wasm, got %v", err)
	}

	badMagic := []byte{0x7f, 0x45, 0x4c, 0x46, 0x01, 0x00, 0x00, 0x00}
	if err := ValidateWasmModule(badMagic); !errors.Is(err, ErrInvalidWasmHeader) {
		t.Errorf("expected ErrInvalidWasmHeader for ELF binary, got %v", err)
	}

	badVersion := []byte{0x00, 0x61, 0x73, 0x6d, 0x02, 0x00, 0x00, 0x00}
	if err := ValidateWasmModule(badVersion); err == nil {
		t.Errorf("expected error for wasm v2, got nil")
	}
}

func TestBundleManagerLifecycle(t *testing.T) {
	mgr := NewBundleManager()

	cfg := BundleConfig{
		TenantID:          "tenant-alpha",
		Name:              "auth-worker",
		Minify:            true,
		CompatibilityDate: "2024-04-01",
		MemoryLimitMB:     256,
		CPUTimeLimitMs:    100,
		EnvBindings:       map[string]string{"MODE": "production"},
	}

	script := `
		// Worker script
		export default {
			fetch(req) { return new Response("OK"); }
		};
	`

	bundle, err := mgr.PackageWorker(cfg, []byte(script), nil)
	if err != nil {
		t.Fatalf("PackageWorker failed: %v", err)
	}

	if bundle.TenantID != "tenant-alpha" || bundle.Name != "auth-worker" {
		t.Errorf("unexpected bundle attributes: %+v", bundle)
	}
	if !bundle.HasValidExport {
		t.Errorf("expected HasValidExport=true")
	}
	if bundle.OriginalBytes <= bundle.MinifiedBytes {
		t.Errorf("expected OriginalBytes > MinifiedBytes")
	}

	// Retrieve by ID
	retrieved, found := mgr.GetBundle(bundle.ID)
	if !found || retrieved.ID != bundle.ID {
		t.Errorf("GetBundle failed to retrieve bundle")
	}

	// List bundles
	list := mgr.ListBundles()
	if len(list) != 1 {
		t.Errorf("expected 1 bundle in list, got %d", len(list))
	}

	// Duplicate bundle rejection for same tenant & name
	_, dupErr := mgr.PackageWorker(cfg, []byte(script), nil)
	if !errors.Is(dupErr, ErrDuplicateBundle) {
		t.Errorf("expected ErrDuplicateBundle, got %v", dupErr)
	}

	// Undeploy
	if err := mgr.UndeployBundle(bundle.ID); err != nil {
		t.Errorf("UndeployBundle failed: %v", err)
	}
	if _, found := mgr.GetBundle(bundle.ID); found {
		t.Errorf("bundle should be gone after undeploy")
	}

	// Undeploy non-existent
	if err := mgr.UndeployBundle("non-existent"); !errors.Is(err, ErrBundleNotFound) {
		t.Errorf("expected ErrBundleNotFound, got %v", err)
	}
}

func TestRouteCompilerMatchingAndPriorities(t *testing.T) {
	rc := NewRouteCompiler()

	// Register multiple routes with different specificity
	rCatchAll, _ := rc.RegisterRoute("*/*", "zone-global", "tenant-1", "worker-fallback")
	rWildcardHost, _ := rc.RegisterRoute("*.example.com/*", "zone-ex", "tenant-1", "worker-wildcard")
	rExactHost, _ := rc.RegisterRoute("api.example.com/*", "zone-ex", "tenant-1", "worker-api")
	rExactPath, _ := rc.RegisterRoute("api.example.com/v1/auth/*", "zone-ex", "tenant-1", "worker-auth")

	tests := []struct {
		host     string
		path     string
		expected *RouteBinding
	}{
		{"api.example.com", "/v1/auth/login", rExactPath},
		{"api.example.com", "/v2/users", rExactHost},
		{"shop.example.com", "/items", rWildcardHost},
		{"otherdomain.org", "/anything", rCatchAll},
	}

	for _, tc := range tests {
		match, found := rc.Match(tc.host, tc.path)
		if !found {
			t.Errorf("expected match for %s%s, got none", tc.host, tc.path)
			continue
		}
		if match.ID != tc.expected.ID {
			t.Errorf("for %s%s expected route %s (%s), got %s (%s)",
				tc.host, tc.path, tc.expected.ID, tc.expected.Pattern, match.ID, match.Pattern)
		}
	}

	// Test route removal
	if err := rc.RemoveRoute(rExactPath.ID); err != nil {
		t.Fatalf("RemoveRoute failed: %v", err)
	}

	// After removing /v1/auth/*, api.example.com/v1/auth/login should fall back to api.example.com/*
	matchFallback, found := rc.Match("api.example.com", "/v1/auth/login")
	if !found || matchFallback.ID != rExactHost.ID {
		t.Errorf("expected fallback to %s, got %+v", rExactHost.ID, matchFallback)
	}

	// List routes ordering
	allRoutes := rc.ListRoutes()
	if len(allRoutes) != 3 {
		t.Errorf("expected 3 routes, got %d", len(allRoutes))
	}
	for i := 0; i < len(allRoutes)-1; i++ {
		if allRoutes[i].Priority < allRoutes[i+1].Priority {
			t.Errorf("routes not sorted by priority descending: index %d (%d) < index %d (%d)",
				i, allRoutes[i].Priority, i+1, allRoutes[i+1].Priority)
		}
	}
}

func TestZeroCopyStreamPipe(t *testing.T) {
	pipe := NewStreamPipe(32 * 1024)

	// Generate 128KB payload
	data := bytes.Repeat([]byte("0123456789abcdef"), 8*1024)
	src := bytes.NewReader(data)

	rec := httptest.NewRecorder()
	ctx := context.Background()

	metrics, err := pipe.PipeStream(ctx, rec, src, "application/octet-stream", int64(len(data)))
	if err != nil {
		t.Fatalf("PipeStream failed: %v", err)
	}

	if !metrics.Completed {
		t.Errorf("expected stream to complete")
	}
	if metrics.BytesWritten != int64(len(data)) {
		t.Errorf("expected %d bytes written, got %d", len(data), metrics.BytesWritten)
	}
	if metrics.ChunksWritten != 4 { // 128KB / 32KB = 4 chunks
		t.Errorf("expected 4 chunks, got %d", metrics.ChunksWritten)
	}

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(body, data) {
		t.Errorf("streamed content mismatch")
	}
	if resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Errorf("missing X-Accel-Buffering: no header")
	}
}

func TestZeroCopyStreamPipeCancellation(t *testing.T) {
	pipe := NewStreamPipe(16 * 1024)
	infiniteReader := &slowInfiniteReader{}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after short delay
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	rec := httptest.NewRecorder()
	metrics, err := pipe.PipeStream(ctx, rec, infiniteReader, "text/plain", -1)
	if !errors.Is(err, ErrClientDisconnected) {
		t.Errorf("expected ErrClientDisconnected, got %v", err)
	}
	if metrics.Completed {
		t.Errorf("expected metrics.Completed=false on canceled stream")
	}
}

type slowInfiniteReader struct{}

func (r *slowInfiniteReader) Read(p []byte) (n int, err error) {
	time.Sleep(10 * time.Millisecond)
	for i := range p {
		p[i] = 'A'
	}
	return len(p), nil
}

func TestCompilerHTTPHandlers(t *testing.T) {
	bundles := NewBundleManager()
	router := NewRouteCompiler()
	stream := NewStreamPipe(32 * 1024)
	handler := NewCompilerHandler(bundles, router, stream)

	// 1. /api/compiler/compile
	compileBody := map[string]any{
		"script": `// Header
			export default {
				fetch() { return new Response("pong"); }
			};`,
		"minify": true,
	}
	payload, _ := json.Marshal(compileBody)
	req := httptest.NewRequest(http.MethodPost, "/api/compiler/compile", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var compileResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &compileResp)
	if compileResp["has_valid_export"] != true {
		t.Errorf("expected has_valid_export=true")
	}

	// 2. /api/compiler/workers (POST)
	deployBody := map[string]any{
		"tenant_id": "tenant-test",
		"name":      "api-worker",
		"script":    "export default { fetch() {} };",
		"minify":    true,
	}
	dPayload, _ := json.Marshal(deployBody)
	dReq := httptest.NewRequest(http.MethodPost, "/api/compiler/workers", bytes.NewReader(dPayload))
	dRec := httptest.NewRecorder()
	handler.ServeHTTP(dRec, dReq)

	if dRec.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201, got %d: %s", dRec.Code, dRec.Body.String())
	}
	var worker WorkerBundle
	_ = json.Unmarshal(dRec.Body.Bytes(), &worker)
	if worker.Name != "api-worker" {
		t.Errorf("unexpected worker name: %s", worker.Name)
	}

	// 3. /api/compiler/workers (GET)
	gReq := httptest.NewRequest(http.MethodGet, "/api/compiler/workers", nil)
	gRec := httptest.NewRecorder()
	handler.ServeHTTP(gRec, gReq)
	if gRec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", gRec.Code)
	}

	// 4. /api/compiler/routes (POST)
	routeBody := map[string]any{
		"pattern":   "api.domain.com/v1/*",
		"zone_id":   "zone-1",
		"tenant_id": "tenant-test",
		"worker_id": worker.ID,
	}
	rPayload, _ := json.Marshal(routeBody)
	rReq := httptest.NewRequest(http.MethodPost, "/api/compiler/routes", bytes.NewReader(rPayload))
	rRec := httptest.NewRecorder()
	handler.ServeHTTP(rRec, rReq)
	if rRec.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201, got %d: %s", rRec.Code, rRec.Body.String())
	}

	// 5. /api/compiler/routes/match (POST)
	matchBody := map[string]any{
		"host": "api.domain.com",
		"path": "/v1/users",
	}
	mPayload, _ := json.Marshal(matchBody)
	mReq := httptest.NewRequest(http.MethodPost, "/api/compiler/routes/match", bytes.NewReader(mPayload))
	mRec := httptest.NewRecorder()
	handler.ServeHTTP(mRec, mReq)
	if mRec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", mRec.Code)
	}
	var matchRes map[string]any
	_ = json.Unmarshal(mRec.Body.Bytes(), &matchRes)
	if matchRes["matched"] != true {
		t.Errorf("expected matched=true")
	}

	// 6. /api/compiler/stream-test (GET)
	sReq := httptest.NewRequest(http.MethodGet, "/api/compiler/stream-test?size_mb=1", nil)
	sRec := httptest.NewRecorder()
	handler.ServeHTTP(sRec, sReq)
	if sRec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", sRec.Code)
	}
	if sRec.Body.Len() < 1000000 {
		t.Errorf("expected ~1MB streamed data, got %d bytes", sRec.Body.Len())
	}
}

func TestRouteCompilerConcurrency(t *testing.T) {
	rc := NewRouteCompiler()
	for i := 0; i < 20; i++ {
		_, _ = rc.RegisterRoute(fmt.Sprintf("sub%d.domain.com/*", i), "zone-1", "tenant-1", fmt.Sprintf("worker-%d", i))
	}

	var wg sync.WaitGroup
	workers := 10
	iterations := 100

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				host := fmt.Sprintf("sub%d.domain.com", i%20)
				match, found := rc.Match(host, "/api/data")
				if !found || match == nil {
					t.Errorf("concurrent match failed for %s", host)
				}
			}
		}(w)
	}

	wg.Wait()
}
