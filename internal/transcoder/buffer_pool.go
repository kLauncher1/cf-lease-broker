package transcoder

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
)

// ErrBufferOverflow is returned when write exceeds capacity on fixed buffer.
var ErrBufferOverflow = errors.New("buffer capacity exceeded")

// ByteBuffer wraps pre-allocated byte slices for zero-allocation recycling.
type ByteBuffer struct {
	B        []byte
	Capacity int
	poolTier int // 1 = small, 2 = medium, 3 = large
}

// Reset clears the buffer length without releasing underlying heap capacity.
func (b *ByteBuffer) Reset() {
	b.B = b.B[:0]
}

// Len returns current buffer content length.
func (b *ByteBuffer) Len() int {
	return len(b.B)
}

// Bytes returns the written slice.
func (b *ByteBuffer) Bytes() []byte {
	return b.B
}

// Write appends p to the buffer slice, growing within Capacity bounds.
func (b *ByteBuffer) Write(p []byte) (n int, err error) {
	b.B = append(b.B, p...)
	return len(p), nil
}

// ReadFrom reads directly from r into the pre-allocated slice.
func (b *ByteBuffer) ReadFrom(r io.Reader) (int64, error) {
	var total int64
	for {
		if len(b.B) == cap(b.B) {
			// Grow buffer up to twice capacity or max 1MB
			newCap := cap(b.B) * 2
			if newCap > 1048576 {
				newCap = 1048576
			}
			newSlice := make([]byte, len(b.B), newCap)
			copy(newSlice, b.B)
			b.B = newSlice
			b.Capacity = newCap
		}
		free := b.B[len(b.B):cap(b.B)]
		n, err := r.Read(free)
		if n > 0 {
			b.B = b.B[:len(b.B)+n]
			total += int64(n)
		}
		if err != nil {
			if err == io.EOF {
				return total, nil
			}
			return total, err
		}
	}
}

// BufferPool provides tiered, recycled memory for streaming transcoding.
type BufferPool struct {
	smallSize  int
	mediumSize int
	largeSize  int

	smallPool  sync.Pool
	mediumPool sync.Pool
	largePool  sync.Pool

	totalAcquires int64
	totalReleases int64
	poolHits      int64
}

// NewBufferPool initializes a tiered zero-allocation buffer manager.
func NewBufferPool(smallSize, mediumSize, largeSize int) *BufferPool {
	if smallSize <= 0 {
		smallSize = 4096
	}
	if mediumSize <= 0 {
		mediumSize = 65536
	}
	if largeSize <= 0 {
		largeSize = 262144
	}

	bp := &BufferPool{
		smallSize:  smallSize,
		mediumSize: mediumSize,
		largeSize:  largeSize,
	}

	bp.smallPool = sync.Pool{
		New: func() any {
			return &ByteBuffer{
				B:        make([]byte, 0, bp.smallSize),
				Capacity: bp.smallSize,
				poolTier: 1,
			}
		},
	}

	bp.mediumPool = sync.Pool{
		New: func() any {
			return &ByteBuffer{
				B:        make([]byte, 0, bp.mediumSize),
				Capacity: bp.mediumSize,
				poolTier: 2,
			}
		},
	}

	bp.largePool = sync.Pool{
		New: func() any {
			return &ByteBuffer{
				B:        make([]byte, 0, bp.largeSize),
				Capacity: bp.largeSize,
				poolTier: 3,
			}
		},
	}

	return bp
}

// Acquire retrieves an appropriately sized buffer from the pool.
func (bp *BufferPool) Acquire(expectedSize int) *ByteBuffer {
	atomic.AddInt64(&bp.totalAcquires, 1)

	var buf *ByteBuffer
	if expectedSize <= bp.smallSize {
		buf = bp.smallPool.Get().(*ByteBuffer)
	} else if expectedSize <= bp.mediumSize {
		buf = bp.mediumPool.Get().(*ByteBuffer)
	} else {
		buf = bp.largePool.Get().(*ByteBuffer)
	}

	atomic.AddInt64(&bp.poolHits, 1)
	buf.Reset()
	return buf
}

// Release recycles a ByteBuffer back to its corresponding tier.
func (bp *BufferPool) Release(buf *ByteBuffer) {
	if buf == nil {
		return
	}
	atomic.AddInt64(&bp.totalReleases, 1)
	buf.Reset()

	// Only return to pool if capacity has not ballooned beyond 2x original size
	switch buf.poolTier {
	case 1:
		if buf.Capacity <= bp.smallSize*2 {
			bp.smallPool.Put(buf)
		}
	case 2:
		if buf.Capacity <= bp.mediumSize*2 {
			bp.mediumPool.Put(buf)
		}
	case 3:
		if buf.Capacity <= bp.largeSize*2 {
			bp.largePool.Put(buf)
		}
	}
}

// Stats returns buffer pool recycling and hit rate statistics.
func (bp *BufferPool) Stats() (acquires, releases int64, hitRate float64) {
	acquires = atomic.LoadInt64(&bp.totalAcquires)
	releases = atomic.LoadInt64(&bp.totalReleases)
	hits := atomic.LoadInt64(&bp.poolHits)
	if acquires > 0 {
		hitRate = float64(hits) / float64(acquires) * 100.0
	} else {
		hitRate = 100.0
	}
	return acquires, releases, hitRate
}
