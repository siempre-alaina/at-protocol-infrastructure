package ingest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// RingBuffer implements a thread-safe ring buffer for events with disk overflow
type RingBuffer struct {
	buffer      []Event
	head        int
	tail        int
	count       int
	capacity    int
	mu          sync.Mutex
	notEmpty    *sync.Cond
	overflowDir string
	overflowSeq int64
}

// NewRingBuffer creates a new ring buffer with optional disk overflow
func NewRingBuffer(capacity int, overflowDir string) *RingBuffer {
	rb := &RingBuffer{
		buffer:      make([]Event, capacity),
		capacity:    capacity,
		overflowDir: overflowDir,
	}
	rb.notEmpty = sync.NewCond(&rb.mu)

	if overflowDir != "" {
		if err := os.MkdirAll(overflowDir, 0755); err != nil {
			log.Error().Err(err).Str("dir", overflowDir).Msg("Failed to create overflow directory")
		}
	}

	return rb
}

// Push adds an event to the buffer
// If the buffer is full and disk overflow is enabled, writes to disk
// Returns true if event was stored, false if dropped
func (rb *RingBuffer) Push(event Event) bool {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.count < rb.capacity {
		// Buffer has space
		rb.buffer[rb.tail] = event
		rb.tail = (rb.tail + 1) % rb.capacity
		rb.count++
		rb.notEmpty.Signal()
		return true
	}

	// Buffer is full - try disk overflow
	if rb.overflowDir != "" {
		return rb.writeOverflow(event)
	}

	// No overflow, drop the event
	log.Warn().
		Str("type", event.Type).
		Str("did", event.DID).
		Int64("seq", event.Seq).
		Msg("Ring buffer full, dropping event")

	return false
}

// Pop removes and returns the next event from the buffer
// Blocks until an event is available
func (rb *RingBuffer) Pop() Event {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	for rb.count == 0 {
		rb.notEmpty.Wait()
	}

	event := rb.buffer[rb.head]
	rb.head = (rb.head + 1) % rb.capacity
	rb.count--

	return event
}

// TryPop attempts to pop an event without blocking
// Returns the event and true if successful, or empty event and false if buffer is empty
func (rb *RingBuffer) TryPop() (Event, bool) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.count == 0 {
		return Event{}, false
	}

	event := rb.buffer[rb.head]
	rb.head = (rb.head + 1) % rb.capacity
	rb.count--

	return event, true
}

// PopWithTimeout attempts to pop an event with a timeout
func (rb *RingBuffer) PopWithTimeout(timeout time.Duration) (Event, bool) {
	deadline := time.Now().Add(timeout)

	rb.mu.Lock()
	defer rb.mu.Unlock()

	for rb.count == 0 {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return Event{}, false
		}

		// Use a goroutine to signal after timeout
		done := make(chan struct{})
		go func() {
			time.Sleep(remaining)
			rb.mu.Lock()
			rb.notEmpty.Broadcast()
			rb.mu.Unlock()
			close(done)
		}()

		rb.notEmpty.Wait()

		select {
		case <-done:
			// Timeout occurred
			if rb.count == 0 {
				return Event{}, false
			}
		default:
		}
	}

	event := rb.buffer[rb.head]
	rb.head = (rb.head + 1) % rb.capacity
	rb.count--

	return event, true
}

// Size returns the current number of events in the buffer
func (rb *RingBuffer) Size() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.count
}

// Capacity returns the buffer capacity
func (rb *RingBuffer) Capacity() int {
	return rb.capacity
}

// IsFull returns whether the buffer is full
func (rb *RingBuffer) IsFull() bool {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.count >= rb.capacity
}

// Clear empties the buffer
func (rb *RingBuffer) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.head = 0
	rb.tail = 0
	rb.count = 0
}

// writeOverflow writes an event to disk when the buffer is full
func (rb *RingBuffer) writeOverflow(event Event) bool {
	rb.overflowSeq++
	filename := filepath.Join(rb.overflowDir, "overflow_"+timeToFilename(time.Now())+"_"+string(rune(rb.overflowSeq))+".json")

	data, err := json.Marshal(event)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal event for overflow")
		return false
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		log.Error().Err(err).Str("file", filename).Msg("Failed to write overflow file")
		return false
	}

	log.Debug().Str("file", filename).Msg("Event written to overflow")
	return true
}

func timeToFilename(t time.Time) string {
	return t.Format("20060102_150405")
}

// BackpressureController manages backpressure signals
type BackpressureController struct {
	highWaterMark float64 // Percentage (0-1) at which to start applying backpressure
	lowWaterMark  float64 // Percentage (0-1) at which to stop applying backpressure
	buffer        *RingBuffer
	mu            sync.RWMutex
	backpressure  bool
}

// NewBackpressureController creates a new backpressure controller
func NewBackpressureController(buffer *RingBuffer, highWaterMark, lowWaterMark float64) *BackpressureController {
	return &BackpressureController{
		highWaterMark: highWaterMark,
		lowWaterMark:  lowWaterMark,
		buffer:        buffer,
	}
}

// ShouldApplyBackpressure returns whether backpressure should be applied
func (bc *BackpressureController) ShouldApplyBackpressure() bool {
	bc.mu.RLock()
	currentState := bc.backpressure
	bc.mu.RUnlock()

	fillRatio := float64(bc.buffer.Size()) / float64(bc.buffer.Capacity())

	bc.mu.Lock()
	defer bc.mu.Unlock()

	if currentState {
		// Currently in backpressure, check if we should exit
		if fillRatio < bc.lowWaterMark {
			bc.backpressure = false
			log.Info().Float64("fill_ratio", fillRatio).Msg("Backpressure released")
		}
	} else {
		// Not in backpressure, check if we should enter
		if fillRatio > bc.highWaterMark {
			bc.backpressure = true
			log.Warn().Float64("fill_ratio", fillRatio).Msg("Backpressure applied")
		}
	}

	return bc.backpressure
}

// GetFillRatio returns the current buffer fill ratio
func (bc *BackpressureController) GetFillRatio() float64 {
	return float64(bc.buffer.Size()) / float64(bc.buffer.Capacity())
}
