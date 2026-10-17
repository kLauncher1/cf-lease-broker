package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLinearMemoryAllocationAndBounds(t *testing.T) {
	mem, err := NewLinearMemory(16, 256)
	if err != nil {
		t.Fatalf("unexpected NewLinearMemory error: %v", err)
	}

	if mem.Pages() != 16 {
		t.Errorf("expected 16 pages, got %d", mem.Pages())
	}
	if mem.Bytes() != 16*WasmPageSize {
		t.Errorf("expected %d bytes, got %d", 16*WasmPageSize, mem.Bytes())
	}

	// Write and Read within bounds
	testPayload := []byte("hello edge wasm sandbox")
	if err := mem.Write(1024, testPayload); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	readBack, err := mem.Read(1024, len(testPayload))
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if !bytes.Equal(readBack, testPayload) {
		t.Errorf("read bytes mismatch: got %s, want %s", string(readBack), string(testPayload))
	}

	// Out of bounds tests
	if err := mem.Write(-1, testPayload); err == nil {
		t.Errorf("expected error for negative write offset")
	}
	if err := mem.Write(mem.Bytes()-5, testPayload); err == nil {
		t.Errorf("expected error for write exceeding memory size")
	}
	if _, err := mem.Read(-1, 10); err == nil {
		t.Errorf("expected error for negative read offset")
	}
	if _, err := mem.Read(mem.Bytes()-5, 10); err == nil {
		t.Errorf("expected error for read exceeding memory size")
	}

	// Invalid initialization parameters
	if _, err := NewLinearMemory(-1, 10); err == nil {
		t.Errorf("expected error for negative initial pages")
	}
	if _, err := NewLinearMemory(10, 5); err == nil {
		t.Errorf("expected error when maxPages < initialPages")
	}
	if _, err := NewLinearMemory(16, 600); err == nil {
		t.Errorf("expected error when maxPages > HardCeilingMemoryPages (512)")
	}
}

func TestLinearMemoryGrowAndHardCeiling(t *testing.T) {
	mem, err := NewLinearMemory(2, 8)
	if err != nil {
		t.Fatalf("NewLinearMemory failed: %v", err)
	}

	prev, err := mem.Grow(3)
	if err != nil {
		t.Fatalf("Grow(3) failed: %v", err)
	}
	if prev != 2 {
		t.Errorf("expected previous pages 2, got %d", prev)
	}
	if mem.Pages() != 5 {
		t.Errorf("expected current pages 5, got %d", mem.Pages())
	}
	if mem.PeakPages() != 5 {
		t.Errorf("expected peak pages 5, got %d", mem.PeakPages())
	}

	// Exceed tenant quota (maxPages = 8)
	_, err = mem.Grow(4)
	if err == nil {
		t.Fatalf("expected error when growing past maxPages")
	}
	if mem.OOMCount() != 1 {
		t.Errorf("expected OOM count 1, got %d", mem.OOMCount())
	}

	// Hard ceiling enforcement (512 pages)
	memLarge, _ := NewLinearMemory(100, HardCeilingMemoryPages)
	_, err = memLarge.Grow(HardCeilingMemoryPages + 1)
	if err == nil {
		t.Fatalf("expected error when exceeding 32MB hard ceiling")
	}
}

func TestLinearMemoryZeroization(t *testing.T) {
	mem, _ := NewLinearMemory(4, 16)
	data := []byte("top_secret_tenant_token_material")
	_ = mem.Write(0, data)
	_ = mem.Write(5000, data)

	mem.Zeroize()

	read1, _ := mem.Read(0, len(data))
	read2, _ := mem.Read(5000, len(data))
	expectedZeros := make([]byte, len(data))

	if !bytes.Equal(read1, expectedZeros) || !bytes.Equal(read2, expectedZeros) {
		t.Errorf("Zeroize failed: residual non-zero memory detected")
	}
}

func TestGasMeterDeterministicAccounting(t *testing.T) {
	meter := NewGasMeter(100)

	if err := meter.Consume(GasBasicOp * 10); err != nil { // 10
		t.Fatalf("Consume failed: %v", err)
	}
	if err := meter.Consume(GasArithmetic * 5); err != nil { // 10
		t.Fatalf("Consume failed: %v", err)
	}
	if err := meter.Consume(GasMemoryAccess * 3); err != nil { // 30
		t.Fatalf("Consume failed: %v", err)
	}

	if meter.Used() != 50 {
		t.Errorf("expected 50 used, got %d", meter.Used())
	}
	if meter.Remaining() != 50 {
		t.Errorf("expected 50 remaining, got %d", meter.Remaining())
	}

	// Consume remaining 50
	if err := meter.Consume(50); err != nil {
		t.Fatalf("Consume remaining 50 failed: %v", err)
	}
	if meter.Remaining() != 0 {
		t.Errorf("expected 0 remaining, got %d", meter.Remaining())
	}

	// Exceeding budget
	if err := meter.Consume(1); err == nil {
		t.Errorf("expected ErrGasExhausted when exceeding budget")
	}
	if !meter.IsExhausted() {
		t.Errorf("expected meter to be exhausted")
	}

	// Reset
	meter.Reset(200)
	if meter.Remaining() != 200 || meter.Used() != 0 || meter.IsExhausted() {
		t.Errorf("Reset did not restore gas state")
	}
}

func TestGasMeterInfiniteLoopTrap(t *testing.T) {
	meter := NewGasMeter(50) // 10 loop steps max (5 gas each)
	var trapErr error
	for i := 0; i < 20; i++ {
		if err := meter.ConsumeLoopStep(); err != nil {
			trapErr = err
			break
		}
	}

	if trapErr == nil {
		t.Fatalf("expected loop to be trapped by gas exhaustion")
	}
	if meter.LoopSteps() != 10 {
		t.Errorf("expected 10 loop steps before exhaustion, got %d", meter.LoopSteps())
	}
}

func TestSandboxEngineComputePayload(t *testing.T) {
	engine := NewSandboxEngine(DefaultSandboxConfig())
	ctx := context.Background()

	res, err := engine.Execute(ctx, ExecutionRequest{
		TenantID:    "tenant-alpha",
		InstanceID:  "inst-compute",
		PayloadType: PayloadCompute,
		GasBudget:   10000,
	})
	if err != nil {
		t.Fatalf("Execute compute failed: %v", err)
	}

	if res.Status != StatusIdle {
		t.Errorf("expected StatusIdle after successful run, got %s", res.Status)
	}
	if len(res.Output) == 0 {
		t.Errorf("expected non-empty output")
	}
	if res.GasConsumed == 0 {
		t.Errorf("expected non-zero gas consumption")
	}

	stats := engine.GetStats()
	if stats.SuccessfulExecutions != 1 {
		t.Errorf("expected 1 successful execution, got %d", stats.SuccessfulExecutions)
	}
}

func TestSandboxEngineInfiniteLoopIsolation(t *testing.T) {
	engine := NewSandboxEngine(DefaultSandboxConfig())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := engine.Execute(ctx, ExecutionRequest{
		TenantID:    "tenant-loop",
		InstanceID:  "inst-infinite",
		PayloadType: PayloadInfiniteLoop,
		GasBudget:   250, // 50 loop steps
	})

	if err == nil {
		t.Fatalf("expected infinite loop to fail with gas exhaustion")
	}
	if res == nil || res.GasRemaining != 0 {
		t.Errorf("expected gas to be completely exhausted")
	}
	if !strings.Contains(err.Error(), "exhausted") {
		t.Errorf("expected exhausted error message, got %v", err)
	}
}

func TestSandboxEngineMemoryOOMInterception(t *testing.T) {
	engine := NewSandboxEngine(DefaultSandboxConfig())
	ctx := context.Background()

	res, err := engine.Execute(ctx, ExecutionRequest{
		TenantID:       "tenant-mem",
		InstanceID:     "inst-oom",
		PayloadType:    PayloadMemoryAlloc,
		RequestedPages: 600, // exceeds 512 hard ceiling
		GasBudget:      1000000,
	})

	if err == nil {
		t.Fatalf("expected OOM error for requesting 600 pages")
	}
	if res == nil {
		t.Fatalf("expected non-nil execution result")
	}

	stats := engine.GetStats()
	if stats.OOMInterceptions != 1 {
		t.Errorf("expected 1 OOM interception, got %d", stats.OOMInterceptions)
	}
}

func TestSandboxEnginePanicRecovery(t *testing.T) {
	engine := NewSandboxEngine(DefaultSandboxConfig())
	ctx := context.Background()

	res, err := engine.Execute(ctx, ExecutionRequest{
		TenantID:    "tenant-fault",
		InstanceID:  "inst-crash",
		PayloadType: PayloadPanicFault,
		GasBudget:   50000,
	})

	// Panic must be trapped gracefully without crashing the host process
	if err != ErrSandboxCrashed {
		t.Errorf("expected ErrSandboxCrashed, got %v", err)
	}
	if res == nil || res.Status != StatusCrashed {
		t.Fatalf("expected StatusCrashed, got %v", res)
	}
	if res.Crash == nil || !res.Crash.Recovered {
		t.Errorf("expected recovered crash report")
	}
	if !strings.Contains(res.Crash.Reason, "SIGSEGV") {
		t.Errorf("expected SIGSEGV in crash reason, got %s", res.Crash.Reason)
	}

	stats := engine.GetStats()
	if stats.PanicRecoveries != 1 {
		t.Errorf("expected 1 panic recovery, got %d", stats.PanicRecoveries)
	}

	crashes := engine.ListCrashes(10)
	if len(crashes) != 1 {
		t.Errorf("expected 1 crash in journal, got %d", len(crashes))
	}
}

func TestTenantQuarantineThreshold(t *testing.T) {
	cfg := DefaultSandboxConfig()
	cfg.QuarantineThreshold = 3
	engine := NewSandboxEngine(cfg)
	ctx := context.Background()

	tenantID := "faulty-tenant"
	_ = engine.SetTenantPolicy(TenantSandboxPolicy{
		TenantID:          tenantID,
		MaxCrashThreshold: 3,
		MaxMemoryPages:    64,
		GasLimit:          50000,
	})

	// Trigger 3 crashes
	for i := 0; i < 3; i++ {
		_, _ = engine.Execute(ctx, ExecutionRequest{
			TenantID:    tenantID,
			InstanceID:  "inst-0",
			PayloadType: PayloadPanicFault,
		})
	}

	pol, found := engine.GetTenantPolicy(tenantID)
	if !found || !pol.Quarantined {
		t.Fatalf("expected tenant to be quarantined after 3 crashes")
	}

	// 4th execution should be blocked by quarantine
	res, err := engine.Execute(ctx, ExecutionRequest{
		TenantID:    tenantID,
		InstanceID:  "inst-0",
		PayloadType: PayloadCompute,
	})
	if err != ErrSandboxQuarantined {
		t.Errorf("expected ErrSandboxQuarantined, got %v", err)
	}
	if res.Status != StatusQuarantined {
		t.Errorf("expected StatusQuarantined, got %s", res.Status)
	}

	// Reset instance & quarantine
	if err := engine.ResetInstance(tenantID, "inst-0"); err != nil {
		t.Fatalf("ResetInstance failed: %v", err)
	}

	// Should execute normally after reset
	res2, err := engine.Execute(ctx, ExecutionRequest{
		TenantID:    tenantID,
		InstanceID:  "inst-0",
		PayloadType: PayloadCompute,
	})
	if err != nil {
		t.Fatalf("Execute after reset failed: %v", err)
	}
	if res2.Status != StatusIdle {
		t.Errorf("expected StatusIdle, got %s", res2.Status)
	}
}

func TestSandboxRESTHandlerEndpoints(t *testing.T) {
	engine := NewSandboxEngine(DefaultSandboxConfig())
	handler := NewSandboxHandler(engine)

	// 1. GET /api/sandbox/status
	req := httptest.NewRequest(http.MethodGet, "/api/sandbox/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// 2. POST /api/sandbox/tenants
	tenantPayload := `{"tenant_id":"api-tenant","max_memory_pages":128,"gas_limit":500000}`
	req = httptest.NewRequest(http.MethodPost, "/api/sandbox/tenants", strings.NewReader(tenantPayload))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201 for tenant registration, got %d", w.Code)
	}

	// 3. GET /api/sandbox/tenants
	req = httptest.NewRequest(http.MethodGet, "/api/sandbox/tenants?tenant_id=api-tenant", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	var statusMap map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&statusMap); err != nil {
		t.Errorf("failed to parse status json: %v", err)
	}

	// 4. POST /api/sandbox/execute
	execPayload := `{"tenant_id":"api-tenant","instance_id":"inst-1","payload_type":"compute"}`
	req = httptest.NewRequest(http.MethodPost, "/api/sandbox/execute", strings.NewReader(execPayload))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for execute compute, got %d", w.Code)
	}

	// 5. GET /api/sandbox/crashes
	req = httptest.NewRequest(http.MethodGet, "/api/sandbox/crashes", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for crashes, got %d", w.Code)
	}

	// 6. POST /api/sandbox/reset
	resetPayload := `{"tenant_id":"api-tenant","instance_id":"inst-1"}`
	req = httptest.NewRequest(http.MethodPost, "/api/sandbox/reset", strings.NewReader(resetPayload))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for reset, got %d", w.Code)
	}
}

func TestSandboxRequestBodyBounding(t *testing.T) {
	engine := NewSandboxEngine(DefaultSandboxConfig())
	handler := NewSandboxHandler(engine)

	// Send oversized payload exceeding 64KB
	largeData := strings.Repeat("A", 70*1024)
	req := httptest.NewRequest(http.MethodPost, "/api/sandbox/execute", strings.NewReader(largeData))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Handler with LimitReader will truncate or fail JSON parse gracefully with 400
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for oversized malformed json, got %d", w.Code)
	}
}

func TestSandboxHighConcurrency(t *testing.T) {
	engine := NewSandboxEngine(DefaultSandboxConfig())
	ctx := context.Background()
	concurrency := 40
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		workerID := i
		go func() {
			defer wg.Done()
			tenantID := "tenant-concurrent"
			instID := "inst-" + string(rune('a'+workerID%10))

			pType := PayloadCompute
			if workerID%5 == 0 {
				pType = PayloadCustom
			}

			_, _ = engine.Execute(ctx, ExecutionRequest{
				TenantID:    tenantID,
				InstanceID:  instID,
				PayloadType: pType,
				Payload:     []byte("concurrent_test_payload"),
				GasBudget:   100000,
			})
		}()
	}

	wg.Wait()

	stats := engine.GetStats()
	if stats.TotalExecutions != int64(concurrency) {
		t.Errorf("expected %d total executions, got %d", concurrency, stats.TotalExecutions)
	}
}
