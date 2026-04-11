// Package upload — RecordingSession state machine.
//
// States per ADR 0019 §Scope: waiting → recording → completed → archived
// The state machine is in-memory; persistence is handled by the caller
// (upload/handler.go writes state transitions to Postgres via the audit
// forwarder so the Admin API can observe session state).
package upload

import (
	"fmt"
	"sync"
	"time"
)

// SessionState represents a recording session's lifecycle stage.
type SessionState string

const (
	// StateWaiting — session created, awaiting first chunk.
	StateWaiting SessionState = "waiting"
	// StateRecording — at least one chunk received; recording in progress.
	StateRecording SessionState = "recording"
	// StateCompleted — final chunk received; no more chunks expected.
	StateCompleted SessionState = "completed"
	// StateArchived — TTL scheduled; object retention tag applied.
	StateArchived SessionState = "archived"
	// StateError — unrecoverable error; no further chunks accepted.
	StateError SessionState = "error"
)

// RecordingSession holds per-session recording state.
// Thread-safe via mu.
type RecordingSession struct {
	mu sync.Mutex

	SessionID  string
	TenantID   string
	State      SessionState
	NextChunk  uint64       // monotonically increasing; next expected chunk index
	StartedAt  time.Time
	LastChunkAt time.Time
	EndedAt    time.Time
	BytesTotal int64
	FrameCount int64
	// WrappedDEK is the Vault-wrapped session DEK stored for playback.
	// Populated during session initialisation; never nil once recording.
	WrappedDEK string
	LVMKVersion int
	// ErrorMessage is set on StateError.
	ErrorMessage string
}

// NewSession creates a new RecordingSession in StateWaiting.
func NewSession(sessionID, tenantID, wrappedDEK string, lvmkVersion int) *RecordingSession {
	return &RecordingSession{
		SessionID:   sessionID,
		TenantID:    tenantID,
		State:       StateWaiting,
		NextChunk:   0,
		StartedAt:   time.Now().UTC(),
		WrappedDEK:  wrappedDEK,
		LVMKVersion: lvmkVersion,
	}
}

// AcceptChunk validates that chunkIndex is the next expected value and advances
// the counter. Returns an error if the chunk is out of order or arrived in
// a terminal/error state.
//
// Per ADR 0019 quality bar: chunk-index monotonicity enforced — out-of-order
// chunks are rejected.
func (s *RecordingSession) AcceptChunk(chunkIndex uint64, sizeBytes int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch s.State {
	case StateCompleted, StateArchived:
		return fmt.Errorf("session %s: already completed, refusing chunk %d", s.SessionID, chunkIndex)
	case StateError:
		return fmt.Errorf("session %s: in error state, refusing chunk %d", s.SessionID, chunkIndex)
	}

	if chunkIndex != s.NextChunk {
		return fmt.Errorf("session %s: expected chunk %d, got %d (out-of-order rejected)",
			s.SessionID, s.NextChunk, chunkIndex)
	}

	s.NextChunk++
	s.BytesTotal += int64(sizeBytes)
	s.LastChunkAt = time.Now().UTC()
	if s.State == StateWaiting {
		s.State = StateRecording
	}
	return nil
}

// Complete marks the session as completed. After this call no further chunks
// are accepted. endedAt is set to now.
func (s *RecordingSession) Complete() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.State != StateRecording && s.State != StateWaiting {
		return fmt.Errorf("session %s: cannot complete from state %s", s.SessionID, s.State)
	}
	s.State = StateCompleted
	s.EndedAt = time.Now().UTC()
	return nil
}

// Archive marks the session as archived (TTL tag applied in MinIO).
func (s *RecordingSession) Archive() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.State != StateCompleted {
		return fmt.Errorf("session %s: cannot archive from state %s", s.SessionID, s.State)
	}
	s.State = StateArchived
	return nil
}

// Fail transitions the session to StateError with a message.
func (s *RecordingSession) Fail(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State = StateError
	s.ErrorMessage = msg
	s.EndedAt = time.Now().UTC()
}

// Snapshot returns a point-in-time copy of the session for safe reading
// without holding the lock for extended periods.
func (s *RecordingSession) Snapshot() RecordingSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *s
	return cp
}
