package transcoder

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestTranscoder_AcceptEncodingNegotiationQualityFactors(t *testing.T) {
	cfg := DefaultTranscoderConfig()
	eng := NewTranscoderEngine(cfg)

	tests := []struct {
		name           string
		acceptEncoding string
		expectedAlgo   CompressionAlgorithm
	}{
		{
			name:           "Standard browser header with zstd preference",
			acceptEncoding: "gzip, deflate, br;q=0.8, zstd;q=1.0",
			expectedAlgo:   AlgoZstd,
		},
		{
			name:           "Brotli higher quality factor than gzip",
			acceptEncoding: "gzip;q=0.5, br;q=0.9",
			expectedAlgo:   AlgoBrotli,
		},
		{
			name:           "Tie break selects zstd over brotli",
			acceptEncoding: "br;q=0.9, zstd;q=0.9",
			expectedAlgo:   AlgoZstd,
		},
		{
			name:           "Zstd rejected with q=0 selects brotli",
			acceptEncoding: "zstd;q=0, br;q=0.9, gzip;q=0.8",
			expectedAlgo:   AlgoBrotli,
		},
		{
			name:           "Wildcard selects default engine algorithm",
			acceptEncoding: "*",
			expectedAlgo:   AlgoZstd,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decision := eng.InspectPayload(tc.acceptEncoding, "application/json", 1024)
			if !decision.ShouldCompress {
				t.Fatalf("expected ShouldCompress to be true")
			}
			if decision.Algorithm != tc.expectedAlgo {
				t.Errorf("expected algorithm %s, got %s", tc.expectedAlgo, decision.Algorithm)
			}
		})
	}
}

func TestTranscoder_ContentFilterAndMinSizeBypass(t *testing.T) {
	cfg := DefaultTranscoderConfig()
	eng := NewTranscoderEngine(cfg)

	// 1. Uncompressible media type
	dec1 := eng.InspectPayload("gzip, br, zstd", "image/png", 50000)
	if dec1.ShouldCompress {
		t.Errorf("expected image/png to be skipped")
	}
	if dec1.SkipReason != "uncompressible_content_type" {
		t.Errorf("unexpected skip reason: %s", dec1.SkipReason)
	}

	// 2. Below minimum size threshold (256 bytes)
	dec2 := eng.InspectPayload("gzip, br, zstd", "application/json", 128)
	if dec2.ShouldCompress {
		t.Errorf("expected 128 bytes payload to be skipped")
	}
	if dec2.SkipReason != "payload_below_min_size" {
		t.Errorf("unexpected skip reason: %s", dec2.SkipReason)
	}

	// 3. Compressible JSON above threshold
	dec3 := eng.InspectPayload("gzip, br, zstd", "application/json", 1024)
	if !dec3.ShouldCompress {
		t.Errorf("expected 1024 bytes JSON payload to be compressed")
	}
}

func TestTranscoder_BrotliFrameEncodeDecode(t *testing.T) {
	// 1. Empty payload
	emptyEnc, err := EncodeBrotli(nil, 6)
	if err != nil {
		t.Fatalf("EncodeBrotli empty failed: %v", err)
	}
	emptyDec, err := DecodeBrotli(emptyEnc)
	if err != nil {
		t.Fatalf("DecodeBrotli empty failed: %v", err)
	}
	if len(emptyDec) != 0 {
		t.Errorf("expected empty payload, got %d bytes", len(emptyDec))
	}

	// 2. Small payload
	src := []byte(`{"message":"hello brotli edge transcoder","timestamp":"2026-10-28T11:00:00Z"}`)
	enc, err := EncodeBrotli(src, 6)
	if err != nil {
		t.Fatalf("EncodeBrotli failed: %v", err)
	}
	dec, err := DecodeBrotli(enc)
	if err != nil {
		t.Fatalf("DecodeBrotli failed: %v", err)
	}
	if !bytes.Equal(src, dec) {
		t.Errorf("roundtrip mismatch: got %q, want %q", dec, src)
	}

	// 3. Large payload crossing 64KB multi-chunk boundary
	largeSrc := make([]byte, 100000)
	for i := range largeSrc {
		largeSrc[i] = byte(i % 251)
	}
	largeEnc, err := EncodeBrotli(largeSrc, 6)
	if err != nil {
		t.Fatalf("EncodeBrotli large failed: %v", err)
	}
	largeDec, err := DecodeBrotli(largeEnc)
	if err != nil {
		t.Fatalf("DecodeBrotli large failed: %v", err)
	}
	if !bytes.Equal(largeSrc, largeDec) {
		t.Errorf("large roundtrip mismatch: len got %d, want %d", len(largeDec), len(largeSrc))
	}

	// 4. Malformed stream detection
	corrupted := append([]byte{}, enc...)
	corrupted[0] = 0xFF // corrupt header
	if _, err := DecodeBrotli(corrupted); err == nil {
		t.Errorf("expected error on corrupted brotli stream")
	}
}

func TestTranscoder_ZstandardFrameEncodeDecode(t *testing.T) {
	// 1. Magic number and header validation
	src := []byte("Cloudflare Lease Broker Zstandard Transcoder Edge Pipeline")
	enc, err := EncodeZstd(src, 3)
	if err != nil {
		t.Fatalf("EncodeZstd failed: %v", err)
	}
	if !bytes.Equal(enc[:4], ZstdMagic) {
		t.Fatalf("Zstd frame missing magic number: %x", enc[:4])
	}

	dec, err := DecodeZstd(enc)
	if err != nil {
		t.Fatalf("DecodeZstd failed: %v", err)
	}
	if !bytes.Equal(src, dec) {
		t.Errorf("roundtrip mismatch: got %q, want %q", dec, src)
	}

	// 2. RLE block test (high repetition)
	rleSrc := bytes.Repeat([]byte("A"), 5000)
	rleEnc, err := EncodeZstd(rleSrc, 3)
	if err != nil {
		t.Fatalf("EncodeZstd RLE failed: %v", err)
	}
	// RLE block should compress 5000 bytes into < 30 bytes
	if len(rleEnc) > 50 {
		t.Errorf("expected high RLE compression (<50 bytes), got %d bytes", len(rleEnc))
	}
	rleDec, err := DecodeZstd(rleEnc)
	if err != nil {
		t.Fatalf("DecodeZstd RLE failed: %v", err)
	}
	if !bytes.Equal(rleSrc, rleDec) {
		t.Errorf("RLE roundtrip mismatch")
	}

	// 3. Multi-block test exceeding 128KB block size
	multiSrc := make([]byte, 160000)
	_, _ = rand.Read(multiSrc)
	multiEnc, err := EncodeZstd(multiSrc, 3)
	if err != nil {
		t.Fatalf("EncodeZstd multi-block failed: %v", err)
	}
	multiDec, err := DecodeZstd(multiEnc)
	if err != nil {
		t.Fatalf("DecodeZstd multi-block failed: %v", err)
	}
	if !bytes.Equal(multiSrc, multiDec) {
		t.Errorf("multi-block roundtrip mismatch")
	}

	// 4. Checksum mismatch detection
	tampered := append([]byte{}, enc...)
	tampered[len(tampered)-1] ^= 0x55 // corrupt checksum byte
	if _, err := DecodeZstd(tampered); err == nil {
		t.Errorf("expected checksum mismatch error on tampered frame")
	}
}

func TestTranscoder_GzipAndDeflateRoundTrip(t *testing.T) {
	cfg := DefaultTranscoderConfig()
	eng := NewTranscoderEngine(cfg)

	payload := []byte(strings.Repeat(`{"service":"edge-worker","status":"ok","latency_ms":12.5},`, 50))

	// Gzip
	decGzip := TranscodeDecision{Algorithm: AlgoGzip, Level: 6, ShouldCompress: true}
	gzComp, err := eng.TranscodeBytes(payload, decGzip, "tenant-1")
	if err != nil {
		t.Fatalf("gzip transcode failed: %v", err)
	}
	if len(gzComp) >= len(payload) {
		t.Errorf("expected gzip compression to reduce size: %d vs %d", len(gzComp), len(payload))
	}
	gzDecomp, err := eng.DecompressBytes(gzComp, AlgoGzip, "tenant-1")
	if err != nil {
		t.Fatalf("gzip decompress failed: %v", err)
	}
	if !bytes.Equal(payload, gzDecomp) {
		t.Errorf("gzip payload mismatch")
	}

	// Deflate
	decDeflate := TranscodeDecision{Algorithm: AlgoDeflate, Level: 6, ShouldCompress: true}
	dfComp, err := eng.TranscodeBytes(payload, decDeflate, "tenant-1")
	if err != nil {
		t.Fatalf("deflate transcode failed: %v", err)
	}
	dfDecomp, err := eng.DecompressBytes(dfComp, AlgoDeflate, "tenant-1")
	if err != nil {
		t.Fatalf("deflate decompress failed: %v", err)
	}
	if !bytes.Equal(payload, dfDecomp) {
		t.Errorf("deflate payload mismatch")
	}
}

func TestTranscoder_BufferPoolZeroAllocation(t *testing.T) {
	bp := NewBufferPool(4096, 65536, 262144)

	// Small buffer
	bSmall := bp.Acquire(2048)
	if bSmall.Capacity < 4096 {
		t.Errorf("expected small buffer capacity >= 4096, got %d", bSmall.Capacity)
	}
	_, _ = bSmall.Write([]byte("test"))
	bp.Release(bSmall)

	// Medium buffer
	bMed := bp.Acquire(32000)
	if bMed.Capacity < 65536 {
		t.Errorf("expected medium buffer capacity >= 65536, got %d", bMed.Capacity)
	}
	bp.Release(bMed)

	// Large buffer
	bLrg := bp.Acquire(150000)
	if bLrg.Capacity < 262144 {
		t.Errorf("expected large buffer capacity >= 262144, got %d", bLrg.Capacity)
	}
	bp.Release(bLrg)

	acquires, releases, hitRate := bp.Stats()
	if acquires != 3 || releases != 3 {
		t.Errorf("expected 3 acquires/releases, got acq=%d, rel=%d", acquires, releases)
	}
	if hitRate < 100.0 {
		t.Errorf("expected 100%% pool hit rate, got %.2f%%", hitRate)
	}
}

func TestTranscoder_DynamicDictionaryTraining(t *testing.T) {
	cfg := DefaultTranscoderConfig()
	eng := NewTranscoderEngine(cfg)

	// Generate 15 representative tenant API JSON samples
	var samples [][]byte
	for i := 0; i < 15; i++ {
		s := fmt.Sprintf(`{"tenant_id":"tenant-alpha","account_id":"acc-%03d","status":"active","metrics":{"cpu":0.15,"memory":256,"bandwidth_bytes":%d},"created_at":"2026-10-28T11:00:00Z","cf_ray":"ray-%08x","colo":"SFO","error":null}`, i, 10000+i*500, i*123456)
		samples = append(samples, []byte(s))
	}

	dict := eng.DictionaryManager().TrainDictionary("tenant-alpha", samples)
	if dict.Size == 0 || dict.TrainedNGramsCount == 0 {
		t.Fatalf("expected non-empty trained dictionary")
	}

	// Test compression of a new sample using the dictionary
	target := []byte(`{"tenant_id":"tenant-alpha","account_id":"acc-999","status":"active","metrics":{"cpu":0.20,"memory":512,"bandwidth_bytes":45000},"created_at":"2026-10-28T11:05:00Z","cf_ray":"ray-a1b2c3d4","colo":"SFO","error":null}`)
	rawLen := len(target)

	compressed, savingsPercent, err := eng.DictionaryManager().CompressWithDictionary(target, dict)
	if err != nil {
		t.Fatalf("CompressWithDictionary failed: %v", err)
	}

	// Verify bandwidth reduction: target JSON should compress by > 65% up to 85%
	if savingsPercent < 60.0 {
		t.Errorf("expected at least 60%% bandwidth savings with dynamic dictionary, got %.2f%% (raw=%d, comp=%d)", savingsPercent, rawLen, len(compressed))
	}

	// Decompress and verify lossless byte fidelity
	decomp, err := eng.DictionaryManager().DecompressWithDictionary(compressed, dict)
	if err != nil {
		t.Fatalf("DecompressWithDictionary failed: %v", err)
	}
	if !bytes.Equal(target, decomp) {
		t.Errorf("dictionary decompression mismatch: got %s, want %s", string(decomp), string(target))
	}
}

func TestTranscoder_ZeroCopyStreamingPipeline(t *testing.T) {
	cfg := DefaultTranscoderConfig()
	eng := NewTranscoderEngine(cfg)

	data := []byte(strings.Repeat(`{"route":"/api/edge/v1","method":"GET","duration_ms":1.24,"cache_status":"HIT"},`, 200))
	reader := bytes.NewReader(data)
	var out bytes.Buffer

	decision := TranscodeDecision{
		Algorithm:      AlgoZstd,
		Level:          3,
		ShouldCompress: true,
	}

	bytesIn, bytesOut, err := eng.TranscodeStream(reader, &out, decision, "tenant-stream")
	if err != nil {
		t.Fatalf("TranscodeStream failed: %v", err)
	}
	if bytesIn != int64(len(data)) {
		t.Errorf("expected bytesIn=%d, got %d", len(data), bytesIn)
	}
	if bytesOut != int64(out.Len()) {
		t.Errorf("expected bytesOut=%d, got %d", out.Len(), bytesOut)
	}

	// Decompress and verify
	decomp, err := DecodeZstd(out.Bytes())
	if err != nil {
		t.Fatalf("DecodeZstd on stream failed: %v", err)
	}
	if !bytes.Equal(data, decomp) {
		t.Errorf("stream transcode payload mismatch")
	}
}

func TestTranscoder_RESTEndpoints(t *testing.T) {
	cfg := DefaultTranscoderConfig()
	eng := NewTranscoderEngine(cfg)
	handler := NewTranscoderHandler(eng)

	// 1. GET /api/transcoder/status
	req := httptest.NewRequest(http.MethodGet, "/api/transcoder/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for /status, got %d", rec.Code)
	}

	// 2. POST /api/transcoder/inspect
	inspectBody := `{"accept_encoding":"br, zstd;q=0.9, gzip","content_type":"application/json","content_length":1500}`
	req = httptest.NewRequest(http.MethodPost, "/api/transcoder/inspect", strings.NewReader(inspectBody))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for /inspect, got %d", rec.Code)
	}
	var dec TranscodeDecision
	_ = json.NewDecoder(rec.Body).Decode(&dec)
	if dec.Algorithm != AlgoBrotli {
		t.Errorf("expected inspect to choose br, got %s", dec.Algorithm)
	}

	// 3. POST /api/transcoder/compress?algo=zstd
	payload := `{"test":"api compression pipeline","tenant":"tenant-edge"}`
	req = httptest.NewRequest(http.MethodPost, "/api/transcoder/compress?algo=zstd", strings.NewReader(payload))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for /compress, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Encoding") != "zstd" {
		t.Errorf("missing or incorrect Content-Encoding: %s", rec.Header().Get("Content-Encoding"))
	}
	compBytes := rec.Body.Bytes()

	// 4. POST /api/transcoder/decompress?algo=zstd
	req = httptest.NewRequest(http.MethodPost, "/api/transcoder/decompress?algo=zstd", bytes.NewReader(compBytes))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for /decompress, got %d", rec.Code)
	}
	if rec.Body.String() != payload {
		t.Errorf("decompression mismatch: got %q, want %q", rec.Body.String(), payload)
	}

	// 5. GET /api/transcoder/dictionary
	req = httptest.NewRequest(http.MethodGet, "/api/transcoder/dictionary", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for /dictionary, got %d", rec.Code)
	}

	// 6. POST /api/transcoder/dictionary/train
	trainBody := `{"tenant_id":"tenant-test","samples":["{\"foo\":\"bar\"}","{\"foo\":\"baz\"}"]}`
	req = httptest.NewRequest(http.MethodPost, "/api/transcoder/dictionary/train", strings.NewReader(trainBody))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for /dictionary/train, got %d", rec.Code)
	}

	// 7. GET /api/transcoder/stats & POST /api/transcoder/reset
	req = httptest.NewRequest(http.MethodGet, "/api/transcoder/stats", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for /stats, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/transcoder/reset", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for /reset, got %d", rec.Code)
	}
}

func TestTranscoder_BodySizeBoundingAndDoS(t *testing.T) {
	cfg := DefaultTranscoderConfig()
	eng := NewTranscoderEngine(cfg)
	handler := NewTranscoderHandler(eng)

	// Oversized payload on dictionary training (>64KB limit)
	hugeBody := `{"tenant_id":"huge","samples":["` + strings.Repeat("A", 128*1024) + `"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/transcoder/dictionary/train", strings.NewReader(hugeBody))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for oversized training payload, got %d", rec.Code)
	}
}

func TestTranscoder_HighConcurrency40Goroutines(t *testing.T) {
	cfg := DefaultTranscoderConfig()
	eng := NewTranscoderEngine(cfg)

	var wg sync.WaitGroup
	workers := 40
	iterations := 25

	sample := []byte(`{"action":"concurrent_compression_stress","timestamp":1700000000,"status":"ok"}`)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			algo := AlgoZstd
			if workerID%3 == 1 {
				algo = AlgoBrotli
			} else if workerID%3 == 2 {
				algo = AlgoGzip
			}

			dec := TranscodeDecision{
				Algorithm:      algo,
				Level:          3,
				ShouldCompress: true,
			}

			for j := 0; j < iterations; j++ {
				comp, err := eng.TranscodeBytes(sample, dec, "tenant-concurrent")
				if err != nil {
					t.Errorf("worker %d iteration %d compress error: %v", workerID, j, err)
					return
				}
				decomp, err := eng.DecompressBytes(comp, algo, "tenant-concurrent")
				if err != nil {
					t.Errorf("worker %d iteration %d decompress error: %v", workerID, j, err)
					return
				}
				if !bytes.Equal(sample, decomp) {
					t.Errorf("worker %d iteration %d data mismatch", workerID, j)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	stats := eng.GetStats()
	expectedTranscodes := int64(workers * iterations)
	if stats.TotalTranscodes != expectedTranscodes {
		t.Errorf("expected %d transcodes in stats, got %d", expectedTranscodes, stats.TotalTranscodes)
	}
}
