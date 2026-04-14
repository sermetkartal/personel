package tickets

import (
	"context"
	"testing"
)

func TestCanTransition(t *testing.T) {
	cases := []struct {
		from State
		to   State
		want bool
	}{
		{StateOpen, StateInProgress, true},
		{StateOpen, StateRejected, true},
		{StateOpen, StateResolved, true}, // skip in_progress
		{StateOpen, StateClosed, false},  // cannot skip resolved
		{StateInProgress, StateResolved, true},
		{StateInProgress, StateRejected, true},
		{StateInProgress, StateOpen, false},
		{StateResolved, StateClosed, true},
		{StateResolved, StateOpen, true}, // reopen
		{StateClosed, StateOpen, false},  // closed is terminal
		{StateClosed, StateResolved, false},
		{StateRejected, StateOpen, true}, // reopen
		{StateRejected, StateResolved, false},
	}
	for _, c := range cases {
		got := canTransition(c.from, c.to)
		if got != c.want {
			t.Errorf("canTransition(%s,%s) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

func TestIsValidSeverity(t *testing.T) {
	valid := []Severity{SeverityP1, SeverityP2, SeverityP3, SeverityP4}
	for _, s := range valid {
		if !isValidSeverity(s) {
			t.Errorf("severity %s should be valid", s)
		}
	}
	if isValidSeverity("P0") {
		t.Error("P0 should not be valid")
	}
	if isValidSeverity("") {
		t.Error("empty should not be valid")
	}
}

func TestIsValidState(t *testing.T) {
	valid := []State{StateOpen, StateInProgress, StateResolved, StateClosed, StateRejected}
	for _, s := range valid {
		if !isValidState(s) {
			t.Errorf("state %s should be valid", s)
		}
	}
	if isValidState("archived") {
		t.Error("'archived' should not be valid")
	}
}

func TestInternalProvider(t *testing.T) {
	p := InternalProvider{}
	if p.Name() != ProviderNameInternal {
		t.Errorf("Name() = %s, want %s", p.Name(), ProviderNameInternal)
	}
	extID, err := p.Create(context.Background(), &Ticket{})
	if err != nil {
		t.Errorf("Create: %v", err)
	}
	if extID != "" {
		t.Errorf("Create returned externalID=%q, want empty", extID)
	}
	if err := p.Update(context.Background(), &Ticket{}); err != nil {
		t.Errorf("Update: %v", err)
	}
	if _, err := p.HandleWebhook(context.Background(), []byte("{}")); err == nil {
		t.Error("HandleWebhook should return ErrNotConfigured")
	}
}

func TestJiraProviderStub(t *testing.T) {
	p := &JiraProvider{BaseURL: "https://example.atlassian.net"}
	if p.Name() != ProviderNameJira {
		t.Errorf("Name() = %s", p.Name())
	}
	_, err := p.Create(context.Background(), &Ticket{Subject: "test"})
	if err != ErrNotConfigured {
		t.Errorf("Create error = %v, want ErrNotConfigured", err)
	}
	// HandleWebhook stub parses the payload enough to find the key
	payload := `{"webhookEvent":"jira:issue_updated","issue":{"key":"TEST-1","fields":{"status":{"name":"In Progress"}}}}`
	partial, err := p.HandleWebhook(context.Background(), []byte(payload))
	if err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}
	if partial.ProviderID != "TEST-1" {
		t.Errorf("ProviderID = %q, want TEST-1", partial.ProviderID)
	}
	if partial.State != StateInProgress {
		t.Errorf("State = %q, want in_progress", partial.State)
	}
}

func TestJiraStatusMapping(t *testing.T) {
	cases := map[string]State{
		"Open":        StateOpen,
		"To Do":       StateOpen,
		"Backlog":     StateOpen,
		"In Progress": StateInProgress,
		"Doing":       StateInProgress,
		"Done":        StateResolved,
		"Resolved":    StateResolved,
		"Closed":      StateClosed,
		"Unknown":     "",
	}
	for in, want := range cases {
		got := jiraStatusToState(in)
		if got != want {
			t.Errorf("jiraStatusToState(%q) = %q, want %q", in, got, want)
		}
	}
}
