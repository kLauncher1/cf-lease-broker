package compiler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	// DefaultBufferSize is 64KB, optimal for high-throughput zero-copy edge proxy streams.
	DefaultBufferSize = 64 * 1024
)

var (
	ErrClientDisconnected = errors.New("client disconnected during stream")
)

// StreamMetrics captures telemetry data from a completed or aborted streaming transfer.
type StreamMetrics struct {
	BytesWritten  int64         `json:"bytes_written"`
	ChunksWritten int64         `json:"chunks_written"`
	Duration      time.Duration `json:"duration"`
	DurationMs    int64         `json:"duration_ms"`
	ThroughputMBs float64       `json:"throughput_mb_s"`
	Completed     bool          `json:"completed"`
}

// StreamPipe orchestrates zero-copy response streaming with reusable buffer pools.
type StreamPipe struct {
	pool       sync.Pool
	bufferSize int
}

// NewStreamPipe initializes a StreamPipe with pooled byte buffers.
func NewStreamPipe(bufferSize int) *StreamPipe {
	if bufferSize <= 0 {
		bufferSize = DefaultBufferSize
	}
	return &StreamPipe{
		bufferSize: bufferSize,
		pool: sync.Pool{
			New: func() any {
				b := make([]byte, bufferSize)
				return &b
			},
		},
	}
}

// AcquireBuffer gets a pre-allocated buffer from the pool.
func (p *StreamPipe) AcquireBuffer() *[]byte {
	return p.pool.Get().(*[]byte)
}

// ReleaseBuffer returns a buffer back to the pool.
func (p *StreamPipe) ReleaseBuffer(buf *[]byte) {
	if buf != nil && len(*buf) == p.bufferSize {
		p.pool.Put(buf)
	}
}

// PipeStream copies data from src to the HTTP response writer in zero-copy chunks, flushing each.
func (p *StreamPipe) PipeStream(ctx context.Context, w http.ResponseWriter, src io.Reader, contentType string, contentLength int64) (*StreamMetrics, error) {
	bufPtr := p.AcquireBuffer()
	defer p.ReleaseBuffer(bufPtr)
	buf := *bufPtr

	// Set streaming headers
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Cache-Control", "no-cache, no-transform")

	if contentLength > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
	} else {
		// Use chunked transfer encoding when size is unknown or streaming
		w.Header().Set("Transfer-Encoding", "chunked")
	}

	flusher, hasFlusher := w.(http.Flusher)
	start := time.Now()
	var totalBytes int64
	var totalChunks int64

	for {
		select {
		case <-ctx.Done():
			elapsed := time.Since(start)
			return &StreamMetrics{
				BytesWritten:  totalBytes,
				ChunksWritten: totalChunks,
				Duration:      elapsed,
				DurationMs:    elapsed.Milliseconds(),
				Completed:     false,
			}, ErrClientDisconnected
		default:
		}

		nr, err := src.Read(buf)
		if nr > 0 {
			nw, writeErr := w.Write(buf[:nr])
			if nw > 0 {
				totalBytes += int64(nw)
				totalChunks++
				if hasFlusher {
					flusher.Flush()
				}
			}
			if writeErr != nil {
				elapsed := time.Since(start)
				return &StreamMetrics{
					BytesWritten:  totalBytes,
					ChunksWritten: totalChunks,
					Duration:      elapsed,
					DurationMs:    elapsed.Milliseconds(),
					Completed:     false,
				}, writeErr
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			elapsed := time.Since(start)
			return &StreamMetrics{
				BytesWritten:  totalBytes,
				ChunksWritten: totalChunks,
				Duration:      elapsed,
				DurationMs:    elapsed.Milliseconds(),
				Completed:     false,
			}, err
		}
	}

	elapsed := time.Since(start)
	sec := elapsed.Seconds()
	var mbPerSec float64
	if sec > 0 {
		mbPerSec = (float64(totalBytes) / (1024 * 1024)) / sec
	}

	return &StreamMetrics{
		BytesWritten:  totalBytes,
		ChunksWritten: totalChunks,
		Duration:      elapsed,
		DurationMs:    elapsed.Milliseconds(),
		ThroughputMBs: mbPerSec,
		Completed:     true,
	}, nil
}
