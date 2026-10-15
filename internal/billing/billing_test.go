package billing

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kLauncher1/cf-lease-broker/internal/broker"
)

func TestBillingRates_MicroCentCalculations(t *testing.T) {
	rates := DefaultCloudflareRates()
	meter := NewBillingMeter(rates)

	// Paid account
	meter.RegisterAccount("paid-1", TierPaidStandard, 10000000)

	// 1,000,000 worker requests @ $0.30/1M = 300,000 micro-cents ($0.30)
	entry := meter.RecordUsage(UsageRecord{
		TenantID:     "t-corp",
		AccountID:    "paid-1",
		Service:      "workers",
		ResourceType: "requests",
		Quantity:     1000000,
		Timestamp:    time.Now().UTC(),
	})

	if entry.TotalCostMicroCents != 300000 {
		t.Fatalf("expected 300000 micro-cents, got %d", entry.TotalCostMicroCents)
	}

	// 50,000,000 CPU ms @ $0.02/1M ms = 1,000,000 micro-cents ($1.00)
	entryCpu := meter.RecordUsage(UsageRecord{
		TenantID:     "t-corp",
		AccountID:    "paid-1",
		Service:      "workers",
		ResourceType: "cpu_ms",
		Quantity:     50000000,
		Timestamp:    time.Now().UTC(),
	})

	if entryCpu.TotalCostMicroCents != 1000000 {
		t.Fatalf("expected 1000000 micro-cents for CPU, got %d", entryCpu.TotalCostMicroCents)
	}

	// 2,000,000 KV reads @ $0.50/1M = 1,000,000 micro-cents ($1.00)
	entryKV := meter.RecordUsage(UsageRecord{
		TenantID:     "t-corp",
		AccountID:    "paid-1",
		Service:      "kv",
		ResourceType: "kv_read",
		Quantity:     2000000,
		Timestamp:    time.Now().UTC(),
	})

	if entryKV.TotalCostMicroCents != 1000000 {
		t.Fatalf("expected 1000000 micro-cents for KV reads, got %d", entryKV.TotalCostMicroCents)
	}
}

func TestUsageMeter_FreeTierAllowance(t *testing.T) {
	rates := DefaultCloudflareRates()
	meter := NewBillingMeter(rates)

	meter.RegisterAccount("free-1", TierFree, 0)

	// 40,000 requests on Free tier (under 100,000 free quota)
	entry1 := meter.RecordUsage(UsageRecord{
		TenantID:     "t-dev",
		AccountID:    "free-1",
		Service:      "workers",
		ResourceType: "requests",
		Quantity:     40000,
	})

	if entry1.TotalCostMicroCents != 0 {
		t.Fatalf("expected 0 cost for free tier request, got %d", entry1.TotalCostMicroCents)
	}

	acc, _ := meter.GetAccountCost("free-1")
	if acc.FreeWorkersRemaining != 60000 {
		t.Fatalf("expected 60000 free workers remaining, got %d", acc.FreeWorkersRemaining)
	}
	if acc.EstimatedSavingsMicroCents != (40000*300000)/1000000 {
		t.Fatalf("expected savings recorded for free requests, got %d", acc.EstimatedSavingsMicroCents)
	}

	// 80,000 requests (60,000 free remaining + 20,000 billable overage)
	entry2 := meter.RecordUsage(UsageRecord{
		TenantID:     "t-dev",
		AccountID:    "free-1",
		Service:      "workers",
		ResourceType: "requests",
		Quantity:     80000,
	})

	expectedOverageCost := (20000 * rates.WorkersPerMillionRequestsMicroCents) / 1000000
	if entry2.TotalCostMicroCents != expectedOverageCost {
		t.Fatalf("expected overage cost %d, got %d", expectedOverageCost, entry2.TotalCostMicroCents)
	}

	accAfter, _ := meter.GetAccountCost("free-1")
	if accAfter.FreeWorkersRemaining != 0 {
		t.Fatalf("expected 0 free workers remaining, got %d", accAfter.FreeWorkersRemaining)
	}
}

func TestUsageMeter_AggregatesAndProxyTelemetry(t *testing.T) {
	meter := NewBillingMeter(DefaultCloudflareRates())
	meter.RegisterAccount("acc-alpha", TierPaidStandard, 5000000)

	meter.RecordProxyRequest("tenant-a", "acc-alpha", "workers", 15, 1024, 2048)
	meter.RecordProxyRequest("tenant-a", "acc-alpha", "workers", 25, 512, 1024)

	summary, ok := meter.GetTenantSummary("tenant-a")
	if !ok {
		t.Fatalf("expected tenant-a summary to exist")
	}

	if summary.TotalRequests != 2 {
		t.Fatalf("expected 2 total requests, got %d", summary.TotalRequests)
	}
	if summary.TotalCpuMs != 40 {
		t.Fatalf("expected 40 total CPU ms, got %d", summary.TotalCpuMs)
	}
	if summary.TotalBytes != 4608 {
		t.Fatalf("expected 4608 total bytes, got %d", summary.TotalBytes)
	}

	cost, _, reqs, bytesCount, cpuMs := meter.GetClusterTotals()
	if reqs != 2 || cpuMs != 40 || bytesCount != 4608 || cost < 0 {
		t.Fatalf("unexpected cluster totals: cost=%d, reqs=%d, bytes=%d, cpuMs=%d", cost, reqs, bytesCount, cpuMs)
	}
}

func TestCostOptimizer_WorkloadMigration(t *testing.T) {
	meter := NewBillingMeter(DefaultCloudflareRates())
	pool := broker.NewPoolManager()

	// Pre-seed accounts in pool
	meter.RegisterAccount("cf-tenant-alpha", TierPaidStandard, 1000000)
	meter.RegisterAccount("cf-tenant-bravo", TierFree, 0)
	meter.RegisterAccount("cf-tenant-charlie", TierFree, 0)

	// Simulate heavy load on alpha (90,000 / 100,000 = 90% usage)
	pool.UpsertAccount(broker.AccountStatus{
		ID:          "cf-tenant-alpha",
		Alias:       "Overloaded Account",
		Plan:        "Paid Standard",
		WorkerLimit: 100000,
		WorkerUsed:  90000,
		IsHealthy:   true,
	})

	rule := MigrationRule{
		OverageThresholdPercent: 80.0,
		PreferFreeTierStandby:   true,
		AutoMigrateOnOverage:    true,
	}

	optimizer := NewCostOptimizer(meter, pool, rule)
	events := optimizer.EvaluateWorkloadMigration()

	if len(events) == 0 {
		t.Fatalf("expected migration events to be triggered for overloaded account")
	}

	mig := events[0]
	if mig.SourceAccountID != "cf-tenant-alpha" {
		t.Fatalf("expected source account cf-tenant-alpha, got %s", mig.SourceAccountID)
	}
	if mig.TargetAccountID != "cf-tenant-bravo" && mig.TargetAccountID != "cf-tenant-charlie" {
		t.Fatalf("expected target account to be a standby free account, got %s", mig.TargetAccountID)
	}
	if mig.Status != "completed" {
		t.Fatalf("expected completed status, got %s", mig.Status)
	}

	// Verify savings accumulation
	if optimizer.GetTotalSavings() <= 0 {
		t.Fatalf("expected total savings > 0, got %d", optimizer.GetTotalSavings())
	}

	// Verify manual migration trigger
	manualEvent, err := optimizer.TriggerManualMigration("cf-tenant-bravo", "cf-tenant-charlie", "t-test", "rebalance")
	if err != nil {
		t.Fatalf("unexpected error on manual migration: %v", err)
	}
	if manualEvent.SourceAccountID != "cf-tenant-bravo" || manualEvent.TargetAccountID != "cf-tenant-charlie" {
		t.Fatalf("unexpected manual event fields: %+v", manualEvent)
	}
}

func TestJournalExporter_CSVFormat(t *testing.T) {
	exporter := NewJournalExporter()
	entries := []CostEntry{
		{
			ID:                  "cost_01",
			Timestamp:           time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
			TenantID:            "tenant-alpha",
			AccountID:           "cf-acc-1",
			Service:             "workers",
			ResourceType:        "requests",
			Quantity:            5000,
			UnitRateMicroCents:  300000,
			TotalCostMicroCents: 1500,
			Currency:            "USD",
		},
		{
			ID:                  "cost_inj",
			Timestamp:           time.Date(2026, 10, 15, 12, 1, 0, 0, time.UTC),
			TenantID:            "=cmd|' /C calc'!A0",
			AccountID:           "+danger_acc",
			Service:             "@malicious_service",
			ResourceType:        "-flag_resource",
			Quantity:            100,
			UnitRateMicroCents:  300000,
			TotalCostMicroCents: 30,
			Currency:            "USD",
		},
	}

	csvBytes, err := exporter.ExportCSV(entries)
	if err != nil {
		t.Fatalf("export CSV failed: %v", err)
	}

	reader := csv.NewReader(bytes.NewReader(csvBytes))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to read exported CSV: %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("expected 3 CSV lines (header + 2 rows), got %d", len(records))
	}

	header := records[0]
	if header[0] != "id" || header[2] != "tenant_id" || header[9] != "cost_usd" {
		t.Fatalf("unexpected CSV header: %v", header)
	}

	row := records[1]
	if row[0] != "cost_01" || row[2] != "tenant-alpha" || row[8] != "1500" || row[9] != "0.001500" {
		t.Fatalf("unexpected CSV data row: %v", row)
	}

	// Verify CWE-1236 CSV Formula Injection defense (prepending single quote)
	rowInj := records[2]
	if rowInj[2] != "'=cmd|' /C calc'!A0" {
		t.Fatalf("expected tenant_id with leading quote defense, got: %s", rowInj[2])
	}
	if rowInj[3] != "'+danger_acc" {
		t.Fatalf("expected account_id with leading quote defense, got: %s", rowInj[3])
	}
	if rowInj[4] != "'@malicious_service" {
		t.Fatalf("expected service with leading quote defense, got: %s", rowInj[4])
	}
	if rowInj[5] != "'-flag_resource" {
		t.Fatalf("expected resource_type with leading quote defense, got: %s", rowInj[5])
	}
}

func TestJournalExporter_JSONLFormat(t *testing.T) {
	exporter := NewJournalExporter()
	entries := []CostEntry{
		{
			ID:                  "cost_10",
			Timestamp:           time.Now().UTC(),
			TenantID:            "tenant-beta",
			AccountID:           "cf-acc-2",
			Service:             "kv",
			ResourceType:        "kv_read",
			Quantity:            10000,
			TotalCostMicroCents: 5000,
			Currency:            "USD",
		},
		{
			ID:                  "cost_11",
			Timestamp:           time.Now().UTC(),
			TenantID:            "tenant-gamma",
			AccountID:           "cf-acc-3",
			Service:             "r2",
			ResourceType:        "r2_class_a",
			Quantity:            2000,
			TotalCostMicroCents: 9000,
			Currency:            "USD",
		},
	}

	jsonlBytes, err := exporter.ExportJSONL(entries)
	if err != nil {
		t.Fatalf("export JSONL failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(jsonlBytes)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d", len(lines))
	}

	var parsed1, parsed2 CostEntry
	if err := json.Unmarshal([]byte(lines[0]), &parsed1); err != nil {
		t.Fatalf("failed to parse first JSONL line: %v", err)
	}
	if parsed1.ID != "cost_10" || parsed1.TenantID != "tenant-beta" {
		t.Fatalf("unexpected entry 1: %+v", parsed1)
	}

	if err := json.Unmarshal([]byte(lines[1]), &parsed2); err != nil {
		t.Fatalf("failed to parse second JSONL line: %v", err)
	}
	if parsed2.ID != "cost_11" || parsed2.ResourceType != "r2_class_a" {
		t.Fatalf("unexpected entry 2: %+v", parsed2)
	}
}

func TestJournalExporter_OpenCostSpec(t *testing.T) {
	exporter := NewJournalExporter()
	tenants := map[string]*TenantSummary{
		"tenant-prod": {
			TenantID:            "tenant-prod",
			TotalCostMicroCents: 2500000, // $2.50
			ServiceBreakdown: map[string]int64{
				"workers": 1500000, // $1.50 cpuCost
				"r2":      1000000, // $1.00 storageCost
			},
		},
	}

	openCostBytes, err := exporter.ExportOpenCost(tenants, "2026-10-15T00:00:00Z/2026-10-15T23:59:59Z")
	if err != nil {
		t.Fatalf("export OpenCost failed: %v", err)
	}

	var report OpenCostReport
	if err := json.Unmarshal(openCostBytes, &report); err != nil {
		t.Fatalf("failed to unmarshal OpenCost JSON: %v", err)
	}

	if report.Version != "1.0" {
		t.Fatalf("expected version 1.0, got %s", report.Version)
	}
	if len(report.Data) != 1 {
		t.Fatalf("expected 1 allocation item, got %d", len(report.Data))
	}

	alloc := report.Data[0]
	if alloc.Name != "tenant-prod" || alloc.Properties.Tenant != "tenant-prod" {
		t.Fatalf("unexpected allocation properties: %+v", alloc)
	}
	if alloc.TotalCost != 2.50 || alloc.CpuCost != 1.50 || alloc.StorageCost != 1.00 {
		t.Fatalf("unexpected allocation costs: Total=%.2f, Cpu=%.2f, Storage=%.2f", alloc.TotalCost, alloc.CpuCost, alloc.StorageCost)
	}
}

func TestBillingHandler_HTTPEndpoints(t *testing.T) {
	meter := NewBillingMeter(DefaultCloudflareRates())
	pool := broker.NewPoolManager()
	optimizer := NewCostOptimizer(meter, pool, DefaultMigrationRule())
	handler := NewBillingHandler(meter, optimizer)

	// 1. Ingest via POST /api/billing/meter
	usagePayload := `{"tenant_id":"tenant-web","account_id":"cf-primary","service":"workers","resource_type":"requests","quantity":10000,"duration_ms":50}`
	req := httptest.NewRequest(http.MethodPost, "/api/billing/meter", strings.NewReader(usagePayload))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201 Created, got %d", rec.Code)
	}

	// 2. Query summary via GET /api/billing/summary
	reqSummary := httptest.NewRequest(http.MethodGet, "/api/billing/summary", nil)
	recSummary := httptest.NewRecorder()
	handler.ServeHTTP(recSummary, reqSummary)

	if recSummary.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", recSummary.Code)
	}
	var summaryResp map[string]interface{}
	if err := json.NewDecoder(recSummary.Body).Decode(&summaryResp); err != nil {
		t.Fatalf("failed to parse summary JSON: %v", err)
	}
	if summaryResp["total_cluster_requests"].(float64) != 10000 {
		t.Fatalf("expected 10000 cluster requests in summary, got %v", summaryResp["total_cluster_requests"])
	}

	// 3. Query ledger CSV export via GET /api/billing/ledger?format=csv
	reqCSV := httptest.NewRequest(http.MethodGet, "/api/billing/ledger?format=csv", nil)
	recCSV := httptest.NewRecorder()
	handler.ServeHTTP(recCSV, reqCSV)

	if recCSV.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK for CSV, got %d", recCSV.Code)
	}
	if !strings.Contains(recCSV.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("unexpected Content-Type for CSV: %s", recCSV.Header().Get("Content-Type"))
	}

	// 4. Query OpenCost export via GET /api/billing/ledger?format=opencost
	reqOpenCost := httptest.NewRequest(http.MethodGet, "/api/billing/ledger?format=opencost", nil)
	recOpenCost := httptest.NewRecorder()
	handler.ServeHTTP(recOpenCost, reqOpenCost)

	if recOpenCost.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK for OpenCost, got %d", recOpenCost.Code)
	}

	// 5. Trigger optimize via POST /api/billing/optimize
	reqOpt := httptest.NewRequest(http.MethodPost, "/api/billing/optimize", nil)
	recOpt := httptest.NewRecorder()
	handler.ServeHTTP(recOpt, reqOpt)

	if recOpt.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK for optimize, got %d", recOpt.Code)
	}

	// 6. Test rates endpoints
	reqRates := httptest.NewRequest(http.MethodGet, "/api/billing/rates", nil)
	recRates := httptest.NewRecorder()
	handler.ServeHTTP(recRates, reqRates)
	if recRates.Code != http.StatusOK {
		t.Fatalf("expected status 200 for rates, got %d", recRates.Code)
	}
}

func TestBillingHandler_SecurityAndLimits(t *testing.T) {
	meter := NewBillingMeter(DefaultCloudflareRates())
	pool := broker.NewPoolManager()
	optimizer := NewCostOptimizer(meter, pool, DefaultMigrationRule())
	handler := NewBillingHandler(meter, optimizer)

	// Oversized body test (exceeding 64KB)
	largeBody := strings.Repeat("A", 70*1024)
	req := httptest.NewRequest(http.MethodPost, "/api/billing/meter", strings.NewReader(largeBody))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for oversized payload, got %d", rec.Code)
	}

	// Malformed JSON test
	reqBad := httptest.NewRequest(http.MethodPost, "/api/billing/meter", strings.NewReader(`{malformed}`))
	recBad := httptest.NewRecorder()
	handler.ServeHTTP(recBad, reqBad)

	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for malformed payload, got %d", recBad.Code)
	}

	// Not found endpoint
	req404 := httptest.NewRequest(http.MethodGet, "/api/billing/unknown", nil)
	rec404 := httptest.NewRecorder()
	handler.ServeHTTP(rec404, req404)

	if rec404.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown route, got %d", rec404.Code)
	}
}

func TestBilling_Concurrency(t *testing.T) {
	meter := NewBillingMeter(DefaultCloudflareRates())
	var wg sync.WaitGroup
	numWorkers := 40
	recordsPerWorker := 50

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < recordsPerWorker; j++ {
				meter.RecordUsage(UsageRecord{
					TenantID:     "tenant-concurrent",
					AccountID:    "cf-acc-pool",
					Service:      "workers",
					ResourceType: "requests",
					Quantity:     10,
					DurationMs:   2,
				})
			}
		}(i)
	}

	wg.Wait()

	totalReqs := int64(numWorkers * recordsPerWorker * 10)
	_, _, reqs, _, _ := meter.GetClusterTotals()
	if reqs != totalReqs {
		t.Fatalf("concurrency mismatch: expected %d requests, got %d", totalReqs, reqs)
	}

	entries := meter.GetRecentEntries(100, "tenant-concurrent")
	if len(entries) == 0 {
		t.Fatalf("expected non-empty recent entries")
	}
}
