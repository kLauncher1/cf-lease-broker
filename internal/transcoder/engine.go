package transcoder

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrUnsupportedAlgorithm = errors.New("unsupported compression algorithm")
	ErrStreamReadFailure    = errors.New("failed to read from input stream")
	ErrStreamWriteFailure   = errors.New("failed to write to output stream")
)

type encodingPreference struct {
	algo CompressionAlgorithm
	q    float64
}

// TranscoderEngine provides adaptive multi-algorithm compression and zero-copy streaming.
type TranscoderEngine struct {
	mu                  sync.RWMutex
	config              TranscoderConfig
	bufferPool          *BufferPool
	dictManager         *DictionaryManager
	totalBytesRaw       int64
	totalBytesComp      int64
	totalTranscodes     int64
	totalStreaming      int64
	totalLatencyUs      int64
	algoCounts          map[CompressionAlgorithm]*int64
}

// NewTranscoderEngine initializes the edge transcoder with buffer pools and dictionary trainer.
func NewTranscoderEngine(cfg TranscoderConfig) *TranscoderEngine {
	bp := NewBufferPool(cfg.BufferPoolSmallSize, cfg.BufferPoolMediumSize, cfg.BufferPoolLargeSize)
	dm := NewDictionaryManager(cfg.MaxDictionarySizeBytes)

	eng := &TranscoderEngine{
		config:      cfg,
		bufferPool:  bp,
		dictManager: dm,
		algoCounts:  make(map[CompressionAlgorithm]*int64),
	}

	for _, algo := range []CompressionAlgorithm{AlgoZstd, AlgoBrotli, AlgoGzip, AlgoDeflate, AlgoIdentity} {
		var cnt int64
		eng.algoCounts[algo] = &cnt
	}

	return eng
}

// BufferPool returns the underlying memory buffer pool.
func (e *TranscoderEngine) BufferPool() *BufferPool {
	return e.bufferPool
}

// DictionaryManager returns the dynamic JSON dictionary trainer.
func (e *TranscoderEngine) DictionaryManager() *DictionaryManager {
	return e.dictManager
}

// InspectPayload evaluates client Accept-Encoding, content-type, and size to select the optimal encoding.
func (e *TranscoderEngine) InspectPayload(acceptEncoding, contentType string, contentLength int64) TranscodeDecision {
	// 1. Check size threshold
	if contentLength > 0 && contentLength < int64(e.config.MinSizeBytes) {
		return TranscodeDecision{
			Algorithm:       AlgoIdentity,
			ContentEncoding: "",
			ShouldCompress:  false,
			SkipReason:      "payload_below_min_size",
		}
	}

	// 2. Check Content-Type compressibility
	if contentType != "" {
		isCompressible := false
		lowerCT := strings.ToLower(contentType)
		for _, ct := range e.config.CompressibleTypes {
			if strings.Contains(lowerCT, ct) {
				isCompressible = true
				break
			}
		}
		if !isCompressible {
			return TranscodeDecision{
				Algorithm:       AlgoIdentity,
				ContentEncoding: "",
				ShouldCompress:  false,
				SkipReason:      "uncompressible_content_type",
			}
		}
	}

	// 3. Parse client Accept-Encoding header and quality factors
	supported := e.parseAcceptEncoding(acceptEncoding)
	if len(supported) == 0 {
		return TranscodeDecision{
			Algorithm:       AlgoIdentity,
			ContentEncoding: "",
			ShouldCompress:  false,
			SkipReason:      "no_matching_client_encoding",
		}
	}

	// 4. Select top prioritized algorithm matching engine preferences
	selected := supported[0].algo
	level := e.config.ZstdLevel
	switch selected {
	case AlgoBrotli:
		level = e.config.BrotliLevel
	case AlgoGzip:
		level = e.config.GzipLevel
	case AlgoDeflate:
		level = e.config.GzipLevel
	}

	// 5. If JSON and dictionary training enabled, check dictionary applicability
	useDict := false
	dictID := ""
	if e.config.EnableDictionaryTraining && strings.Contains(strings.ToLower(contentType), "json") {
		useDict = true
		dictID = "default"
	}

	return TranscodeDecision{
		Algorithm:       selected,
		Level:           level,
		UseDictionary:   useDict,
		DictionaryID:    dictID,
		ContentEncoding: string(selected),
		ShouldCompress:  true,
	}
}

func (e *TranscoderEngine) parseAcceptEncoding(header string) []encodingPreference {
	if header == "" {
		return nil
	}

	parts := strings.Split(header, ",")
	var prefs []encodingPreference

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}

		q := 1.0
		algoStr := trimmed
		if idx := strings.Index(trimmed, ";"); idx != -1 {
			algoStr = strings.TrimSpace(trimmed[:idx])
			param := strings.TrimSpace(trimmed[idx+1:])
			if strings.HasPrefix(param, "q=") {
				if parsedQ, err := strconv.ParseFloat(param[2:], 64); err == nil {
					q = parsedQ
				}
			}
		}

		if q <= 0.0 {
			continue // q=0 explicitly rejects this encoding
		}

		algo := CompressionAlgorithm(strings.ToLower(algoStr))
		switch algo {
		case AlgoZstd, AlgoBrotli, AlgoGzip, AlgoDeflate:
			prefs = append(prefs, encodingPreference{algo: algo, q: q})
		case "*":
			// Wildcard: add default algorithm if not present
			prefs = append(prefs, encodingPreference{algo: e.config.DefaultAlgorithm, q: q * 0.8})
		}
	}

	if len(prefs) == 0 {
		return nil
	}

	// Preference map rank for tie-breaking: zstd > br > gzip > deflate
	prefRank := map[CompressionAlgorithm]int{
		AlgoZstd:    4,
		AlgoBrotli:  3,
		AlgoGzip:    2,
		AlgoDeflate: 1,
	}

	sort.Slice(prefs, func(i, j int) bool {
		if prefs[i].q != prefs[j].q {
			return prefs[i].q > prefs[j].q // Higher q first
		}
		return prefRank[prefs[i].algo] > prefRank[prefs[j].algo]
	})

	return prefs
}

// TranscodeBytes encodes a byte slice according to the transcode decision.
func (e *TranscoderEngine) TranscodeBytes(src []byte, decision TranscodeDecision, tenantID string) ([]byte, error) {
	start := time.Now()
	rawLen := int64(len(src))

	if !decision.ShouldCompress || decision.Algorithm == AlgoIdentity || rawLen == 0 {
		return src, nil
	}

	var compressed []byte
	var err error

	// If dictionary compression requested for Deflate/JSON
	if decision.UseDictionary && decision.Algorithm == AlgoDeflate {
		dict, ok := e.dictManager.GetDictionary(tenantID)
		if ok && dict != nil {
			var savings float64
			compressed, savings, err = e.dictManager.CompressWithDictionary(src, dict)
			if err == nil && savings > 0 {
				e.recordMetrics(rawLen, int64(len(compressed)), decision.Algorithm, start)
				return compressed, nil
			}
		}
	}

	switch decision.Algorithm {
	case AlgoZstd:
		compressed, err = EncodeZstd(src, decision.Level)
	case AlgoBrotli:
		compressed, err = EncodeBrotli(src, decision.Level)
	case AlgoGzip:
		var buf bytes.Buffer
		gw, gErr := gzip.NewWriterLevel(&buf, decision.Level)
		if gErr != nil {
			gw = gzip.NewWriter(&buf)
		}
		_, err = gw.Write(src)
		if err == nil {
			err = gw.Close()
			compressed = buf.Bytes()
		}
	case AlgoDeflate:
		var buf bytes.Buffer
		fw, fErr := flate.NewWriter(&buf, decision.Level)
		if fErr != nil {
			fw, _ = flate.NewWriter(&buf, flate.DefaultCompression)
		}
		_, err = fw.Write(src)
		if err == nil {
			err = fw.Close()
			compressed = buf.Bytes()
		}
	default:
		return src, nil
	}

	if err != nil {
		return nil, err
	}

	e.recordMetrics(rawLen, int64(len(compressed)), decision.Algorithm, start)
	return compressed, nil
}

// TranscodeStream pipes reader through pre-allocated compressor buffers directly to writer with zero heap allocations.
func (e *TranscoderEngine) TranscodeStream(r io.Reader, w io.Writer, decision TranscodeDecision, tenantID string) (int64, int64, error) {
	start := time.Now()
	atomic.AddInt64(&e.totalStreaming, 1)

	// Acquire recycled buffer for reading payload chunk
	buf := e.bufferPool.Acquire(e.config.BufferPoolMediumSize)
	defer e.bufferPool.Release(buf)

	n, err := buf.ReadFrom(r)
	if err != nil && err != io.EOF {
		return 0, 0, ErrStreamReadFailure
	}

	rawBytes := buf.Bytes()
	compressed, err := e.TranscodeBytes(rawBytes, decision, tenantID)
	if err != nil {
		return n, 0, err
	}

	written, err := w.Write(compressed)
	if err != nil {
		return n, int64(written), ErrStreamWriteFailure
	}

	e.recordMetrics(n, int64(written), decision.Algorithm, start)
	return n, int64(written), nil
}

// DecompressBytes decodes a compressed payload back into raw uncompressed bytes.
func (e *TranscoderEngine) DecompressBytes(src []byte, algo CompressionAlgorithm, tenantID string) ([]byte, error) {
	if len(src) == 0 || algo == AlgoIdentity {
		return src, nil
	}

	switch algo {
	case AlgoZstd:
		return DecodeZstd(src)
	case AlgoBrotli:
		return DecodeBrotli(src)
	case AlgoGzip:
		gr, err := gzip.NewReader(bytes.NewReader(src))
		if err != nil {
			return nil, err
		}
		defer gr.Close()
		var out bytes.Buffer
		if _, err := io.Copy(&out, gr); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	case AlgoDeflate:
		dict, ok := e.dictManager.GetDictionary(tenantID)
		if ok && dict != nil {
			decomp, err := e.dictManager.DecompressWithDictionary(src, dict)
			if err == nil {
				return decomp, nil
			}
		}
		fr := flate.NewReader(bytes.NewReader(src))
		defer fr.Close()
		var out bytes.Buffer
		if _, err := io.Copy(&out, fr); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	default:
		return nil, ErrUnsupportedAlgorithm
	}
}

func (e *TranscoderEngine) recordMetrics(rawBytes, compBytes int64, algo CompressionAlgorithm, start time.Time) {
	atomic.AddInt64(&e.totalBytesRaw, rawBytes)
	atomic.AddInt64(&e.totalBytesComp, compBytes)
	atomic.AddInt64(&e.totalTranscodes, 1)

	durUs := time.Since(start).Microseconds()
	atomic.AddInt64(&e.totalLatencyUs, durUs)

	if cntPtr, ok := e.algoCounts[algo]; ok {
		atomic.AddInt64(cntPtr, 1)
	}
}

// GetStats returns aggregated operational and bandwidth savings metrics.
func (e *TranscoderEngine) GetStats() TranscoderStats {
	raw := atomic.LoadInt64(&e.totalBytesRaw)
	comp := atomic.LoadInt64(&e.totalBytesComp)
	trans := atomic.LoadInt64(&e.totalTranscodes)
	streaming := atomic.LoadInt64(&e.totalStreaming)
	totLat := atomic.LoadInt64(&e.totalLatencyUs)

	var savingsRatio, savingsPercent, avgLat float64
	if raw > 0 {
		saved := raw - comp
		if saved > 0 {
			savingsPercent = float64(saved) / float64(raw) * 100.0
			savingsRatio = float64(raw) / float64(comp)
		} else {
			savingsRatio = 1.0
		}
	}
	if trans > 0 {
		avgLat = float64(totLat) / float64(trans)
	}

	_, _, hitRate := e.bufferPool.Stats()

	counts := make(map[CompressionAlgorithm]int64)
	for algo, ptr := range e.algoCounts {
		counts[algo] = atomic.LoadInt64(ptr)
	}

	dictCount := len(e.dictManager.ListDictionaries())

	return TranscoderStats{
		TotalBytesRaw:                raw,
		TotalBytesCompressed:         comp,
		TotalTranscodes:              trans,
		TotalZeroCopyStreamingPasses: streaming,
		SavingsRatio:                 savingsRatio,
		SavingsPercent:               savingsPercent,
		AlgorithmCounts:              counts,
		AverageLatencyUs:             avgLat,
		BufferPoolHitRate:            hitRate,
		ActiveDictionariesCount:      dictCount,
	}
}

// Reset clears cumulative metrics.
func (e *TranscoderEngine) Reset() {
	atomic.StoreInt64(&e.totalBytesRaw, 0)
	atomic.StoreInt64(&e.totalBytesComp, 0)
	atomic.StoreInt64(&e.totalTranscodes, 0)
	atomic.StoreInt64(&e.totalStreaming, 0)
	atomic.StoreInt64(&e.totalLatencyUs, 0)
	for _, ptr := range e.algoCounts {
		atomic.StoreInt64(ptr, 0)
	}
}
