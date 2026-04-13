// Package audit — tests for the in-process fanout broker (Faz 6 #66).
//
// These tests exercise Broker.Publish / Subscribe / Unsubscribe and the
// StripSensitive KVKK guardrail directly. The WebSocket handler itself
// is exercised in the integration suite because it needs a live chi
// router and a recorder stub.
package audit

import (
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newEntry is a small helper so each test case is a one-liner.
func newEntry(tenant, actor string, action Action, target string) Entry {
	return Entry{
		Actor:    actor,
		TenantID: tenant,
		Action:   action,
		Target:   target,
	}
}

// ── Broker.Publish fanout ────────────────────────────────────────────────────

func TestBroker_Publish_FansOutToAllMatching(t *testing.T) {
	b := NewBroker(testLogger())

	a := NewSubscriber("tenant-1", false, StreamFilter{})
	c := NewSubscriber("tenant-1", false, StreamFilter{})
	b.Subscribe(a)
	b.Subscribe(c)

	if got := b.Subscribers(); got != 2 {
		t.Fatalf("Subscribers()=%d, want 2", got)
	}

	b.Publish(newEntry("tenant-1", "u-1", ActionPolicyPushed, "policy/42"))

	// Both subscribers must observe the entry.
	for name, sub := range map[string]*Subscriber{"a": a, "c": c} {
		select {
		case got := <-sub.C():
			if got.Action != ActionPolicyPushed {
				t.Errorf("%s: Action=%q, want %q", name, got.Action, ActionPolicyPushed)
			}
		case <-time.After(time.Second):
			t.Errorf("%s: did not receive entry within timeout", name)
		}
	}
}

// ── Slow subscriber drop semantics ───────────────────────────────────────────

func TestBroker_Publish_SlowSubscriberDoesNotBlockOthers(t *testing.T) {
	b := NewBroker(testLogger())

	// "slow" never reads from its channel. We publish enough entries to
	// exceed the 64-element buffer and confirm publish still returns and
	// the fast subscriber still receives every entry.
	slow := NewSubscriber("tenant-1", false, StreamFilter{})
	fast := NewSubscriber("tenant-1", false, StreamFilter{})
	b.Subscribe(slow)
	b.Subscribe(fast)

	// Drain fast concurrently so its channel never fills.
	received := make(chan int, 1)
	go func() {
		n := 0
		deadline := time.After(2 * time.Second)
		for {
			select {
			case _, ok := <-fast.C():
				if !ok {
					received <- n
					return
				}
				n++
				if n == 200 {
					received <- n
					return
				}
			case <-deadline:
				received <- n
				return
			}
		}
	}()

	for i := 0; i < 200; i++ {
		b.Publish(newEntry("tenant-1", "u-1", ActionPolicyPushed, "policy/42"))
	}

	got := <-received
	if got != 200 {
		t.Errorf("fast received %d entries, want 200", got)
	}

	// Slow must have recorded drops: buffer is 64, we sent 200.
	if slow.Dropped() == 0 {
		t.Error("expected slow subscriber to report dropped>0, got 0")
	}
	if slow.Dropped() > int64(200-64) {
		t.Errorf("slow dropped=%d, should be at most %d", slow.Dropped(), 200-64)
	}
}

// ── Tenant isolation ─────────────────────────────────────────────────────────

func TestBroker_Publish_RespectsTenantIsolation(t *testing.T) {
	b := NewBroker(testLogger())

	a := NewSubscriber("tenant-A", false, StreamFilter{})
	b2 := NewSubscriber("tenant-B", false, StreamFilter{})
	b.Subscribe(a)
	b.Subscribe(b2)

	b.Publish(newEntry("tenant-A", "u-1", ActionPolicyPushed, "policy/1"))

	select {
	case <-a.C():
		// Expected.
	case <-time.After(time.Second):
		t.Error("tenant-A subscriber did not receive its own entry")
	}

	select {
	case got := <-b2.C():
		t.Errorf("tenant-B subscriber leaked tenant-A entry: %+v", got)
	case <-time.After(100 * time.Millisecond):
		// Expected: nothing arrives.
	}
}

// ── all_tenants bypass ───────────────────────────────────────────────────────

func TestBroker_Publish_AllTenantsBypassesFilter(t *testing.T) {
	b := NewBroker(testLogger())

	dpo := NewSubscriber("tenant-DPO", true, StreamFilter{})
	b.Subscribe(dpo)

	b.Publish(newEntry("tenant-X", "u-1", ActionDSRSubmitted, "dsr/1"))
	b.Publish(newEntry("tenant-Y", "u-2", ActionDSRSubmitted, "dsr/2"))

	seen := 0
	for i := 0; i < 2; i++ {
		select {
		case <-dpo.C():
			seen++
		case <-time.After(time.Second):
			t.Fatalf("DPO subscriber received only %d of 2 entries", seen)
		}
	}
	if seen != 2 {
		t.Errorf("seen=%d, want 2", seen)
	}
}

// ── StreamFilter.match ───────────────────────────────────────────────────────

func TestStreamFilter_Actions(t *testing.T) {
	f := StreamFilter{Actions: []string{string(ActionPolicyPushed)}}
	if !f.match(newEntry("t", "u", ActionPolicyPushed, "x")) {
		t.Error("expected match for allowlisted action")
	}
	if f.match(newEntry("t", "u", ActionDSRSubmitted, "x")) {
		t.Error("expected no match for non-allowlisted action")
	}
}

func TestStreamFilter_ActorIDs(t *testing.T) {
	f := StreamFilter{ActorIDs: []string{"u-42"}}
	if !f.match(newEntry("t", "u-42", ActionPolicyPushed, "x")) {
		t.Error("expected match for allowlisted actor")
	}
	if f.match(newEntry("t", "u-99", ActionPolicyPushed, "x")) {
		t.Error("expected no match for non-allowlisted actor")
	}
}

func TestStreamFilter_TargetPrefix(t *testing.T) {
	f := StreamFilter{TargetPrefix: "endpoint."}
	if !f.match(newEntry("t", "u", ActionEndpointWipe, "endpoint.ep-42")) {
		t.Error("expected match for endpoint. prefix")
	}
	if f.match(newEntry("t", "u", ActionEndpointWipe, "policy.p-42")) {
		t.Error("expected no match for policy. target with endpoint. filter")
	}
}

// ── Unsubscribe lifecycle ────────────────────────────────────────────────────

func TestBroker_Unsubscribe_ClosesChannelAndRemoves(t *testing.T) {
	b := NewBroker(testLogger())

	s := NewSubscriber("tenant-1", false, StreamFilter{})
	b.Subscribe(s)

	if got := b.Subscribers(); got != 1 {
		t.Fatalf("Subscribers()=%d, want 1", got)
	}

	b.Unsubscribe(s)

	if got := b.Subscribers(); got != 0 {
		t.Errorf("Subscribers()=%d after unsubscribe, want 0", got)
	}

	// Channel must be closed so handler goroutines unblock.
	select {
	case _, ok := <-s.C():
		if ok {
			t.Error("expected channel closed after Unsubscribe")
		}
	case <-time.After(time.Second):
		t.Error("channel not closed within timeout")
	}
}

func TestBroker_Unsubscribe_Idempotent(t *testing.T) {
	b := NewBroker(testLogger())
	s := NewSubscriber("t", false, StreamFilter{})
	b.Subscribe(s)
	b.Unsubscribe(s)
	// Second call must not panic or double-close.
	b.Unsubscribe(s)
}

// ── Concurrent publish + unsubscribe ─────────────────────────────────────────

func TestBroker_ConcurrentPublishAndUnsubscribe_NoPanic(t *testing.T) {
	b := NewBroker(testLogger())

	subs := make([]*Subscriber, 16)
	for i := range subs {
		subs[i] = NewSubscriber("tenant-1", false, StreamFilter{})
		b.Subscribe(subs[i])
	}

	var wg sync.WaitGroup

	// Publisher.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			b.Publish(newEntry("tenant-1", "u", ActionPolicyPushed, "policy/x"))
		}
	}()

	// Draining consumers.
	for i := range subs {
		sub := subs[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range sub.C() {
				// drain
			}
		}()
	}

	// Unsubscribe half of them while publish is running.
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		for i := 0; i < len(subs); i += 2 {
			b.Unsubscribe(subs[i])
		}
	}()

	// Let the publisher finish, then tear down the rest.
	time.Sleep(100 * time.Millisecond)
	for i := 1; i < len(subs); i += 2 {
		b.Unsubscribe(subs[i])
	}
	wg.Wait()

	if got := b.Subscribers(); got != 0 {
		t.Errorf("Subscribers()=%d after teardown, want 0", got)
	}
}

// ── StripSensitive KVKK invariant ────────────────────────────────────────────

func TestStripSensitive_RemovesKeystrokeContent(t *testing.T) {
	e := Entry{
		Action: ActionDLPMatchViewed,
		Details: map[string]any{
			"keystroke_content": "GIZLI-PAROLA",
			"app":               "Word",
			"password":          "abcdef",
			"window_title":      "Rapor.docx",
		},
	}
	out := StripSensitive(e)
	if _, ok := out.Details["keystroke_content"]; ok {
		t.Error("keystroke_content must be stripped")
	}
	if _, ok := out.Details["password"]; ok {
		t.Error("password must be stripped")
	}
	if v, ok := out.Details["app"]; !ok || v != "Word" {
		t.Error("non-sensitive keys must be preserved")
	}
	// Original entry must not have been mutated — the audit chain
	// input is authoritative and callers rely on it being unchanged.
	if _, ok := e.Details["keystroke_content"]; !ok {
		t.Error("StripSensitive must not mutate the input Entry.Details")
	}
}

func TestStripSensitive_NoOpWhenClean(t *testing.T) {
	e := Entry{
		Details: map[string]any{"app": "Excel"},
	}
	out := StripSensitive(e)
	// Same map reference is fine — no copy needed when nothing matches.
	if len(out.Details) != 1 {
		t.Errorf("Details len=%d, want 1", len(out.Details))
	}
}

func TestBroker_Publish_StripsSensitiveBeforeFanout(t *testing.T) {
	b := NewBroker(testLogger())
	s := NewSubscriber("t", false, StreamFilter{})
	b.Subscribe(s)

	b.Publish(Entry{
		TenantID: "t",
		Action:   ActionDLPMatchViewed,
		Target:   "dlp/match/1",
		Details: map[string]any{
			"keystroke_content": "GIZLI",
			"app":               "Notepad",
		},
	})

	select {
	case got := <-s.C():
		if _, ok := got.Details["keystroke_content"]; ok {
			t.Error("stream leaked keystroke_content — KVKK violation")
		}
		if got.Details["app"] != "Notepad" {
			t.Error("non-sensitive detail missing")
		}
	case <-time.After(time.Second):
		t.Fatal("no entry received")
	}
}
