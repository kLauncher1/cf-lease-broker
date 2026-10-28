package transcoder

import (
	"bytes"
	"compress/flate"
	"errors"
	"io"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrDictionaryNotFound = errors.New("tenant dictionary not found")
	ErrEmptyTrainingData  = errors.New("training samples slice cannot be empty")
)

// BaseJSONTokens contains ubiquitous structural tokens across Cloudflare edge API payloads.
var BaseJSONTokens = []string{
	`{"status":"ok"`,
	`"success":true`,
	`"status":"active"`,
	`"code":200`,
	`"tenant_id":`,
	`"account_id":`,
	`"timestamp":`,
	`"created_at":`,
	`"updated_at":`,
	`"metrics":{`,
	`"data":{`,
	`"items":[`,
	`"results":[`,
	`"error":null`,
	`"cf_ray":`,
	`"colo":`,
	`"latency_us":`,
	`"bandwidth_bytes":`,
	`"rate_limit":`,
	`"quota_remaining":`,
	`"lease_voucher":`,
	`"cloudflare"`,
	`"edge_worker"`,
	`"application/json"`,
}

// TenantDictionary stores pre-trained token dictionaries for a specific tenant.
type TenantDictionary struct {
	TenantID              string    `json:"tenant_id"`
	Bytes                 []byte    `json:"-"`
	Size                  int       `json:"size"`
	TrainedNGramsCount    int       `json:"trained_ngrams_count"`
	TrainedAt             time.Time `json:"trained_at"`
	HitCount              int64     `json:"hit_count"`
	TotalBytesRaw         int64     `json:"total_bytes_raw"`
	TotalBytesCompressed  int64     `json:"total_bytes_compressed"`
	TotalBytesSaved       int64     `json:"total_bytes_saved"`
	AverageSavingsPercent float64   `json:"average_savings_percent"`
}

// Stats returns a clean copy of the dictionary metrics.
func (d *TenantDictionary) Stats() DictionaryStats {
	raw := atomic.LoadInt64(&d.TotalBytesRaw)
	saved := atomic.LoadInt64(&d.TotalBytesSaved)
	hits := atomic.LoadInt64(&d.HitCount)

	var avgSavings float64
	if raw > 0 {
		avgSavings = float64(saved) / float64(raw) * 100.0
	}

	return DictionaryStats{
		TenantID:              d.TenantID,
		DictionarySize:        d.Size,
		TrainedNGramsCount:    d.TrainedNGramsCount,
		TrainedAt:             d.TrainedAt,
		HitCount:              hits,
		TotalBytesSaved:       saved,
		AverageSavingsPercent: avgSavings,
	}
}

// DictionaryManager coordinates training and caching of tenant-specific dictionaries.
type DictionaryManager struct {
	mu           sync.RWMutex
	maxDictBytes int
	dictionaries map[string]*TenantDictionary
}

// NewDictionaryManager creates a dynamic dictionary trainer and registry.
func NewDictionaryManager(maxDictBytes int) *DictionaryManager {
	if maxDictBytes <= 0 || maxDictBytes > 32768 {
		maxDictBytes = 32768 // Maximum Deflate window dictionary limit is 32 KB
	}
	dm := &DictionaryManager{
		maxDictBytes: maxDictBytes,
		dictionaries: make(map[string]*TenantDictionary),
	}

	// Pre-seed global/default tenant dictionary
	_ = dm.TrainDictionary("default", nil)
	return dm
}

// TrainDictionary extracts frequent JSON tokens and compiles a Deflate-compatible dictionary.
func (dm *DictionaryManager) TrainDictionary(tenantID string, samples [][]byte) *TenantDictionary {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if tenantID == "" {
		tenantID = "default"
	}

	ngramCounts := make(map[string]int)

	// Ingest base tokens
	for _, tok := range BaseJSONTokens {
		ngramCounts[tok] += 50
	}

	// Extract n-grams from samples
	for _, sample := range samples {
		n := len(sample)
		// Extract words and JSON key substrings between 4 and 24 bytes
		for sz := 4; sz <= 24 && sz <= n; sz += 2 {
			for i := 0; i+sz <= n; i += 3 {
				slice := string(sample[i : i+sz])
				ngramCounts[slice]++
			}
		}
	}

	type weightedToken struct {
		tok    string
		weight int
	}

	var candidates []weightedToken
	for tok, count := range ngramCounts {
		// Score = count * (len(tok) - 2)
		weight := count * (len(tok) - 2)
		if weight > 0 {
			candidates = append(candidates, weightedToken{tok: tok, weight: weight})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].weight > candidates[j].weight
	})

	var dictBuffer bytes.Buffer
	tokenCount := 0

	for _, cand := range candidates {
		if dictBuffer.Len()+len(cand.tok) > dm.maxDictBytes {
			break
		}
		dictBuffer.WriteString(cand.tok)
		tokenCount++
	}

	dictBytes := dictBuffer.Bytes()
	dict := &TenantDictionary{
		TenantID:           tenantID,
		Bytes:              dictBytes,
		Size:               len(dictBytes),
		TrainedNGramsCount: tokenCount,
		TrainedAt:          time.Now(),
	}

	dm.dictionaries[tenantID] = dict
	return dict
}

// GetDictionary returns the dictionary for tenantID or the default dictionary.
func (dm *DictionaryManager) GetDictionary(tenantID string) (*TenantDictionary, bool) {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	dict, exists := dm.dictionaries[tenantID]
	if !exists {
		dict, exists = dm.dictionaries["default"]
	}
	return dict, exists
}

// ListDictionaries returns statistics for all active tenant dictionaries.
func (dm *DictionaryManager) ListDictionaries() []DictionaryStats {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	out := make([]DictionaryStats, 0, len(dm.dictionaries))
	for _, d := range dm.dictionaries {
		out = append(out, d.Stats())
	}
	return out
}

// CompressWithDictionary compresses data using the tenant dictionary via Deflate.
func (dm *DictionaryManager) CompressWithDictionary(data []byte, dict *TenantDictionary) ([]byte, float64, error) {
	if dict == nil || len(dict.Bytes) == 0 {
		return nil, 0, ErrDictionaryNotFound
	}

	var buf bytes.Buffer
	writer, err := flate.NewWriterDict(&buf, flate.BestCompression, dict.Bytes)
	if err != nil {
		return nil, 0, err
	}

	if _, err := writer.Write(data); err != nil {
		return nil, 0, err
	}
	if err := writer.Close(); err != nil {
		return nil, 0, err
	}

	compressed := buf.Bytes()
	rawLen := int64(len(data))
	compLen := int64(len(compressed))
	saved := rawLen - compLen
	if saved < 0 {
		saved = 0
	}

	atomic.AddInt64(&dict.HitCount, 1)
	atomic.AddInt64(&dict.TotalBytesRaw, rawLen)
	atomic.AddInt64(&dict.TotalBytesCompressed, compLen)
	atomic.AddInt64(&dict.TotalBytesSaved, saved)

	savingsPercent := 0.0
	if rawLen > 0 {
		savingsPercent = float64(saved) / float64(rawLen) * 100.0
	}

	return compressed, savingsPercent, nil
}

// DecompressWithDictionary decompresses dictionary-compressed Deflate payloads.
func (dm *DictionaryManager) DecompressWithDictionary(compressed []byte, dict *TenantDictionary) ([]byte, error) {
	if dict == nil || len(dict.Bytes) == 0 {
		return nil, ErrDictionaryNotFound
	}

	reader := flate.NewReaderDict(bytes.NewReader(compressed), dict.Bytes)
	defer reader.Close()

	var out bytes.Buffer
	if _, err := io.Copy(&out, reader); err != nil {
		return nil, err
	}

	return out.Bytes(), nil
}
