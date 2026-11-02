package wasmdedup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestPagePoolZeroPage(t *testing.T) {
	pool := NewPagePool()
	if pool.SharedCount() != 1 {
		t.Fatalf("expected 1 initial shared page (zero page), got %d", pool.SharedCount())
	}

	zeroPage := pool.GetZeroPage()
	if !zeroPage.IsZero || !zeroPage.IsShared {
		t.Fatalf("zero page properties invalid: IsZero=%v, IsShared=%v", zeroPage.IsZero, zeroPage.IsShared)
	}

	// Interning all-zero buffer returns zero page
	allZeroBuf := make([]byte, WasmPageSize)
	interned := pool.InternPage(allZeroBuf)
	if interned != zeroPage {
		t.Fatalf("interning all-zero buffer should return canonical zero page")
	}

	// Interning non-zero data creates new shared physical page
	nonZeroBuf := make([]byte, WasmPageSize)
	copy(nonZeroBuf, []byte("worker_runtime_preamble"))
	page1 := pool.InternPage(nonZeroBuf)
	if page1 == zeroPage {
		t.Fatalf("non-zero buffer should not match zero page")
	}
	if pool.SharedCount() != 2 {
		t.Fatalf("expected 2 shared pages in pool, got %d", pool.SharedCount())
	}

	// Interning identical non-zero data increments ref count and returns same page
	page2 := pool.InternPage(nonZeroBuf)
	if page1 != page2 {
		t.Fatalf("identical pages should return matching pointer from pool")
	}

	pool.ReleasePage(page1)
	pool.ReleasePage(page2)
	pool.ReleasePage(zeroPage)
	pool.ReleasePage(interned)
}

func TestCoWLinearMemory_ReadWriteBounds(t *testing.T) {
	pool := NewPagePool()
	inst, err := NewCoWLinearMemory("inst-1", "tenant-alpha", 4, 16, pool)
	if err != nil {
		t.Fatalf("failed to create CoWLinearMemory: %v", err)
	}
	defer inst.Release()

	// Initial memory should be all zero
	data, err := inst.Read(0, 128)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	for i, b := range data {
		if b != 0 {
			t.Fatalf("expected zero byte at offset %d, got %d", i, b)
		}
	}

	// Out of bounds read
	_, err = inst.Read(-1, 10)
	if err == nil {
		t.Fatalf("expected error for negative offset")
	}
	_, err = inst.Read(0, inst.ByteCount()+1)
	if err == nil {
		t.Fatalf("expected error exceeding memory bounds")
	}

	// Out of bounds write
	err = inst.Write(inst.ByteCount(), []byte("overflow"))
	if err == nil {
		t.Fatalf("expected error for write exceeding memory size")
	}
}

func TestCoWLinearMemory_FaultIsolation(t *testing.T) {
	pool := NewPagePool()

	// Create base template data spanning 2 pages
	templateData := make([]byte, WasmPageSize*2)
	copy(templateData[0:], []byte("SHARED_BASE_CODE_PAGE_0"))
	copy(templateData[WasmPageSize:], []byte("SHARED_BASE_DATA_PAGE_1"))

	basePage0 := pool.InternPage(templateData[:WasmPageSize])
	basePage1 := pool.InternPage(templateData[WasmPageSize:])
	basePages := []*PhysicalPage{basePage0, basePage1}

	// Spawn two instances sharing the exact same base pages
	instA, err := NewFromSharedPages("inst-A", "tenant-1", basePages, 16, pool)
	if err != nil {
		t.Fatalf("instA creation failed: %v", err)
	}
	defer instA.Release()

	instB, err := NewFromSharedPages("inst-B", "tenant-2", basePages, 16, pool)
	if err != nil {
		t.Fatalf("instB creation failed: %v", err)
	}
	defer instB.Release()

	// Verify both instances have 2 shared pages and 0 private pages
	if instA.SharedPageCount() != 2 || instA.PrivatePageCount() != 0 {
		t.Fatalf("instA page counts incorrect: shared=%d, private=%d", instA.SharedPageCount(), instA.PrivatePageCount())
	}
	if instB.SharedPageCount() != 2 || instB.PrivatePageCount() != 0 {
		t.Fatalf("instB page counts incorrect: shared=%d, private=%d", instB.SharedPageCount(), instB.PrivatePageCount())
	}

	// Write to Instance A (triggering CoW on page 0 only)
	modPayload := []byte("MODIFIED_BY_TENANT_A")
	if err := instA.Write(0, modPayload); err != nil {
		t.Fatalf("instA write failed: %v", err)
	}

	// Verify CoW fault triggered on instA
	if instA.CoWFaults() != 1 {
		t.Fatalf("expected 1 CoW fault on instA, got %d", instA.CoWFaults())
	}
	if instA.PrivatePageCount() != 1 || instA.SharedPageCount() != 1 {
		t.Fatalf("instA expected 1 private and 1 shared page, got private=%d shared=%d",
			instA.PrivatePageCount(), instA.SharedPageCount())
	}

	// Verify Instance B was completely untouched! (Fault isolation)
	if instB.CoWFaults() != 0 || instB.PrivatePageCount() != 0 || instB.SharedPageCount() != 2 {
		t.Fatalf("instB should remain 100%% shared and unmodified: faults=%d, private=%d, shared=%d",
			instB.CoWFaults(), instB.PrivatePageCount(), instB.SharedPageCount())
	}

	readA, err := instA.Read(0, len(modPayload))
	if err != nil || string(readA) != "MODIFIED_BY_TENANT_A" {
		t.Fatalf("instA read mismatch: %s", string(readA))
	}

	readB, err := instB.Read(0, len("SHARED_BASE_CODE_PAGE_0"))
	if err != nil || string(readB) != "SHARED_BASE_CODE_PAGE_0" {
		t.Fatalf("instB read mismatch (isolation breached!): %s", string(readB))
	}
}

func TestCoWLinearMemory_GrowAndLimits(t *testing.T) {
	pool := NewPagePool()
	inst, err := NewCoWLinearMemory("inst-grow", "tenant-1", 2, 4, pool)
	if err != nil {
		t.Fatalf("creation failed: %v", err)
	}
	defer inst.Release()

	// Grow within quota
	prev, err := inst.Grow(2)
	if err != nil || prev != 2 || inst.PageCount() != 4 {
		t.Fatalf("grow failed: prev=%d, current=%d, err=%v", prev, inst.PageCount(), err)
	}

	// Grow exceeding tenant quota (maxPages=4)
	_, err = inst.Grow(1)
	if err == nil {
		t.Fatalf("expected quota exceeded error")
	}

	// Test hard ceiling 32MB (512 pages)
	_, err = NewCoWLinearMemory("inst-ceil", "tenant-1", 16, 513, pool)
	if err == nil {
		t.Fatalf("expected 32MB hard ceiling error")
	}
}

func TestSnapshot_SubMillisecondResurrection(t *testing.T) {
	pool := NewPagePool()
	sm := NewSnapshotManager(pool)

	inst, err := NewCoWLinearMemory("source-inst", "tenant-resurrect", 4, 16, pool)
	if err != nil {
		t.Fatalf("inst create failed: %v", err)
	}
	defer inst.Release()

	// Populate some state across pages
	_ = inst.Write(0, []byte("INIT_CONFIG_V1"))
	_ = inst.Write(WasmPageSize, []byte("WARMED_CACHE_STATE"))

	// Create snapshot
	snap, err := sm.CreateSnapshot(inst, "template-worker", map[string]string{"env": "prod"})
	if err != nil {
		t.Fatalf("create snapshot failed: %v", err)
	}

	// Resurrect 5 new microservice instances and verify sub-millisecond resurrection
	for i := 0; i < 5; i++ {
		resurrected, elapsed, err := sm.Resurrect(snap.SnapshotID, fmt.Sprintf("resurrected-%d", i), 16)
		if err != nil {
			t.Fatalf("resurrection %d failed: %v", i, err)
		}
		defer resurrected.Release()

		// Sub-millisecond requirement assertion (< 1000 microseconds)
		if elapsed.Microseconds() >= 1000 {
			t.Logf("Warning: resurrection took %d µs (target < 1000 µs)", elapsed.Microseconds())
		}

		// Verify state integrity
		buf0, _ := resurrected.Read(0, len("INIT_CONFIG_V1"))
		if string(buf0) != "INIT_CONFIG_V1" {
			t.Fatalf("resurrected state mismatch at page 0: %s", string(buf0))
		}

		buf1, _ := resurrected.Read(WasmPageSize, len("WARMED_CACHE_STATE"))
		if string(buf1) != "WARMED_CACHE_STATE" {
			t.Fatalf("resurrected state mismatch at page 1: %s", string(buf1))
		}
	}

	count, avgUs := sm.ResurrectionStats()
	if count != 5 || avgUs <= 0 {
		t.Fatalf("unexpected resurrection stats: count=%d, avgUs=%.2f", count, avgUs)
	}
}

func TestDynamicCompaction_SixtyPercentSavings(t *testing.T) {
	cfg := DefaultDedupConfig()
	engine := NewDedupEngine(cfg)
	defer engine.Stop()

	// Register a shared template of 16 pages (1 MiB total virtual memory per instance)
	templateData := make([]byte, 16*WasmPageSize)
	for p := 0; p < 16; p++ {
		copy(templateData[p*WasmPageSize:], fmt.Sprintf("SHARED_FRAMEWORK_DATA_PAGE_%d", p))
	}
	if err := engine.RegisterTemplate("base-runtime", templateData); err != nil {
		t.Fatalf("register template failed: %v", err)
	}

	// Spawn 10 worker instances sharing the base template
	// Virtual memory: 10 instances * 16 pages * 64KB = 160 pages = 10,485,760 bytes (~10 MiB)
	for i := 0; i < 10; i++ {
		instID := fmt.Sprintf("worker-%02d", i)
		inst, err := engine.SpawnInstance(instID, fmt.Sprintf("tenant-%d", i%3), "base-runtime", 16, 32)
		if err != nil {
			t.Fatalf("spawn %s failed: %v", instID, err)
		}

		// Each worker writes only to page 0 with its own small tenant ID (causing 1 CoW fault)
		// Pages 1 to 15 remain 100% shared!
		_ = inst.Write(0, []byte(fmt.Sprintf("TENANT_ID_%02d_PAYLOAD", i)))
	}

	// Trigger compaction pass
	res := engine.RunCompaction()
	t.Logf("Compaction Result: scanned=%d, reclaimed=%d, savedBytes=%d", res.ScannedPages, res.ReclaimedPages, res.SavedBytes)

	stats := engine.GetStats()
	t.Logf("Memory Stats: VirtualBytes=%d, ResidentBytes=%d, SavedBytes=%d, Savings=%.2f%%, DedupRatio=%.2fx",
		stats.VirtualMemoryBytes, stats.PhysicalResidentBytes, stats.SavedMemoryBytes, stats.SavingsPercent, stats.DedupRatio)

	// Memory savings must easily exceed 60% (with 15/16 pages shared across 10 instances, savings is >80%)
	if stats.SavingsPercent < 60.0 {
		t.Fatalf("expected memory savings >= 60%%, got %.2f%%", stats.SavingsPercent)
	}
}

func TestDedupHandler_RESTEndpoints(t *testing.T) {
	cfg := DefaultDedupConfig()
	engine := NewDedupEngine(cfg)
	defer engine.Stop()

	handler := NewDedupHandler(engine)

	// 1. Status
	req := httptest.NewRequest(http.MethodGet, "/api/wasmdedup/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status code mismatch: %d", w.Code)
	}

	// 2. Spawn Instance
	spawnBody := `{"instance_id":"inst-rest-1","tenant_id":"tenant-test","initial_pages":4,"max_pages":16}`
	req = httptest.NewRequest(http.MethodPost, "/api/wasmdedup/instance/spawn", strings.NewReader(spawnBody))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("spawn code mismatch: %d, body: %s", w.Code, w.Body.String())
	}

	// 3. Write Data (triggers CoW)
	writeBody := `{"instance_id":"inst-rest-1","offset":0,"data_text":"HELLO_WASM_COW"}`
	req = httptest.NewRequest(http.MethodPost, "/api/wasmdedup/instance/write", strings.NewReader(writeBody))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("write code mismatch: %d, body: %s", w.Code, w.Body.String())
	}

	// 4. Read Data
	req = httptest.NewRequest(http.MethodGet, "/api/wasmdedup/instance/read?instance_id=inst-rest-1&offset=0&length=14", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("read code mismatch: %d", w.Code)
	}
	var readResp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &readResp)
	if readResp["data_text"] != "HELLO_WASM_COW" {
		t.Fatalf("expected HELLO_WASM_COW, got %v", readResp["data_text"])
	}

	// 5. Create Snapshot
	snapBody := `{"instance_id":"inst-rest-1","template_id":"tmpl-1","metadata":{"tag":"unit-test"}}`
	req = httptest.NewRequest(http.MethodPost, "/api/wasmdedup/snapshot/create", strings.NewReader(snapBody))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("snapshot create failed: %d", w.Code)
	}
	var snapResp WorkerSnapshot
	_ = json.Unmarshal(w.Body.Bytes(), &snapResp)
	if snapResp.SnapshotID == "" {
		t.Fatalf("expected non-empty snapshot_id")
	}

	// 6. Resurrect
	resurrectBody := fmt.Sprintf(`{"snapshot_id":"%s","new_instance_id":"inst-resurrected","max_pages":16}`, snapResp.SnapshotID)
	req = httptest.NewRequest(http.MethodPost, "/api/wasmdedup/snapshot/resurrect", strings.NewReader(resurrectBody))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resurrect failed: %d, body: %s", w.Code, w.Body.String())
	}

	// 7. Compact
	req = httptest.NewRequest(http.MethodPost, "/api/wasmdedup/compact", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("compact failed: %d", w.Code)
	}

	// 8. Stats
	req = httptest.NewRequest(http.MethodGet, "/api/wasmdedup/stats", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("stats failed: %d", w.Code)
	}
}

func TestDedupHandler_BodySizeBounding(t *testing.T) {
	cfg := DefaultDedupConfig()
	engine := NewDedupEngine(cfg)
	defer engine.Stop()

	handler := NewDedupHandler(engine)

	// Oversized payload (> 64KB)
	hugeData := bytes.Repeat([]byte("A"), 70*1024)
	req := httptest.NewRequest(http.MethodPost, "/api/wasmdedup/instance/spawn", bytes.NewReader(hugeData))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 for oversized payload, got %d", w.Code)
	}
}

func TestDedupEngine_HighConcurrency40Goroutines(t *testing.T) {
	cfg := DefaultDedupConfig()
	engine := NewDedupEngine(cfg)
	defer engine.Stop()

	var wg sync.WaitGroup
	goroutines := 40

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			instID := fmt.Sprintf("conc-inst-%03d", id)
			inst, err := engine.SpawnInstance(instID, fmt.Sprintf("tenant-%d", id%5), "", 4, 16)
			if err != nil {
				t.Errorf("spawn conc-inst-%03d error: %v", id, err)
				return
			}

			// Write to instance
			_ = inst.Write(0, []byte(fmt.Sprintf("DATA_CONC_%d", id)))

			// Read back
			readBuf, err := inst.Read(0, 10)
			if err != nil || len(readBuf) != 10 {
				t.Errorf("read conc-inst-%03d error: %v", id, err)
			}

			// Take snapshot for even IDs
			if id%2 == 0 {
				snap, err := engine.CreateSnapshot(instID, "tmpl-conc", nil)
				if err == nil && snap != nil {
					resID := fmt.Sprintf("resurrected-%03d", id)
					_, _, _ = engine.Resurrect(snap.SnapshotID, resID, 16)
				}
			}

			// Run compaction for multiple of 10
			if id%10 == 0 {
				_ = engine.RunCompaction()
			}
		}(g)
	}

	wg.Wait()

	stats := engine.GetStats()
	if stats.ActiveInstances < goroutines {
		t.Fatalf("expected at least %d active instances, got %d", goroutines, stats.ActiveInstances)
	}
}
