package cloudflare

import (
	"context"
	"net/http"
	"time"
)

// HealthStatus wraps Cloudflare API connection readiness.
type HealthStatus struct {
	Connected   bool      `json:"connected"`
	APIEndpoint string    `json:"api_endpoint"`
	CheckedAt   time.Time `json:"checked_at"`
	LatencyMS   int64     `json:"latency_ms"`
}

// Client interacts with Cloudflare API endpoints.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient initializes a Cloudflare API client with resilient timeouts.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 3 * time.Second},
		baseURL:    "https://api.cloudflare.com/client/v4",
	}
}

// CheckConnectivity verifies connectivity to Cloudflare public edge.
func (c *Client) CheckConnectivity(ctx context.Context) HealthStatus {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/user/tokens/verify", nil)
	if err != nil {
		return HealthStatus{
			Connected:   false,
			APIEndpoint: c.baseURL,
			CheckedAt:   time.Now().UTC(),
			LatencyMS:   -1,
		}
	}

	resp, err := c.httpClient.Do(req)
	latency := time.Since(start).Milliseconds()

	// Even if unauthorized (HTTP 401/400), connectivity to edge is successful
	connected := err == nil && resp.StatusCode > 0
	if resp != nil {
		resp.Body.Close()
	}

	return HealthStatus{
		Connected:   connected,
		APIEndpoint: c.baseURL,
		CheckedAt:   time.Now().UTC(),
		LatencyMS:   latency,
	}
}
