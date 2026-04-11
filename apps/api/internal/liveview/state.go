// Package liveview — state machine definitions for live view sessions.
// States and transitions match live-view-protocol.md exactly.
package liveview

import "fmt"

// State represents a live view session state.
type State string

const (
	StateRequested       State = "REQUESTED"
	StateApproved        State = "APPROVED"
	StateActive          State = "ACTIVE"
	StateEnded           State = "ENDED"
	StateDenied          State = "DENIED"
	StateExpired         State = "EXPIRED"
	StateFailed          State = "FAILED"
	StateTerminatedByHR  State = "TERMINATED_BY_HR"
	StateTerminatedByDPO State = "TERMINATED_BY_DPO"
)

// IsTerminal returns true if no further transitions are possible.
func (s State) IsTerminal() bool {
	switch s {
	case StateEnded, StateDenied, StateExpired, StateFailed,
		StateTerminatedByHR, StateTerminatedByDPO:
		return true
	}
	return false
}

// Transition validates and returns the target state for an event.
// Returns an error if the transition is invalid.
func Transition(current State, event string) (State, error) {
	type edge struct {
		from  State
		event string
		to    State
	}
	edges := []edge{
		{StateRequested, "approve", StateApproved},
		{StateRequested, "deny", StateDenied},
		{StateRequested, "expire", StateExpired},
		{StateApproved, "agent_start", StateActive},
		{StateApproved, "agent_fail", StateFailed},
		{StateApproved, "agent_decline", StateFailed},
		{StateActive, "end", StateEnded},
		// Legacy HR terminate kept as a no-op transition target so old
		// rows in live_view_sessions don't break state machine lookups.
		// New IT-based terminations land in the same terminal state
		// family — the audit action is what distinguishes them.
		{StateActive, "hr_terminate", StateTerminatedByHR},
		{StateActive, "admin_terminate", StateTerminatedByHR},
		{StateActive, "it_manager_terminate", StateTerminatedByHR},
		{StateActive, "dpo_terminate", StateTerminatedByDPO},
		{StateActive, "expire", StateExpired},
	}
	for _, e := range edges {
		if e.from == current && e.event == event {
			return e.to, nil
		}
	}
	return current, fmt.Errorf("liveview: invalid transition from %s on event %q", current, event)
}
