package grpcserver

import (
	"sync"
	"sync/atomic"
)

// Window implements a bounded in-flight window for EventBatch ACKing.
// The gateway only sends BatchAck after JetStream publish confirms,
// so the agent won't race ahead more than MaxUnacked batches.
//
// Design: a counting semaphore backed by a channel. The stream handler
// acquires a slot before dispatching a batch to the publisher goroutine;
// the publisher releases the slot after the NATS ack is received. If
// all slots are taken, the stream handler blocks (backpressure) which
// naturally stops reading from the gRPC recv loop, causing TCP flow
// control to propagate back to the agent.
type Window struct {
	sem      chan struct{}
	maxSlots int
	// inflight is an atomic counter for metrics / debug.
	inflight int64
}

// NewWindow creates a Window with capacity maxUnacked.
// A maxUnacked of 16 means at most 16 batches can be in-flight between
// the gateway and NATS at any given time per stream.
func NewWindow(maxUnacked int) *Window {
	sem := make(chan struct{}, maxUnacked)
	for i := 0; i < maxUnacked; i++ {
		sem <- struct{}{}
	}
	return &Window{sem: sem, maxSlots: maxUnacked}
}

// Acquire blocks until a window slot is available or ctx is cancelled.
// Returns false if the context was cancelled before a slot became available.
func (w *Window) Acquire(done <-chan struct{}) bool {
	select {
	case <-w.sem:
		atomic.AddInt64(&w.inflight, 1)
		return true
	case <-done:
		return false
	}
}

// Release returns a slot to the window. Must be called exactly once per
// successful Acquire, typically in a defer inside the publish goroutine.
func (w *Window) Release() {
	atomic.AddInt64(&w.inflight, -1)
	w.sem <- struct{}{}
}

// Inflight returns the current number of batches awaiting NATS ack.
func (w *Window) Inflight() int {
	return int(atomic.LoadInt64(&w.inflight))
}

// batchRequest carries a single EventBatch through the publish pipeline.
type batchRequest struct {
	// batchID from the proto EventBatch.batch_id field.
	batchID uint64
	// subject is the NATS JetStream subject to publish on.
	subject string
	// payload is the serialized proto bytes of the EventBatch.
	payload []byte
	// result is signalled by the publisher after NATS ack; true = success.
	result chan publishResult
}

// publishResult is the outcome of an async NATS publish.
type publishResult struct {
	err error
}

// publishQueue is an in-process queue of batchRequests between the stream
// handler and the NATS publisher goroutine(s). The buffer size is bounded by
// Window.maxSlots so no batch can enter the queue without a window slot.
type publishQueue struct {
	ch chan batchRequest
	mu sync.Mutex
}

func newPublishQueue(cap int) *publishQueue {
	return &publishQueue{ch: make(chan batchRequest, cap)}
}
