// Package storage — MinIO object path layout for live view recordings.
//
// Per ADR 0019 §Storage:
//   Object path: live-view-recordings/<tenant_id>/<yyyy>/<mm>/<session_id>.webm.enc
//
// This package also defines the chunked object path used by livrec-service's
// ingest flow (browser sends chunks, not a single stream):
//   tenant/{tid}/session/{sid}/chunk-{N}.webm.enc
//
// Both layouts coexist: the top-level path is used for the completed assembly
// reference stored in Postgres; the chunked path is used during live ingest.
package storage

import (
	"fmt"
	"time"
)

// ChunkObjectKey returns the MinIO object key for a single encrypted WebM chunk.
// Format: tenant/{tenantID}/session/{sessionID}/chunk-{chunkIndex}.webm.enc
func ChunkObjectKey(tenantID, sessionID string, chunkIndex uint64) string {
	return fmt.Sprintf("tenant/%s/session/%s/chunk-%d.webm.enc", tenantID, sessionID, chunkIndex)
}

// SessionObjectKey returns the MinIO key for the completed session recording.
// Format: <tenantID>/<yyyy>/<mm>/<sessionID>.webm.enc
// This matches the ADR 0019 §Storage object path specification.
func SessionObjectKey(tenantID, sessionID string, startedAt time.Time) string {
	return fmt.Sprintf("%s/%04d/%02d/%s.webm.enc",
		tenantID,
		startedAt.UTC().Year(),
		startedAt.UTC().Month(),
		sessionID,
	)
}

// ChunkPrefix returns the MinIO prefix for all chunks belonging to a session.
// Used for listing chunks during playback stream assembly.
func ChunkPrefix(tenantID, sessionID string) string {
	return fmt.Sprintf("tenant/%s/session/%s/", tenantID, sessionID)
}
