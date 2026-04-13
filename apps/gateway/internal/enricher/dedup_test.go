package enricher

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestDeduper_SeenTwice(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	d := NewDeduper(1000, 5*time.Minute, reg)
	defer d.Close()

	ctx := context.Background()
	payload := []byte(`{"pid":1234,"image":"C:\\a.exe"}`)

	if seen := d.Seen(ctx, "agent-1", "process_start", 1_700_000_000_000_000_000, payload); seen {
		t.Fatal("first call should be a miss")
	}
	if seen := d.Seen(ctx, "agent-1", "process_start", 1_700_000_000_000_000_000, payload); !seen {
		t.Fatal("second call with same tuple should be a hit")
	}
}

func TestDeduper_DifferentTupleDifferentBucket(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	d := NewDeduper(1000, 5*time.Minute, reg)
	defer d.Close()

	ctx := context.Background()
	payload := []byte(`{"pid":1234}`)

	cases := []struct {
		agent, kind string
		ts          int64
	}{
		{"agent-1", "process_start", 1_000_000_000},
		{"agent-2", "process_start", 1_000_000_000}, // different agent
		{"agent-1", "process_stop", 1_000_000_000},  // different kind
		{"agent-1", "process_start", 2_000_000_000}, // different ts
	}
	for _, c := range cases {
		if seen := d.Seen(ctx, c.agent, c.kind, c.ts, payload); seen {
			t.Fatalf("tuple %+v should be a miss on first insertion", c)
		}
	}
	// Re-insert each tuple → should all be hits now.
	for _, c := range cases {
		if seen := d.Seen(ctx, c.agent, c.kind, c.ts, payload); !seen {
			t.Fatalf("tuple %+v should be a hit on second insertion", c)
		}
	}
}

func TestDeduper_TTLExpiry(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	d := NewDeduper(1000, 50*time.Millisecond, reg)
	defer d.Close()

	ctx := context.Background()
	_ = d.Seen(ctx, "agent-1", "file_created", 1, []byte("x"))
	time.Sleep(100 * time.Millisecond) // > TTL

	// After TTL expiry, the tuple should be treated as unseen again.
	if seen := d.Seen(ctx, "agent-1", "file_created", 1, []byte("x")); seen {
		t.Fatal("expired tuple should be a miss after TTL")
	}
}

func TestDeduper_CapacityEviction(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	d := NewDeduper(3, 5*time.Minute, reg)
	defer d.Close()

	ctx := context.Background()

	d.Seen(ctx, "a", "k1", 1, []byte("1"))
	d.Seen(ctx, "a", "k1", 2, []byte("2"))
	d.Seen(ctx, "a", "k1", 3, []byte("3"))
	if d.Len() != 3 {
		t.Fatalf("expected 3 entries, got %d", d.Len())
	}

	// Insert a 4th — FIFO should evict the 1st.
	d.Seen(ctx, "a", "k1", 4, []byte("4"))
	if d.Len() != 3 {
		t.Fatalf("expected 3 entries after eviction, got %d", d.Len())
	}

	// The first (ts=1) tuple should now be a miss again.
	if seen := d.Seen(ctx, "a", "k1", 1, []byte("1")); seen {
		t.Fatal("evicted tuple should be a miss")
	}
}

func TestDeduper_KeystrokeContentExcludesPayload(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	d := NewDeduper(1000, 5*time.Minute, reg)
	defer d.Close()

	ctx := context.Background()
	// Two different ciphertexts at the same (agent, ts, kind) — should
	// collapse to the same dedup bucket because we strip payload for
	// keystroke content events.
	if seen := d.Seen(ctx, "a", keystrokeContentEventKind, 1, []byte("ciphertext-one")); seen {
		t.Fatal("first keystroke-content insert should be a miss")
	}
	if seen := d.Seen(ctx, "a", keystrokeContentEventKind, 1, []byte("ciphertext-TWO")); !seen {
		t.Fatal("second keystroke-content insert at same (agent,ts) should be a hit — payload is excluded from hash")
	}
}

func TestDeduper_NonKeystrokeHashesPayload(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	d := NewDeduper(1000, 5*time.Minute, reg)
	defer d.Close()

	ctx := context.Background()
	if seen := d.Seen(ctx, "a", "process_start", 1, []byte("pid=1")); seen {
		t.Fatal("first insert should be a miss")
	}
	if seen := d.Seen(ctx, "a", "process_start", 1, []byte("pid=2")); seen {
		t.Fatal("different payload at same (agent,ts,kind) should be a miss for non-keystroke events")
	}
}

func TestDeduper_CloseIdempotent(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	d := NewDeduper(10, time.Second, reg)
	d.Close()
	d.Close() // should not panic
}
