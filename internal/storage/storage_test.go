package storage

import (
	"bytes"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kLauncher1/cf-lease-broker/internal/broker"
)

func TestChunkerSplitAndReassemble(t *testing.T) {
	accounts := []string{"acc-1", "acc-2", "acc-3"}
	chunker := NewChunker(256) // 256 bytes per chunk

	payload := make([]byte, 1000)
	for i := range payload {
		payload[i] = byte(i % 251)
	}

	chunks, totalSize, etag, err := chunker.Split(bytes.NewReader(payload), accounts)
	if err != nil {
		t.Fatalf("Split failed: %v", err)
	}
	if totalSize != 1000 {
		t.Fatalf("expected totalSize 1000, got %d", totalSize)
	}
	if len(chunks) != 4 { // ceil(1000 / 256) = 4
		t.Fatalf("expected 4 chunks, got %d", len(chunks))
	}
	if etag == "" {
		t.Errorf("expected non-empty etag")
	}

	// Verify account distribution
	if chunks[0].AccountID != "acc-1" || chunks[1].AccountID != "acc-2" || chunks[2].AccountID != "acc-3" || chunks[3].AccountID != "acc-1" {
		t.Errorf("unexpected account distribution across chunks")
	}

	// Reassemble
	reassembled, err := chunker.Reassemble(chunks)
	if err != nil {
		t.Fatalf("Reassemble failed: %v", err)
	}
	if !bytes.Equal(payload, reassembled) {
		t.Fatalf("reassembled content does not match original payload")
	}
}

func TestChunkerRangeReading(t *testing.T) {
	accounts := []string{"acc-1", "acc-2"}
	chunker := NewChunker(100) // 100 bytes per chunk

	payload := make([]byte, 350)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	chunks, _, _, err := chunker.Split(bytes.NewReader(payload), accounts)
	if err != nil {
		t.Fatalf("Split failed: %v", err)
	}

	// Test cross-boundary range: 50 to 250 (spans chunk 0, 1, 2)
	sliced, err := chunker.ReadRange(chunks, 50, 250)
	if err != nil {
		t.Fatalf("ReadRange failed: %v", err)
	}
	expected := payload[50:251]
	if !bytes.Equal(sliced, expected) {
		t.Fatalf("range data mismatch: expected %d bytes, got %d bytes", len(expected), len(sliced))
	}

	// Test boundary edge: single byte at end
	single, err := chunker.ReadRange(chunks, 349, 349)
	if err != nil {
		t.Fatalf("ReadRange single byte failed: %v", err)
	}
	if len(single) != 1 || single[0] != payload[349] {
		t.Errorf("unexpected single byte range result")
	}

	// Test invalid range: start exceeds total
	_, err = chunker.ReadRange(chunks, 400, 500)
	if err == nil {
		t.Errorf("expected error for range exceeding total size")
	}
}

func TestKVCachePartitioningAndHitMiss(t *testing.T) {
	pm := broker.NewPoolManager()
	kv := NewKVCacheLayer(pm)

	// Set items
	tenant, err := kv.Set("configs", "app.json", []byte(`{"env":"prod"}`), 1*time.Hour)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if tenant == "" {
		t.Errorf("expected assigned tenant ID")
	}

	// Get existing item (HIT)
	val, hit, getTenant := kv.Get("configs", "app.json")
	if !hit || string(val) != `{"env":"prod"}` || getTenant != tenant {
		t.Fatalf("unexpected Get hit result: hit=%v, val=%s, tenant=%s", hit, string(val), getTenant)
	}

	// Get non-existing item (MISS)
	_, hitMiss, _ := kv.Get("configs", "missing.json")
	if hitMiss {
		t.Errorf("expected hit=false for missing key")
	}

	// ListKeys
	keys := kv.ListKeys("configs")
	if len(keys) != 1 || keys[0] != "app.json" {
		t.Errorf("unexpected ListKeys: %v", keys)
	}

	// Stats
	stats := kv.Stats()
	if stats.Hits != 1 || stats.Misses != 1 || stats.TotalKeys != 1 {
		t.Errorf("unexpected stats: %+v", stats)
	}

	// Delete
	deleted := kv.Delete("configs", "app.json")
	if !deleted {
		t.Errorf("expected delete to return true")
	}
	_, hitAfterDelete, _ := kv.Get("configs", "app.json")
	if hitAfterDelete {
		t.Errorf("key should not exist after deletion")
	}
}

func TestKVCacheTTLExpiration(t *testing.T) {
	pm := broker.NewPoolManager()
	kv := NewKVCacheLayer(pm)

	_, err := kv.Set("transient", "temp.token", []byte("12345"), 15*time.Millisecond)
	if err != nil {
		t.Fatalf("Set with TTL failed: %v", err)
	}

	// Immediate get
	_, hit, _ := kv.Get("transient", "temp.token")
	if !hit {
		t.Fatalf("expected immediate hit")
	}

	time.Sleep(25 * time.Millisecond)

	// Get after expiration
	_, hitExpired, _ := kv.Get("transient", "temp.token")
	if hitExpired {
		t.Errorf("expected key to be expired and evicted")
	}
}

func TestR2GatewayLifecycleAndS3Endpoints(t *testing.T) {
	pm := broker.NewPoolManager()
	kv := NewKVCacheLayer(pm)
	wd := NewWebhookDispatcher("")
	gw := NewR2Gateway(pm, kv, wd, 512) // 512 bytes chunk threshold

	server := httptest.NewServer(gw)
	defer server.Close()

	client := server.Client()

	// 1. PUT Object: /s3/mybucket/docs/manual.txt
	data := strings.Repeat("ABCDEFGHIJ", 120) // 1200 bytes -> 3 striped chunks
	putReq, _ := http.NewRequest(http.MethodPut, server.URL+"/s3/mybucket/docs/manual.txt", strings.NewReader(data))
	putReq.Header.Set("Content-Type", "text/plain")

	putResp, err := client.Do(putReq)
	if err != nil {
		t.Fatalf("PUT request failed: %v", err)
	}
	defer putResp.Body.Close()

	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("expected PUT 200 OK, got %d", putResp.StatusCode)
	}
	etag := putResp.Header.Get("ETag")
	if etag == "" {
		t.Errorf("expected ETag header in PUT response")
	}
	chunksCount := putResp.Header.Get("x-amz-meta-striped-chunks")
	if chunksCount != "3" {
		t.Errorf("expected 3 striped chunks, got %s", chunksCount)
	}

	// 2. HEAD Object: /s3/mybucket/docs/manual.txt
	headReq, _ := http.NewRequest(http.MethodHead, server.URL+"/s3/mybucket/docs/manual.txt", nil)
	headResp, err := client.Do(headReq)
	if err != nil {
		t.Fatalf("HEAD request failed: %v", err)
	}
	defer headResp.Body.Close()

	if headResp.StatusCode != http.StatusOK {
		t.Fatalf("expected HEAD 200 OK, got %d", headResp.StatusCode)
	}
	if headResp.Header.Get("Content-Type") != "text/plain" {
		t.Errorf("unexpected Content-Type: %s", headResp.Header.Get("Content-Type"))
	}
	if headResp.Header.Get("Content-Length") != "1200" {
		t.Errorf("unexpected Content-Length: %s", headResp.Header.Get("Content-Length"))
	}

	// 3. GET Object: full read
	getReq, _ := http.NewRequest(http.MethodGet, server.URL+"/s3/mybucket/docs/manual.txt", nil)
	getResp, err := client.Do(getReq)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected GET 200 OK, got %d", getResp.StatusCode)
	}
	body, _ := io.ReadAll(getResp.Body)
	if string(body) != data {
		t.Fatalf("GET body does not match uploaded content")
	}

	// 4. GET Object: Range read (bytes=10-29)
	rangeReq, _ := http.NewRequest(http.MethodGet, server.URL+"/s3/mybucket/docs/manual.txt", nil)
	rangeReq.Header.Set("Range", "bytes=10-29")
	rangeResp, err := client.Do(rangeReq)
	if err != nil {
		t.Fatalf("GET range request failed: %v", err)
	}
	defer rangeResp.Body.Close()

	if rangeResp.StatusCode != http.StatusPartialContent {
		t.Fatalf("expected 206 Partial Content, got %d", rangeResp.StatusCode)
	}
	rangeBody, _ := io.ReadAll(rangeResp.Body)
	if string(rangeBody) != data[10:30] {
		t.Errorf("range body mismatch: expected %s, got %s", data[10:30], string(rangeBody))
	}
	if rangeResp.Header.Get("Content-Range") != "bytes 10-29/1200" {
		t.Errorf("unexpected Content-Range: %s", rangeResp.Header.Get("Content-Range"))
	}

	// 5. Inspect JSON helper: /s3/inspect/mybucket/docs/manual.txt
	inspectReq, _ := http.NewRequest(http.MethodGet, server.URL+"/s3/inspect/mybucket/docs/manual.txt", nil)
	inspectResp, err := client.Do(inspectReq)
	if err != nil {
		t.Fatalf("Inspect request failed: %v", err)
	}
	defer inspectResp.Body.Close()
	if inspectResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 from inspect endpoint, got %d", inspectResp.StatusCode)
	}

	// 6. ListObjectsV2: GET /s3/mybucket
	listReq, _ := http.NewRequest(http.MethodGet, server.URL+"/s3/mybucket", nil)
	listResp, err := client.Do(listReq)
	if err != nil {
		t.Fatalf("ListObjects request failed: %v", err)
	}
	defer listResp.Body.Close()

	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected ListObjects 200 OK, got %d", listResp.StatusCode)
	}
	var listResult S3ListBucketResult
	if err := xml.NewDecoder(listResp.Body).Decode(&listResult); err != nil {
		t.Fatalf("failed to decode S3 ListBucketResult XML: %v", err)
	}
	if listResult.Name != "mybucket" || listResult.KeyCount != 1 || listResult.Contents[0].Key != "docs/manual.txt" {
		t.Errorf("unexpected listResult: %+v", listResult)
	}

	// 7. DELETE Object
	delReq, _ := http.NewRequest(http.MethodDelete, server.URL+"/s3/mybucket/docs/manual.txt", nil)
	delResp, err := client.Do(delReq)
	if err != nil {
		t.Fatalf("DELETE request failed: %v", err)
	}
	defer delResp.Body.Close()

	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected DELETE 204 No Content, got %d", delResp.StatusCode)
	}

	// Verify 404 after deletion
	getAfterDel, _ := http.NewRequest(http.MethodGet, server.URL+"/s3/mybucket/docs/manual.txt", nil)
	getAfterDelResp, err := client.Do(getAfterDel)
	if err != nil {
		t.Fatalf("GET after delete failed: %v", err)
	}
	defer getAfterDelResp.Body.Close()
	if getAfterDelResp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 after deletion, got %d", getAfterDelResp.StatusCode)
	}
}

func TestR2GatewayValidationAndPathTraversal(t *testing.T) {
	pm := broker.NewPoolManager()
	kv := NewKVCacheLayer(pm)
	wd := NewWebhookDispatcher("")
	gw := NewR2Gateway(pm, kv, wd, 1024)

	server := httptest.NewServer(gw)
	defer server.Close()
	client := server.Client()

	// Path traversal attempt: /s3/mybucket/../../etc/passwd
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/s3/mybucket/../../etc/passwd", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Traversal should be rejected as bad request or not allowed
	if resp.StatusCode == http.StatusOK {
		t.Errorf("expected failure on path traversal attempt, got %d", resp.StatusCode)
	}

	// Invalid short bucket: /s3/ab/test.txt
	shortReq, _ := http.NewRequest(http.MethodGet, server.URL+"/s3/ab/test.txt", nil)
	shortResp, err := client.Do(shortReq)
	if err != nil {
		t.Fatalf("short bucket request failed: %v", err)
	}
	defer shortResp.Body.Close()
	if shortResp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for short bucket name, got %d", shortResp.StatusCode)
	}
}

func TestWebhookDispatcherThresholdsAndAlerts(t *testing.T) {
	wd := NewWebhookDispatcher("")

	// Normal usage: 50%
	normalAcc := broker.AccountStatus{
		ID:          "cf-tenant-test",
		WorkerLimit: 100000,
		WorkerUsed:  50000,
		R2StorageMB: 1000.0,
	}
	alerts := wd.CheckAccount(normalAcc)
	if len(alerts) != 0 {
		t.Errorf("expected 0 alerts for 50%% usage, got %d", len(alerts))
	}

	// Warning usage: 82% Worker
	warnAcc := broker.AccountStatus{
		ID:          "cf-tenant-warn",
		WorkerLimit: 100000,
		WorkerUsed:  82000,
		R2StorageMB: 2000.0,
	}
	warnAlerts := wd.CheckAccount(warnAcc)
	if len(warnAlerts) != 1 || warnAlerts[0].Level != "WARNING" {
		t.Fatalf("expected 1 WARNING alert, got %+v", warnAlerts)
	}

	// Test cooldown: duplicate check should return 0 new alerts
	cooldownAlerts := wd.CheckAccount(warnAcc)
	if len(cooldownAlerts) != 0 {
		t.Errorf("expected cooldown to suppress alert, got %d", len(cooldownAlerts))
	}

	// Test HTTP handler
	req := httptest.NewRequest(http.MethodGet, "/api/webhooks/alerts", nil)
	rec := httptest.NewRecorder()
	wd.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 from webhooks endpoint, got %d", rec.Code)
	}

	testReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/test", nil)
	testRec := httptest.NewRecorder()
	wd.ServeHTTP(testRec, testReq)
	if testRec.Code != http.StatusOK {
		t.Errorf("expected 200 from webhooks test endpoint, got %d", testRec.Code)
	}
}

func TestKVCacheHTTPRESTEndpoints(t *testing.T) {
	pm := broker.NewPoolManager()
	kv := NewKVCacheLayer(pm)

	server := httptest.NewServer(kv)
	defer server.Close()
	client := server.Client()

	// 1. PUT /api/kv/settings/theme
	putReq, _ := http.NewRequest(http.MethodPut, server.URL+"/api/kv/settings/theme", strings.NewReader("dark-glass"))
	putResp, err := client.Do(putReq)
	if err != nil {
		t.Fatalf("PUT KV failed: %v", err)
	}
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from PUT KV, got %d", putResp.StatusCode)
	}

	// 2. GET /api/kv/settings/theme
	getReq, _ := http.NewRequest(http.MethodGet, server.URL+"/api/kv/settings/theme", nil)
	getResp, err := client.Do(getReq)
	if err != nil {
		t.Fatalf("GET KV failed: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from GET KV, got %d", getResp.StatusCode)
	}
	body, _ := io.ReadAll(getResp.Body)
	if string(body) != "dark-glass" {
		t.Errorf("expected 'dark-glass', got '%s'", string(body))
	}
	if getResp.Header.Get("X-CF-KV-Cache") != "HIT" {
		t.Errorf("expected X-CF-KV-Cache HIT header")
	}

	// 3. GET /api/kv/stats
	statsReq, _ := http.NewRequest(http.MethodGet, server.URL+"/api/kv/stats", nil)
	statsResp, err := client.Do(statsReq)
	if err != nil {
		t.Fatalf("GET /api/kv/stats failed: %v", err)
	}
	defer statsResp.Body.Close()
	if statsResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK from stats, got %d", statsResp.StatusCode)
	}

	// 4. DELETE /api/kv/settings/theme
	delReq, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/kv/settings/theme", nil)
	delResp, err := client.Do(delReq)
	if err != nil {
		t.Fatalf("DELETE /api/kv/settings/theme failed: %v", err)
	}
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK from DELETE KV, got %d", delResp.StatusCode)
	}
}
