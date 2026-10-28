package transcoder

import (
	"time"
)

// CompressionAlgorithm defines supported edge compression encodings.
type CompressionAlgorithm string

const (
	AlgoGzip     CompressionAlgorithm = "gzip"
	AlgoBrotli   CompressionAlgorithm = "br"
	AlgoZstd     CompressionAlgorithm = "zstd"
	AlgoDeflate  CompressionAlgorithm = "deflate"
	AlgoIdentity CompressionAlgorithm = "identity"
)

// TranscodeDecision represents the result of client header inspection and algorithm selection.
type TranscodeDecision struct {
	Algorithm       CompressionAlgorithm `json:"algorithm"`
	Level           int                  `json:"level"`
	UseDictionary   bool                 `json:"use_dictionary"`
	DictionaryID    string               `json:"dictionary_id,omitempty"`
	SkipReason      string               `json:"skip_reason,omitempty"`
	ContentEncoding string               `json:"content_encoding"`
	ShouldCompress  bool                 `json:"should_compress"`
}

// TranscoderConfig configures compression levels, algorithm priorities, and thresholds.
type TranscoderConfig struct {
	MinSizeBytes               int                    `json:"min_size_bytes"`
	DefaultAlgorithm           CompressionAlgorithm   `json:"default_algorithm"`
	PreferredAlgorithms        []CompressionAlgorithm `json:"preferred_algorithms"`
	CompressibleTypes          []string               `json:"compressible_types"`
	GzipLevel                  int                    `json:"gzip_level"`
	BrotliLevel                int                    `json:"brotli_level"`
	ZstdLevel                  int                    `json:"zstd_level"`
	EnableDictionaryTraining   bool                   `json:"enable_dictionary_training"`
	MaxDictionarySizeBytes     int                    `json:"max_dictionary_size_bytes"`
	BufferPoolSmallSize        int                    `json:"buffer_pool_small_size"`
	BufferPoolMediumSize       int                    `json:"buffer_pool_medium_size"`
	BufferPoolLargeSize        int                    `json:"buffer_pool_large_size"`
}

// DefaultTranscoderConfig returns production defaults for edge transcoding.
func DefaultTranscoderConfig() TranscoderConfig {
	return TranscoderConfig{
		MinSizeBytes:             256,
		DefaultAlgorithm:         AlgoZstd,
		PreferredAlgorithms:      []CompressionAlgorithm{AlgoZstd, AlgoBrotli, AlgoGzip, AlgoDeflate},
		CompressibleTypes: []string{
			"application/json",
			"application/javascript",
			"text/html",
			"text/plain",
			"text/css",
			"application/xml",
			"text/xml",
			"application/wasm",
		},
		GzipLevel:                6,
		BrotliLevel:              6,
		ZstdLevel:                3,
		EnableDictionaryTraining: true,
		MaxDictionarySizeBytes:   32768, // 32 KB standard dictionary ceiling
		BufferPoolSmallSize:      4096,  // 4 KB
		BufferPoolMediumSize:     65536, // 64 KB
		BufferPoolLargeSize:      262144,// 256 KB
	}
}

// TranscoderStats provides real-time operational telemetry for edge compression.
type TranscoderStats struct {
	TotalBytesRaw                 int64                           `json:"total_bytes_raw"`
	TotalBytesCompressed          int64                           `json:"total_bytes_compressed"`
	TotalTranscodes               int64                           `json:"total_transcodes"`
	TotalZeroCopyStreamingPasses  int64                           `json:"total_zero_copy_streaming_passes"`
	SavingsRatio                  float64                         `json:"savings_ratio"`
	SavingsPercent                float64                         `json:"savings_percent"`
	AlgorithmCounts               map[CompressionAlgorithm]int64 `json:"algorithm_counts"`
	AverageLatencyUs              float64                         `json:"average_latency_us"`
	BufferPoolHitRate             float64                         `json:"buffer_pool_hit_rate"`
	ActiveDictionariesCount       int                             `json:"active_dictionaries_count"`
}

// DictionaryStats summarizes dynamic dictionary performance for tenant JSON payloads.
type DictionaryStats struct {
	TenantID              string    `json:"tenant_id"`
	DictionarySize        int       `json:"dictionary_size"`
	TrainedNGramsCount    int       `json:"trained_ngrams_count"`
	TrainedAt             time.Time `json:"trained_at"`
	HitCount              int64     `json:"hit_count"`
	TotalBytesSaved       int64     `json:"total_bytes_saved"`
	AverageSavingsPercent float64   `json:"average_savings_percent"`
}
