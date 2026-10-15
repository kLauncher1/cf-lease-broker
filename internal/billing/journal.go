package billing

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// OpenCostWindow represents the time window for an OpenCost allocation.
type OpenCostWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// OpenCostProperties holds metadata labels for OpenCost 1.0.
type OpenCostProperties struct {
	Cluster  string `json:"cluster"`
	Tenant   string `json:"tenant"`
	Provider string `json:"provider"`
}

// OpenCostAllocation represents an OpenCost 1.0 line item allocation.
type OpenCostAllocation struct {
	Name        string             `json:"name"`
	Properties  OpenCostProperties `json:"properties"`
	Window      OpenCostWindow     `json:"window"`
	CpuCost     float64            `json:"cpuCost"`
	RamCost     float64            `json:"ramCost"`
	NetworkCost float64            `json:"networkCost"`
	StorageCost float64            `json:"storageCost"`
	TotalCost   float64            `json:"totalCost"`
}

// OpenCostReport encapsulates a complete OpenCost specification export.
type OpenCostReport struct {
	Version  string               `json:"version"`
	Provider string               `json:"provider"`
	Window   string               `json:"window"`
	Data     []OpenCostAllocation `json:"data"`
}

// JournalExporter converts ledger cost entries and tenant summaries into exportable formats.
type JournalExporter struct{}

// NewJournalExporter constructs a new JournalExporter instance.
func NewJournalExporter() *JournalExporter {
	return &JournalExporter{}
}

// ExportCSV formats a slice of CostEntry items as RFC 4180 standard CSV.
func (e *JournalExporter) ExportCSV(entries []CostEntry) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	header := []string{
		"id",
		"timestamp",
		"tenant_id",
		"account_id",
		"service",
		"resource_type",
		"quantity",
		"unit_rate_micro_cents",
		"total_cost_micro_cents",
		"cost_usd",
		"currency",
	}

	if err := writer.Write(header); err != nil {
		return nil, fmt.Errorf("failed to write CSV header: %w", err)
	}

	for _, entry := range entries {
		costUSD := fmt.Sprintf("%.6f", float64(entry.TotalCostMicroCents)/1000000.0)
		row := []string{
			sanitizeCSVField(entry.ID),
			entry.Timestamp.UTC().Format(time.RFC3339),
			sanitizeCSVField(entry.TenantID),
			sanitizeCSVField(entry.AccountID),
			sanitizeCSVField(entry.Service),
			sanitizeCSVField(entry.ResourceType),
			strconv.FormatInt(entry.Quantity, 10),
			strconv.FormatInt(entry.UnitRateMicroCents, 10),
			strconv.FormatInt(entry.TotalCostMicroCents, 10),
			costUSD,
			sanitizeCSVField(entry.Currency),
		}
		if err := writer.Write(row); err != nil {
			return nil, fmt.Errorf("failed to write CSV row: %w", err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("csv flush error: %w", err)
	}

	return buf.Bytes(), nil
}

// ExportJSONL formats a slice of CostEntry items as newline-delimited JSON (JSONL).
func (e *JournalExporter) ExportJSONL(entries []CostEntry) ([]byte, error) {
	var buf bytes.Buffer

	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal JSONL entry %s: %w", entry.ID, err)
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}

	return buf.Bytes(), nil
}

// ExportOpenCost formats tenant billing summaries into the OpenCost 1.0 JSON specification.
func (e *JournalExporter) ExportOpenCost(tenants map[string]*TenantSummary, windowLabel string) ([]byte, error) {
	now := time.Now().UTC()
	startStr := now.Truncate(24 * time.Hour).Format(time.RFC3339)
	endStr := now.Format(time.RFC3339)
	if windowLabel == "" {
		windowLabel = fmt.Sprintf("%s/%s", startStr, endStr)
	}

	allocations := make([]OpenCostAllocation, 0, len(tenants))

	for tenantID, summary := range tenants {
		var cpuCost float64 = 0.0
		var storageCost float64 = 0.0
		var networkCost float64 = 0.0

		for service, costMicroCents := range summary.ServiceBreakdown {
			costUSD := float64(costMicroCents) / 1000000.0
			switch service {
			case "workers":
				cpuCost += costUSD
			case "r2":
				storageCost += costUSD
			case "kv":
				storageCost += costUSD
			case "proxy":
				networkCost += costUSD
			default:
				cpuCost += costUSD
			}
		}

		totalUSD := float64(summary.TotalCostMicroCents) / 1000000.0

		alloc := OpenCostAllocation{
			Name: tenantID,
			Properties: OpenCostProperties{
				Cluster:  "cf-lease-broker",
				Tenant:   tenantID,
				Provider: "Cloudflare",
			},
			Window: OpenCostWindow{
				Start: startStr,
				End:   endStr,
			},
			CpuCost:     cpuCost,
			RamCost:     0.0,
			NetworkCost: networkCost,
			StorageCost: storageCost,
			TotalCost:   totalUSD,
		}

		allocations = append(allocations, alloc)
	}

	report := OpenCostReport{
		Version:  "1.0",
		Provider: "Cloudflare Edge Broker",
		Window:   windowLabel,
		Data:     allocations,
	}

	return json.MarshalIndent(report, "", "  ")
}

// sanitizeCSVField neutralizes CSV/formula injection (CWE-1236) by prepending a single quote
// if the string value starts with spreadsheet formula trigger characters ('=', '+', '-', '@', '\t', '\r').
func sanitizeCSVField(val string) string {
	if len(val) == 0 {
		return val
	}
	switch val[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + val
	default:
		return val
	}
}

