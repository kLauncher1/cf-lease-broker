package cloudflare

import (
	"context"
	"testing"
	"time"
)

func TestCloudflareClient(t *testing.T) {
	client := NewClient()
	if client == nil {
		t.Fatalf("expected non-nil client")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	status := client.CheckConnectivity(ctx)
	if status.CheckedAt.IsZero() {
		t.Errorf("expected non-zero CheckedAt")
	}
	if status.APIEndpoint == "" {
		t.Errorf("expected non-empty APIEndpoint")
	}
}
