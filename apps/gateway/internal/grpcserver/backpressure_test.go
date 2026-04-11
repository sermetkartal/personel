package grpcserver

import (
	"testing"
)

func TestWindowAcquireRelease(t *testing.T) {
	w := NewWindow(3)
	done := make(chan struct{})
	defer close(done)

	if !w.Acquire(done) {
		t.Fatal("first Acquire should succeed immediately")
	}
	if !w.Acquire(done) {
		t.Fatal("second Acquire should succeed immediately")
	}
	if !w.Acquire(done) {
		t.Fatal("third Acquire should succeed immediately")
	}
	if w.Inflight() != 3 {
		t.Errorf("expected 3 inflight, got %d", w.Inflight())
	}

	// Release one and verify we can acquire again.
	w.Release()
	if w.Inflight() != 2 {
		t.Errorf("expected 2 inflight after release, got %d", w.Inflight())
	}
	if !w.Acquire(done) {
		t.Fatal("Acquire after Release should succeed")
	}
}

func TestWindowBlocksOnFull(t *testing.T) {
	w := NewWindow(1)
	done := make(chan struct{})

	if !w.Acquire(done) {
		t.Fatal("first Acquire should succeed")
	}

	// The window is full; Acquire should block.
	// Test by closing done which should cause Acquire to return false.
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		result := w.Acquire(done)
		if result {
			t.Errorf("Acquire on full window with closed done should return false")
		}
	}()

	close(done) // signal cancellation
	<-doneCh   // wait for goroutine
}
