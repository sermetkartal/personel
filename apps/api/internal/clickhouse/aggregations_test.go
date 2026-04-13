// aggregations_test.go — unit tests for the /v1/reports/ch aggregation
// queries. These exercise the query-building + result-scanning code
// paths without a live ClickHouse cluster by injecting a fake
// queryRunner.
//
// What they assert:
//  1. tenant_id is always the first positional parameter (tenant
//     isolation invariant — the CC6.1 KVKK-m.5 gate).
//  2. LIMIT clause is always emitted, and caps are enforced when the
//     caller passes Limit > 100.
//  3. user_ids filter builds an `IN ?` clause and binds the slice.
//  4. Missing-table errors from ClickHouse return []row{} + nil err
//     (fresh-install graceful degradation).
//  5. nil client → ErrClientUnavailable from every method.
//  6. Empty result → empty slice (never nil) per the handler contract.
package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// --- fake runner ----------------------------------------------------------

type fakeRunner struct {
	lastQuery string
	lastArgs  []any
	rows      driverRows
	err       error
	callCount int
}

func (f *fakeRunner) Query(_ context.Context, query string, args ...any) (driverRows, error) {
	f.callCount++
	f.lastQuery = query
	f.lastArgs = append([]any(nil), args...)
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

// emptyRows is a driverRows that yields zero rows.
type emptyRows struct{ closed bool }

func (r *emptyRows) Next() bool            { return false }
func (r *emptyRows) Scan(_ ...any) error   { return nil }
func (r *emptyRows) Err() error            { return nil }
func (r *emptyRows) Close() error          { r.closed = true; return nil }

// --- tests ----------------------------------------------------------------

func TestAggTopApps_TenantIDFirstArg(t *testing.T) {
	fake := &fakeRunner{rows: &emptyRows{}}
	c := newClientWithRunner(fake)

	from := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	_, err := c.AggTopApps(context.Background(), "tenant-uuid-xyz", TopAppsParams{
		From: from, To: to, Limit: 5,
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if len(fake.lastArgs) == 0 {
		t.Fatalf("expected positional args, got none")
	}
	if got := fake.lastArgs[0].(string); got != "tenant-uuid-xyz" {
		t.Errorf("tenant_id must be first arg; got %v", got)
	}
	if !strings.Contains(fake.lastQuery, "tenant_id = ?") {
		t.Errorf("query missing tenant_id filter: %s", fake.lastQuery)
	}
	if !strings.Contains(fake.lastQuery, "LIMIT 5") {
		t.Errorf("limit clause missing: %s", fake.lastQuery)
	}
}

func TestAggTopApps_LimitCapped(t *testing.T) {
	fake := &fakeRunner{rows: &emptyRows{}}
	c := newClientWithRunner(fake)

	_, err := c.AggTopApps(context.Background(), "t", TopAppsParams{Limit: 9999})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(fake.lastQuery, "LIMIT 100") {
		t.Errorf("limit should be capped to 100, got: %s", fake.lastQuery)
	}
}

func TestAggTopApps_DefaultLimitWhenZero(t *testing.T) {
	fake := &fakeRunner{rows: &emptyRows{}}
	c := newClientWithRunner(fake)

	_, err := c.AggTopApps(context.Background(), "t", TopAppsParams{Limit: 0})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(fake.lastQuery, "LIMIT 10") {
		t.Errorf("default limit should be 10, got: %s", fake.lastQuery)
	}
}

func TestAggTopApps_UserIDsBuildsINClause(t *testing.T) {
	fake := &fakeRunner{rows: &emptyRows{}}
	c := newClientWithRunner(fake)

	users := []string{"S-1-5-21-A", "S-1-5-21-B"}
	_, err := c.AggTopApps(context.Background(), "t", TopAppsParams{
		UserIDs: users,
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(fake.lastQuery, "user_sid IN ?") {
		t.Errorf("missing user_sid IN clause: %s", fake.lastQuery)
	}
	// The user slice must be passed as a single arg (clickhouse-go v2
	// expands slice→tuple server-side); confirm the last arg is our slice.
	lastArg := fake.lastArgs[len(fake.lastArgs)-1]
	got, ok := lastArg.([]string)
	if !ok || !reflect.DeepEqual(got, users) {
		t.Errorf("expected user slice as last arg, got %T:%v", lastArg, lastArg)
	}
}

func TestAggTopApps_CategoryFilterBindsArg(t *testing.T) {
	fake := &fakeRunner{rows: &emptyRows{}}
	c := newClientWithRunner(fake)

	_, err := c.AggTopApps(context.Background(), "t", TopAppsParams{
		Category: "productive",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(fake.lastQuery, "category") {
		t.Errorf("query should reference category: %s", fake.lastQuery)
	}
	// args: tenant, from, to, category
	if len(fake.lastArgs) < 4 {
		t.Fatalf("expected ≥4 args, got %d", len(fake.lastArgs))
	}
	if fake.lastArgs[3] != "productive" {
		t.Errorf("category arg must be 'productive', got %v", fake.lastArgs[3])
	}
}

func TestAggTopApps_MissingTableReturnsEmpty(t *testing.T) {
	fake := &fakeRunner{err: errors.New("code: 60, DB::Exception: Table default.events_raw does not exist (UNKNOWN_TABLE)")}
	c := newClientWithRunner(fake)

	rows, err := c.AggTopApps(context.Background(), "t", TopAppsParams{})
	if err != nil {
		t.Fatalf("expected nil err on missing table, got %v", err)
	}
	if rows == nil {
		t.Errorf("expected empty slice, got nil")
	}
	if len(rows) != 0 {
		t.Errorf("expected empty slice, got %d rows", len(rows))
	}
}

func TestAggTopApps_NilClientReturnsErr(t *testing.T) {
	var c *Client
	_, err := c.AggTopApps(context.Background(), "t", TopAppsParams{})
	if !errors.Is(err, ErrClientUnavailable) {
		t.Errorf("expected ErrClientUnavailable from nil client, got %v", err)
	}
}

func TestAggTopApps_NilRunnerReturnsErr(t *testing.T) {
	c := &Client{runner: nil}
	_, err := c.AggTopApps(context.Background(), "t", TopAppsParams{})
	if !errors.Is(err, ErrClientUnavailable) {
		t.Errorf("expected ErrClientUnavailable from nil runner, got %v", err)
	}
}

func TestAggIdleActive_TenantIDFirstArg(t *testing.T) {
	fake := &fakeRunner{rows: &emptyRows{}}
	c := newClientWithRunner(fake)

	_, err := c.AggIdleActive(context.Background(), "tenant-abc", IdleActiveParams{
		From: time.Now().Add(-24 * time.Hour), To: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if fake.lastArgs[0].(string) != "tenant-abc" {
		t.Errorf("tenant_id must be first arg")
	}
	if !strings.Contains(fake.lastQuery, fmt.Sprintf("LIMIT %d", aggQueryLimit)) {
		t.Errorf("hard LIMIT 10000 must be present: %s", fake.lastQuery)
	}
}

func TestAggIdleActive_NilClient(t *testing.T) {
	var c *Client
	_, err := c.AggIdleActive(context.Background(), "t", IdleActiveParams{})
	if !errors.Is(err, ErrClientUnavailable) {
		t.Errorf("expected ErrClientUnavailable, got %v", err)
	}
}

func TestAggIdleActive_MissingTableGraceful(t *testing.T) {
	fake := &fakeRunner{err: errors.New("code: 81, Database default does not exist")}
	c := newClientWithRunner(fake)
	rows, err := c.AggIdleActive(context.Background(), "t", IdleActiveParams{})
	if err != nil {
		t.Fatalf("expected graceful empty, got %v", err)
	}
	if rows == nil || len(rows) != 0 {
		t.Errorf("expected empty slice, got %v", rows)
	}
}

func TestAggProductivity_TenantAndLimit(t *testing.T) {
	fake := &fakeRunner{rows: &emptyRows{}}
	c := newClientWithRunner(fake)

	_, err := c.AggProductivityScore(context.Background(), "t", ProductivityParams{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if fake.lastArgs[0].(string) != "t" {
		t.Errorf("tenant_id must be first arg")
	}
	if !strings.Contains(fake.lastQuery, fmt.Sprintf("LIMIT %d", aggQueryLimit)) {
		t.Errorf("hard LIMIT missing: %s", fake.lastQuery)
	}
}

func TestAggProductivity_NilClient(t *testing.T) {
	var c *Client
	_, err := c.AggProductivityScore(context.Background(), "t", ProductivityParams{})
	if !errors.Is(err, ErrClientUnavailable) {
		t.Errorf("expected ErrClientUnavailable, got %v", err)
	}
}

func TestAggAppBlocks_TenantAndDefaults(t *testing.T) {
	fake := &fakeRunner{rows: &emptyRows{}}
	c := newClientWithRunner(fake)

	_, err := c.AggAppBlocks(context.Background(), "t", AppBlocksParams{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if fake.lastArgs[0].(string) != "t" {
		t.Errorf("tenant_id must be first arg")
	}
	if !strings.Contains(fake.lastQuery, "LIMIT 50") {
		t.Errorf("default limit 50 missing: %s", fake.lastQuery)
	}
}

func TestAggAppBlocks_LimitCapped(t *testing.T) {
	fake := &fakeRunner{rows: &emptyRows{}}
	c := newClientWithRunner(fake)
	_, err := c.AggAppBlocks(context.Background(), "t", AppBlocksParams{Limit: 5000})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(fake.lastQuery, "LIMIT 100") {
		t.Errorf("limit cap 100 missing: %s", fake.lastQuery)
	}
}

func TestAggAppBlocks_NilClient(t *testing.T) {
	var c *Client
	_, err := c.AggAppBlocks(context.Background(), "t", AppBlocksParams{})
	if !errors.Is(err, ErrClientUnavailable) {
		t.Errorf("expected ErrClientUnavailable, got %v", err)
	}
}

func TestAggAppBlocks_MissingTableGraceful(t *testing.T) {
	fake := &fakeRunner{err: errors.New("something about UNKNOWN_TABLE happened")}
	c := newClientWithRunner(fake)
	rows, err := c.AggAppBlocks(context.Background(), "t", AppBlocksParams{})
	if err != nil {
		t.Fatalf("graceful missing-table failed: %v", err)
	}
	if rows == nil || len(rows) != 0 {
		t.Errorf("expected empty slice, got %v", rows)
	}
}

// TestAggTopApps_NoLeadingInterpolation asserts the literal "tenant_id ="
// binding is present and that no tenant_id value ever appears inline in
// the query string (defence in depth against regressions that might
// accidentally introduce Sprintf-ing of untrusted input).
func TestAggTopApps_NoLeadingInterpolation(t *testing.T) {
	fake := &fakeRunner{rows: &emptyRows{}}
	c := newClientWithRunner(fake)

	evil := "'; DROP TABLE events_raw; --"
	_, err := c.AggTopApps(context.Background(), evil, TopAppsParams{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if strings.Contains(fake.lastQuery, "DROP TABLE") {
		t.Errorf("tenant_id interpolated into query (SQLi window!): %s", fake.lastQuery)
	}
	if fake.lastArgs[0].(string) != evil {
		t.Errorf("tenant_id must still be bound as parameter")
	}
}

func TestIsMissingTable(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("random error"), false},
		{errors.New("UNKNOWN_TABLE"), true},
		{errors.New("code: 60"), true},
		{errors.New("code: 81"), true},
		{errors.New("code: 60, DB::Exception: Table events_raw does not exist"), true},
	}
	for _, tc := range cases {
		if got := isMissingTable(tc.err); got != tc.want {
			t.Errorf("isMissingTable(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
