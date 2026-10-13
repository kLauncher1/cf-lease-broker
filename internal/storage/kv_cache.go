package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kLauncher1/cf-lease-broker/internal/broker"
)

// KVEntry represents an individual cached record mapped to a tenant partition.
type KVEntry struct {
	Namespace string    `json:"namespace"`
	Key       string    `json:"key"`
	Value     []byte    `json:"-"`
	TenantID  string    `json:"tenant_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// KVStats captures aggregate metrics across multi-tenant KV operations.
type KVStats struct {
	TotalKeys       int              `json:"total_keys"`
	Hits            int64            `json:"hits"`
	Misses          int64            `json:"misses"`
	Evictions       int64            `json:"evictions"`
	HitRatePercent  float64          `json:"hit_rate_percent"`
	PerTenantKeys   map[string]int   `json:"per_tenant_keys"`
	PerTenantWrites map[string]int64 `json:"per_tenant_writes"`
}

// KVCacheLayer coordinates partitioned KV caching across pooled Cloudflare tenants.
type KVCacheLayer struct {
	mu              sync.RWMutex
	poolMgr         *broker.PoolManager
	entries         map[string]*KVEntry // composite key: namespace:key
	hits            int64
	misses          int64
	evictions       int64
	perTenantWrites map[string]int64
}

// NewKVCacheLayer instantiates a new thread-safe partitioned KV caching service.
func NewKVCacheLayer(pm *broker.PoolManager) *KVCacheLayer {
	return &KVCacheLayer{
		poolMgr:         pm,
		entries:         make(map[string]*KVEntry),
		perTenantWrites: make(map[string]int64),
	}
}

func compositeKVKey(namespace, key string) string {
	return fmt.Sprintf("%s:%s", namespace, key)
}

// PartitionAccount selects the designated tenant for a key via consistent FNV hashing.
func (kv *KVCacheLayer) PartitionAccount(namespace, key string) (string, error) {
	healthy := kv.poolMgr.ListHealthyAccountIDs()
	if len(healthy) == 0 {
		return "", errors.New("no healthy tenant accounts available for KV partitioning")
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(compositeKVKey(namespace, key)))
	idx := int(h.Sum32()) % len(healthy)
	if idx < 0 {
		idx = -idx
	}
	return healthy[idx], nil
}

// Get retrieves a cached record, updating hit/miss counters and tenant read metrics.
func (kv *KVCacheLayer) Get(namespace, key string) ([]byte, bool, string) {
	kv.mu.Lock()
	defer kv.mu.Unlock()

	cKey := compositeKVKey(namespace, key)
	entry, exists := kv.entries[cKey]
	if !exists {
		kv.misses++
		return nil, false, ""
	}

	// Check TTL expiration
	if !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt) {
		delete(kv.entries, cKey)
		kv.evictions++
		kv.misses++
		return nil, false, ""
	}

	kv.hits++
	_ = kv.poolMgr.IncrementKVRead(entry.TenantID)

	valCopy := make([]byte, len(entry.Value))
	copy(valCopy, entry.Value)
	return valCopy, true, entry.TenantID
}

// Set stores a value partitioned across tenant accounts with an optional TTL.
func (kv *KVCacheLayer) Set(namespace, key string, val []byte, ttl time.Duration) (string, error) {
	tenantID, err := kv.PartitionAccount(namespace, key)
	if err != nil {
		return "", err
	}

	kv.mu.Lock()
	defer kv.mu.Unlock()

	now := time.Now().UTC()
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = now.Add(ttl)
	}

	valCopy := make([]byte, len(val))
	copy(valCopy, val)

	cKey := compositeKVKey(namespace, key)
	kv.entries[cKey] = &KVEntry{
		Namespace: namespace,
		Key:       key,
		Value:     valCopy,
		TenantID:  tenantID,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}
	kv.perTenantWrites[tenantID]++

	return tenantID, nil
}

// Delete evicts a key from the partitioned cache.
func (kv *KVCacheLayer) Delete(namespace, key string) bool {
	kv.mu.Lock()
	defer kv.mu.Unlock()

	cKey := compositeKVKey(namespace, key)
	_, exists := kv.entries[cKey]
	if exists {
		delete(kv.entries, cKey)
		kv.evictions++
		return true
	}
	return false
}

// ListKeys returns all active unexpired keys for a given namespace.
func (kv *KVCacheLayer) ListKeys(namespace string) []string {
	kv.mu.Lock()
	defer kv.mu.Unlock()

	now := time.Now()
	var keys []string
	for cKey, entry := range kv.entries {
		if entry.Namespace != namespace {
			continue
		}
		if !entry.ExpiresAt.IsZero() && now.After(entry.ExpiresAt) {
			delete(kv.entries, cKey)
			kv.evictions++
			continue
		}
		keys = append(keys, entry.Key)
	}
	return keys
}

// Stats returns a snapshot of cache efficiency and tenant key distributions.
func (kv *KVCacheLayer) Stats() KVStats {
	kv.mu.RLock()
	defer kv.mu.RUnlock()

	perTenant := make(map[string]int)
	writes := make(map[string]int64)

	for _, entry := range kv.entries {
		perTenant[entry.TenantID]++
	}
	for k, v := range kv.perTenantWrites {
		writes[k] = v
	}

	totalOps := kv.hits + kv.misses
	rate := 0.0
	if totalOps > 0 {
		rate = (float64(kv.hits) / float64(totalOps)) * 100.0
	}

	return KVStats{
		TotalKeys:       len(kv.entries),
		Hits:            kv.hits,
		Misses:          kv.misses,
		Evictions:       kv.evictions,
		HitRatePercent:  rate,
		PerTenantKeys:   perTenant,
		PerTenantWrites: writes,
	}
}

// ServeHTTP provides the REST endpoint handler for `/api/kv/*`.
func (kv *KVCacheLayer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Path format: /api/kv/stats or /api/kv/{namespace}/{key...}
	subpath := strings.TrimPrefix(r.URL.Path, "/api/kv")
	subpath = strings.TrimPrefix(subpath, "/")

	if subpath == "stats" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(kv.Stats())
		return
	}

	parts := strings.SplitN(subpath, "/", 2)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		http.Error(w, `{"error":"invalid kv path: expected /api/kv/{namespace}/{key}"}`, http.StatusBadRequest)
		return
	}

	namespace := parts[0]
	key := parts[1]

	switch r.Method {
	case http.MethodGet:
		val, hit, tenantID := kv.Get(namespace, key)
		if !hit {
			w.Header().Set("X-CF-KV-Cache", "MISS")
			http.Error(w, `{"error":"key not found"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("X-CF-KV-Cache", "HIT")
		w.Header().Set("X-CF-KV-Tenant", tenantID)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(val)

	case http.MethodPut, http.MethodPost:
		body, err := io.ReadAll(io.LimitReader(r.Body, 2*1024*1024))
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"failed to read payload: %s"}`, err), http.StatusBadRequest)
			return
		}

		ttl := 24 * time.Hour
		if ttlQuery := r.URL.Query().Get("ttl"); ttlQuery != "" {
			if d, err := time.ParseDuration(ttlQuery); err == nil && d > 0 {
				ttl = d
			}
		}

		tenantID, err := kv.Set(namespace, key, body, ttl)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"failed to partition key: %s"}`, err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "stored",
			"namespace": namespace,
			"key":       key,
			"tenant_id": tenantID,
			"size":      len(body),
		})

	case http.MethodDelete:
		deleted := kv.Delete(namespace, key)
		w.Header().Set("Content-Type", "application/json")
		if !deleted {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "not_found"})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
