package storage

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kLauncher1/cf-lease-broker/internal/broker"
)

// ManifestNamespace identifies the KV key space used to index object manifests.
const ManifestNamespace = "r2-manifests"

// ObjectManifest records striped storage layout, chunks, and metadata for an R2 object.
type ObjectManifest struct {
	Bucket          string    `json:"bucket"`
	Key             string    `json:"key"`
	Size            int64     `json:"size"`
	ContentType     string    `json:"content_type"`
	ETag            string    `json:"etag"`
	Chunks          []Chunk   `json:"chunks"`
	StripedAccounts []string  `json:"striped_accounts"`
	CreatedAt       time.Time `json:"created_at"`
	ModifiedAt      time.Time `json:"modified_at"`
}

// S3ListBucketResult defines the standard S3 XML serialization for ListObjectsV2.
type S3ListBucketResult struct {
	XMLName     xml.Name        `xml:"ListBucketResult"`
	Xmlns       string          `xml:"xmlns,attr"`
	Name        string          `xml:"Name"`
	Prefix      string          `xml:"Prefix"`
	KeyCount    int             `xml:"KeyCount"`
	MaxKeys     int             `xml:"MaxKeys"`
	IsTruncated bool            `xml:"IsTruncated"`
	Contents    []S3ObjectEntry `xml:"Contents"`
}

// S3ObjectEntry represents an individual object within an S3 bucket listing.
type S3ObjectEntry struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
}

// R2Gateway implements an S3-compatible virtual storage gateway striped across R2 buckets.
type R2Gateway struct {
	poolMgr *broker.PoolManager
	kvCache *KVCacheLayer
	webhook *WebhookDispatcher
	chunker *Chunker
}

// NewR2Gateway creates a new virtual S3 gateway backed by pooled R2 and KV layers.
func NewR2Gateway(pm *broker.PoolManager, kv *KVCacheLayer, wd *WebhookDispatcher, chunkSize int64) *R2Gateway {
	return &R2Gateway{
		poolMgr: pm,
		kvCache: kv,
		webhook: wd,
		chunker: NewChunker(chunkSize),
	}
}

func sanitizeStoragePath(rawPath string) (bucket, key string, err error) {
	trimmed := strings.TrimPrefix(rawPath, "/s3")
	trimmed = strings.TrimPrefix(trimmed, "/")

	if trimmed == "" {
		return "", "", fmt.Errorf("missing bucket name")
	}

	parts := strings.SplitN(trimmed, "/", 2)
	bucket = parts[0]
	if len(parts) > 1 {
		key = parts[1]
	}

	// Validate bucket name
	if len(bucket) < 3 || len(bucket) > 63 || strings.Contains(bucket, "..") {
		return "", "", fmt.Errorf("invalid S3 bucket name: %s", bucket)
	}

	// Validate key against directory traversal
	if key != "" {
		if strings.Contains(key, "..") || strings.HasPrefix(key, "/") || strings.Contains(key, "\\") {
			return "", "", fmt.Errorf("invalid S3 object key (path traversal detected): %s", key)
		}
	}

	return bucket, key, nil
}

// ServeHTTP routes S3 REST operations (PUT, GET, HEAD, DELETE, LIST).
func (g *R2Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Support JSON inspection endpoint /s3/inspect/{bucket}/{key...}
	if strings.HasPrefix(r.URL.Path, "/s3/inspect/") {
		g.handleInspect(w, r)
		return
	}

	bucket, key, err := sanitizeStoragePath(r.URL.Path)
	if err != nil {
		g.writeS3Error(w, http.StatusBadRequest, "InvalidRequest", err.Error())
		return
	}

	if key == "" {
		// Bucket-level request (e.g. ListObjectsV2)
		if r.Method == http.MethodGet {
			g.handleListObjects(w, r, bucket)
			return
		}
		g.writeS3Error(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "Method not allowed on bucket root")
		return
	}

	switch r.Method {
	case http.MethodPut:
		g.handlePutObject(w, r, bucket, key)
	case http.MethodGet:
		g.handleGetObject(w, r, bucket, key)
	case http.MethodHead:
		g.handleHeadObject(w, r, bucket, key)
	case http.MethodDelete:
		g.handleDeleteObject(w, r, bucket, key)
	default:
		g.writeS3Error(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "Unsupported S3 HTTP method")
	}
}

func (g *R2Gateway) handlePutObject(w http.ResponseWriter, r *http.Request, bucket, key string) {
	healthyAccounts := g.poolMgr.ListHealthyAccountIDs()
	if len(healthyAccounts) == 0 {
		g.writeS3Error(w, http.StatusServiceUnavailable, "SlowDown", "No healthy Cloudflare accounts available in storage pool")
		return
	}

	// Max upload bound: 50MB per single request to protect heap
	const maxUploadSize = 50 * 1024 * 1024
	chunks, totalSize, etag, err := g.chunker.Split(io.LimitReader(r.Body, maxUploadSize), healthyAccounts)
	if err != nil {
		g.writeS3Error(w, http.StatusInternalServerError, "InternalError", fmt.Sprintf("Chunking failed: %s", err))
		return
	}

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	accountSet := make(map[string]bool)
	for _, chk := range chunks {
		accountSet[chk.AccountID] = true
		// Convert bytes to MB for account telemetry
		mb := float64(chk.Size) / (1024 * 1024)
		_ = g.poolMgr.UpdateR2Storage(chk.AccountID, mb)
	}

	var stripedAccounts []string
	for acc := range accountSet {
		stripedAccounts = append(stripedAccounts, acc)
		if account, err := g.poolMgr.GetAccount(acc); err == nil {
			g.webhook.CheckAccount(*account)
		}
	}

	// Persist striped chunks across tenant storage partitions
	for _, chk := range chunks {
		chunkNS := fmt.Sprintf("r2-chunks-%s", chk.AccountID)
		if _, err := g.kvCache.Set(chunkNS, chk.ID, chk.Data, 0); err != nil {
			g.writeS3Error(w, http.StatusInternalServerError, "InternalError", fmt.Sprintf("Failed to store chunk %s: %s", chk.ID, err))
			return
		}
	}

	now := time.Now().UTC()
	manifest := ObjectManifest{
		Bucket:          bucket,
		Key:             key,
		Size:            totalSize,
		ContentType:     contentType,
		ETag:            etag,
		Chunks:          chunks,
		StripedAccounts: stripedAccounts,
		CreatedAt:       now,
		ModifiedAt:      now,
	}

	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		g.writeS3Error(w, http.StatusInternalServerError, "InternalError", "Failed to encode object manifest")
		return
	}

	kvKey := fmt.Sprintf("%s/%s", bucket, key)
	_, err = g.kvCache.Set(ManifestNamespace, kvKey, manifestBytes, 0)
	if err != nil {
		g.writeS3Error(w, http.StatusInternalServerError, "InternalError", "Failed to persist manifest into KV cache")
		return
	}

	w.Header().Set("ETag", fmt.Sprintf("\"%s\"", etag))
	w.Header().Set("x-amz-meta-striped-chunks", strconv.Itoa(len(chunks)))
	w.Header().Set("x-amz-meta-accounts", strings.Join(stripedAccounts, ","))
	w.WriteHeader(http.StatusOK)
}

func (g *R2Gateway) hydrateChunks(chunks []Chunk) ([]Chunk, error) {
	hydrated := make([]Chunk, len(chunks))
	for i, chk := range chunks {
		chunkNS := fmt.Sprintf("r2-chunks-%s", chk.AccountID)
		val, hit, _ := g.kvCache.Get(chunkNS, chk.ID)
		if !hit {
			return nil, fmt.Errorf("chunk %s missing from tenant %s storage", chk.ID, chk.AccountID)
		}
		chk.Data = val
		hydrated[i] = chk
	}
	return hydrated, nil
}

func (g *R2Gateway) handleGetObject(w http.ResponseWriter, r *http.Request, bucket, key string) {
	manifest, found := g.loadManifest(bucket, key)
	if !found {
		g.writeS3Error(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.")
		return
	}

	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" && strings.HasPrefix(rangeHeader, "bytes=") {
		g.serveRangeResponse(w, r, manifest, strings.TrimPrefix(rangeHeader, "bytes="))
		return
	}

	hydratedChunks, err := g.hydrateChunks(manifest.Chunks)
	if err != nil {
		g.writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	data, err := g.chunker.Reassemble(hydratedChunks)
	if err != nil {
		g.writeS3Error(w, http.StatusInternalServerError, "InternalError", fmt.Sprintf("Object reassembly failed: %s", err))
		return
	}

	w.Header().Set("Content-Type", manifest.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(manifest.Size, 10))
	w.Header().Set("ETag", fmt.Sprintf("\"%s\"", manifest.ETag))
	w.Header().Set("x-amz-meta-striped-chunks", strconv.Itoa(len(manifest.Chunks)))
	w.Header().Set("x-cf-kv-cached", "true")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (g *R2Gateway) serveRangeResponse(w http.ResponseWriter, r *http.Request, manifest *ObjectManifest, rangeSpec string) {
	parts := strings.Split(rangeSpec, "-")
	if len(parts) != 2 {
		g.writeS3Error(w, http.StatusRequestedRangeNotSatisfiable, "InvalidRange", "Invalid Range header specification")
		return
	}

	start, err1 := strconv.ParseInt(parts[0], 10, 64)
	var end int64
	if parts[1] == "" {
		end = manifest.Size - 1
	} else {
		var err2 error
		end, err2 = strconv.ParseInt(parts[1], 10, 64)
		if err2 != nil {
			g.writeS3Error(w, http.StatusRequestedRangeNotSatisfiable, "InvalidRange", "Invalid Range end value")
			return
		}
	}

	if err1 != nil || start < 0 || end < start || start >= manifest.Size {
		g.writeS3Error(w, http.StatusRequestedRangeNotSatisfiable, "InvalidRange", "Range exceeds object bounds")
		return
	}

	hydratedChunks, err := g.hydrateChunks(manifest.Chunks)
	if err != nil {
		g.writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	data, err := g.chunker.ReadRange(hydratedChunks, start, end)
	if err != nil {
		g.writeS3Error(w, http.StatusInternalServerError, "InternalError", fmt.Sprintf("Range retrieval error: %s", err))
		return
	}

	w.Header().Set("Content-Type", manifest.ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+int64(len(data))-1, manifest.Size))
	w.Header().Set("ETag", fmt.Sprintf("\"%s\"", manifest.ETag))
	w.Header().Set("x-amz-meta-striped-chunks", strconv.Itoa(len(manifest.Chunks)))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(data)
}

func (g *R2Gateway) handleHeadObject(w http.ResponseWriter, r *http.Request, bucket, key string) {
	manifest, found := g.loadManifest(bucket, key)
	if !found {
		g.writeS3Error(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.")
		return
	}

	w.Header().Set("Content-Type", manifest.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(manifest.Size, 10))
	w.Header().Set("ETag", fmt.Sprintf("\"%s\"", manifest.ETag))
	w.Header().Set("x-amz-meta-striped-chunks", strconv.Itoa(len(manifest.Chunks)))
	w.Header().Set("x-amz-meta-accounts", strings.Join(manifest.StripedAccounts, ","))
	w.WriteHeader(http.StatusOK)
}

func (g *R2Gateway) handleDeleteObject(w http.ResponseWriter, r *http.Request, bucket, key string) {
	manifest, found := g.loadManifest(bucket, key)
	if found {
		for _, chk := range manifest.Chunks {
			mb := float64(chk.Size) / (1024 * 1024)
			_ = g.poolMgr.UpdateR2Storage(chk.AccountID, -mb)
			chunkNS := fmt.Sprintf("r2-chunks-%s", chk.AccountID)
			g.kvCache.Delete(chunkNS, chk.ID)
		}
		kvKey := fmt.Sprintf("%s/%s", bucket, key)
		g.kvCache.Delete(ManifestNamespace, kvKey)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (g *R2Gateway) handleListObjects(w http.ResponseWriter, r *http.Request, bucket string) {
	prefix := r.URL.Query().Get("prefix")
	maxKeysStr := r.URL.Query().Get("max-keys")
	maxKeys := 1000
	if maxKeysStr != "" {
		if mk, err := strconv.Atoi(maxKeysStr); err == nil && mk > 0 {
			maxKeys = mk
		}
	}

	allKeys := g.kvCache.ListKeys(ManifestNamespace)
	bucketPrefix := bucket + "/"

	var entries []S3ObjectEntry
	for _, k := range allKeys {
		if !strings.HasPrefix(k, bucketPrefix) {
			continue
		}
		objKey := strings.TrimPrefix(k, bucketPrefix)
		if prefix != "" && !strings.HasPrefix(objKey, prefix) {
			continue
		}

		manifest, ok := g.loadManifest(bucket, objKey)
		if !ok {
			continue
		}

		entries = append(entries, S3ObjectEntry{
			Key:          manifest.Key,
			LastModified: manifest.ModifiedAt.Format(time.RFC3339),
			ETag:         fmt.Sprintf("\"%s\"", manifest.ETag),
			Size:         manifest.Size,
			StorageClass: "STANDARD",
		})

		if len(entries) >= maxKeys {
			break
		}
	}

	result := S3ListBucketResult{
		Xmlns:       "http://s3.amazonaws.com/doc/2006-03-01/",
		Name:        bucket,
		Prefix:      prefix,
		KeyCount:    len(entries),
		MaxKeys:     maxKeys,
		IsTruncated: false,
		Contents:    entries,
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(result)
}

func (g *R2Gateway) handleInspect(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/s3/inspect/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) < 2 {
		http.Error(w, `{"error":"invalid inspect path: expected /s3/inspect/{bucket}/{key}"}`, http.StatusBadRequest)
		return
	}

	manifest, found := g.loadManifest(parts[0], parts[1])
	if !found {
		http.Error(w, `{"error":"object not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(manifest)
}

func (g *R2Gateway) loadManifest(bucket, key string) (*ObjectManifest, bool) {
	kvKey := fmt.Sprintf("%s/%s", bucket, key)
	data, hit, _ := g.kvCache.Get(ManifestNamespace, kvKey)
	if !hit {
		return nil, false
	}

	var m ObjectManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, false
	}
	return &m, true
}

func (g *R2Gateway) writeS3Error(w http.ResponseWriter, status int, code, message string) {
	type s3Error struct {
		XMLName xml.Name `xml:"Error"`
		Code    string   `xml:"Code"`
		Message string   `xml:"Message"`
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(s3Error{
		Code:    code,
		Message: message,
	})
}
