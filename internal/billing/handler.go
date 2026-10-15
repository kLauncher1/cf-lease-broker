package billing

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const (
	maxBillingBodyBytes = 64 * 1024 // 64 KB DoS protection bound
)

// BillingHandler exposes RESTful endpoints for billing telemetry, optimization, and exports.
type BillingHandler struct {
	meter     *BillingMeter
	optimizer *CostOptimizer
	exporter  *JournalExporter
}

// NewBillingHandler constructs a new BillingHandler instance.
func NewBillingHandler(meter *BillingMeter, optimizer *CostOptimizer) *BillingHandler {
	return &BillingHandler{
		meter:     meter,
		optimizer: optimizer,
		exporter:  NewJournalExporter(),
	}
}

// ServeHTTP routes incoming /api/billing/* requests.
func (h *BillingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/billing")
	path = strings.TrimPrefix(path, "/")

	switch {
	case path == "summary" && r.Method == http.MethodGet:
		h.handleSummary(w, r)
	case path == "ledger" && r.Method == http.MethodGet:
		h.handleLedger(w, r)
	case path == "meter" && r.Method == http.MethodPost:
		h.handleMeter(w, r)
	case path == "optimize" && r.Method == http.MethodPost:
		h.handleOptimize(w, r)
	case path == "migrations" && r.Method == http.MethodGet:
		h.handleMigrations(w, r)
	case path == "rates" && r.Method == http.MethodGet:
		h.handleGetRates(w, r)
	case path == "rates" && r.Method == http.MethodPost:
		h.handlePostRates(w, r)
	case path == "reset" && r.Method == http.MethodPost:
		h.handleReset(w, r)
	default:
		http.Error(w, `{"error":"endpoint not found"}`, http.StatusNotFound)
	}
}

func (h *BillingHandler) handleSummary(w http.ResponseWriter, _ *http.Request) {
	cost, savings, reqs, bytesCount, cpuMs := h.meter.GetClusterTotals()
	tenants := h.meter.GetAllTenants()
	accounts := h.meter.GetAllAccounts()

	resp := map[string]interface{}{
		"total_cluster_cost_micro_cents":    cost,
		"total_cluster_cost_usd":            fmt.Sprintf("%.6f", float64(cost)/1000000.0),
		"total_cluster_savings_micro_cents": savings,
		"total_cluster_savings_usd":        fmt.Sprintf("%.6f", float64(savings)/1000000.0),
		"total_cluster_requests":            reqs,
		"total_cluster_bytes":               bytesCount,
		"total_cluster_cpu_ms":              cpuMs,
		"tenants":                           tenants,
		"accounts":                          accounts,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *BillingHandler) handleLedger(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	format := strings.ToLower(q.Get("format"))
	tenantFilter := q.Get("tenant")
	limitStr := q.Get("limit")

	limit := 100
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
			if limit > 2000 {
				limit = 2000
			}
		}
	}

	entries := h.meter.GetRecentEntries(limit, tenantFilter)

	switch format {
	case "csv":
		csvBytes, err := h.exporter.ExportCSV(entries)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"failed to generate CSV: %s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=\"billing-ledger.csv\"")
		_, _ = w.Write(csvBytes)

	case "jsonl":
		jsonlBytes, err := h.exporter.ExportJSONL(entries)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"failed to generate JSONL: %s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=\"billing-ledger.jsonl\"")
		_, _ = w.Write(jsonlBytes)

	case "opencost":
		tenants := h.meter.GetAllTenants()
		openCostBytes, err := h.exporter.ExportOpenCost(tenants, "")
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"failed to generate OpenCost report: %s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=\"opencost-allocation.json\"")
		_, _ = w.Write(openCostBytes)

	default:
		// Default JSON response
		resp := map[string]interface{}{
			"count":   len(entries),
			"limit":   limit,
			"entries": entries,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func (h *BillingHandler) handleMeter(w http.ResponseWriter, r *http.Request) {
	limitedReader := io.LimitReader(r.Body, maxBillingBodyBytes)
	var record UsageRecord
	if err := json.NewDecoder(limitedReader).Decode(&record); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}

	entry := h.meter.RecordUsage(record)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(entry)
}

func (h *BillingHandler) handleOptimize(w http.ResponseWriter, _ *http.Request) {
	events := h.optimizer.EvaluateWorkloadMigration()
	totalSavings := h.optimizer.GetTotalSavings()

	resp := map[string]interface{}{
		"status":                     "success",
		"migrations_triggered":       len(events),
		"events":                     events,
		"total_savings_micro_cents": totalSavings,
		"total_savings_usd":         fmt.Sprintf("%.6f", float64(totalSavings)/1000000.0),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *BillingHandler) handleMigrations(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	events := h.optimizer.GetRecentMigrations(limit)
	savings := h.optimizer.GetTotalSavings()
	rule := h.optimizer.GetRule()

	resp := map[string]interface{}{
		"count":                      len(events),
		"total_savings_micro_cents": savings,
		"total_savings_usd":         fmt.Sprintf("%.6f", float64(savings)/1000000.0),
		"rule":                       rule,
		"migrations":                 events,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *BillingHandler) handleGetRates(w http.ResponseWriter, _ *http.Request) {
	rates := h.meter.GetRates()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rates)
}

func (h *BillingHandler) handlePostRates(w http.ResponseWriter, r *http.Request) {
	limitedReader := io.LimitReader(r.Body, maxBillingBodyBytes)
	var newRates CloudflareRates
	if err := json.NewDecoder(limitedReader).Decode(&newRates); err != nil {
		http.Error(w, `{"error":"invalid rates payload"}`, http.StatusBadRequest)
		return
	}

	h.meter.SetRates(newRates)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func (h *BillingHandler) handleReset(w http.ResponseWriter, _ *http.Request) {
	h.meter.Reset()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ledger reset successfully"})
}
