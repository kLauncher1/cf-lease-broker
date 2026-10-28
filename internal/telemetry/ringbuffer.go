package telemetry

import (
	"sync"
	"sync/atomic"
)

// RingBuffer provides a high-throughput, bounded circular buffer for eBPF socket events.
type RingBuffer struct {
	mu       sync.Mutex
	capacity int
	buffer   []SocketMetrics
	head     int
	tail     int
	count    int
	dropped  uint64
}

// NewRingBuffer allocates a circular buffer with the specified capacity.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = 1024
	}
	return &RingBuffer{
		capacity: capacity,
		buffer:   make([]SocketMetrics, capacity),
	}
}

// Push appends a socket metrics event into the ring buffer.
// If the buffer is full, the oldest event is overwritten and the dropped count is incremented.
func (rb *RingBuffer) Push(metrics SocketMetrics) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.buffer[rb.tail] = metrics
	rb.tail = (rb.tail + 1) % rb.capacity

	if rb.count < rb.capacity {
		rb.count++
	} else {
		// Overwrite oldest unread entry
		rb.head = (rb.head + 1) % rb.capacity
		atomic.AddUint64(&rb.dropped, 1)
	}
}

// Drain extracts up to limit events from the ring buffer in FIFO order.
func (rb *RingBuffer) Drain(limit int) []SocketMetrics {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.count == 0 || limit <= 0 {
		return nil
	}

	n := limit
	if n > rb.count {
		n = rb.count
	}

	items := make([]SocketMetrics, n)
	for i := 0; i < n; i++ {
		items[i] = rb.buffer[rb.head]
		rb.head = (rb.head + 1) % rb.capacity
	}
	rb.count -= n

	return items
}

// DrainAll extracts all queued events from the ring buffer.
func (rb *RingBuffer) DrainAll() []SocketMetrics {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.count == 0 {
		return nil
	}

	items := make([]SocketMetrics, rb.count)
	for i := 0; i < rb.count; i++ {
		items[i] = rb.buffer[rb.head]
		rb.head = (rb.head + 1) % rb.capacity
	}
	rb.count = 0

	return items
}

// Len returns the current number of queued events.
func (rb *RingBuffer) Len() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.count
}

// Capacity returns the maximum capacity of the ring buffer.
func (rb *RingBuffer) Capacity() int {
	return rb.capacity
}

// DroppedCount returns the total number of events dropped due to buffer overflow.
func (rb *RingBuffer) DroppedCount() uint64 {
	return atomic.LoadUint64(&rb.dropped)
}

// Reset clears the ring buffer and resets counters.
func (rb *RingBuffer) Reset() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.head = 0
	rb.tail = 0
	rb.count = 0
	atomic.StoreUint64(&rb.dropped, 0)
}
