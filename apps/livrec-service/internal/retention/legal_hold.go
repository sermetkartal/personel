// Package retention — legal hold scaffolding.
//
// Per ADR 0019 §Storage: an active legal hold suspends the TTL lifecycle rule
// for affected recordings. The actual Postgres query is TBD (depends on the
// legal_holds schema which lives in the Admin API's migration set).
//
// SCAFFOLDED: This file defines the interface only. The real implementation
// will query Postgres `live_view_recordings.legal_hold_id IS NOT NULL` via
// the shared connection pool once the Postgres dependency is added to livrec.
package retention

import (
	"context"
)

// PostgresLegalHoldChecker implements LegalHoldChecker against Postgres.
// The actual SQL query is a stub pending Phase 3 Postgres wiring.
// See SCAFFOLDED note above.
type PostgresLegalHoldChecker struct {
	// db is the connection pool — TBD in Phase 3 when livrec gets its own
	// Postgres role with read access to live_view_recordings.
	// db *pgxpool.Pool
}

// NewPostgresLegalHoldChecker returns a PostgresLegalHoldChecker.
// db parameter is reserved for Phase 3; pass nil until then.
func NewPostgresLegalHoldChecker() *PostgresLegalHoldChecker {
	return &PostgresLegalHoldChecker{}
}

// IsOnLegalHold checks whether the session has an active legal hold.
// STUB: always returns false until Postgres query is wired in Phase 3.
// Replace this implementation with a real pgxpool query before Phase 3 launch.
func (c *PostgresLegalHoldChecker) IsOnLegalHold(_ context.Context, _ string) (bool, error) {
	// TODO(phase3): implement Postgres query:
	// SELECT COUNT(1) FROM live_view_recordings
	// WHERE session_id = $1 AND legal_hold_id IS NOT NULL;
	return false, nil
}
