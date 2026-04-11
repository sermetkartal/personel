package grpcserver

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/google/uuid"
	personelv1 "github.com/personel/proto/personel/v1"

	"github.com/personel/gateway/internal/observability"
	"github.com/personel/gateway/internal/postgres"
)

// keyVersionHandshake validates the Hello message's key version fields against
// the expected versions stored in Postgres for this endpoint.
//
// Decision tree (from key-hierarchy.md §Key Version Handshake):
//   - tmk_version < expected:  stale TMK → send RotateCert(reason="rekey"), return KEY_VERSION_STALE.
//   - pe_dek_version < expected: stale PE-DEK → same.
//   - pe_dek_version > expected or tmk_version > expected: agent is AHEAD of server
//     which indicates possible replay/rollback → emit agent.tamper_detected HIGH, return error.
//   - versions match: return nil (accept EventBatch stream).
//
// The gateway NEVER decrypts; this is a plain integer comparison.
func keyVersionHandshake(
	ctx context.Context,
	hello *personelv1.Hello,
	db *postgres.Pool,
	metrics *observability.Metrics,
	logger *slog.Logger,
) error {
	ai, err := AuthInfoFromContext(ctx)
	if err != nil {
		return status.Error(codes.Internal, "key handshake: missing auth context")
	}

	endpointUUID, err := uuid.Parse(ai.EndpointID)
	if err != nil {
		return status.Errorf(codes.Internal, "key handshake: invalid endpoint_id: %v", err)
	}

	kv, err := db.GetKeyVersions(ctx, endpointUUID)
	if err != nil {
		logger.ErrorContext(ctx, "handshake: key version lookup failed",
			slog.String("endpoint_id", ai.EndpointID),
			slog.String("error", err.Error()),
		)
		return status.Error(codes.Internal, "key version lookup failed")
	}

	agentPEDEK := hello.GetPeDekVersion()
	agentTMK := hello.GetTmkVersion()
	expectedPEDEK := kv.ExpectedPEDEKVersion
	expectedTMK := kv.ExpectedTMKVersion

	// Agent is ahead of server — should never happen under correct operation.
	if agentPEDEK > expectedPEDEK || agentTMK > expectedTMK {
		metrics.KeyVersionMismatch.WithLabelValues(ai.TenantID, "agent_ahead").Inc()
		logger.WarnContext(ctx, "handshake: KEY_VERSION_AGENT_AHEAD — possible tamper",
			slog.String("tenant_id", ai.TenantID),
			slog.String("endpoint_id", ai.EndpointID),
			slog.Uint64("agent_pe_dek", uint64(agentPEDEK)),
			slog.Uint64("expected_pe_dek", uint64(expectedPEDEK)),
			slog.Uint64("agent_tmk", uint64(agentTMK)),
			slog.Uint64("expected_tmk", uint64(expectedTMK)),
		)
		tenantUUID, _ := uuid.Parse(ai.TenantID)
		epUUID := endpointUUID
		_ = db.WriteAuditEntry(ctx, postgres.AuditEntry{
			TenantID:   &tenantUUID,
			EndpointID: &epUUID,
			EventType:  "agent.tamper_detected.key_version_ahead",
			Details: map[string]any{
				"agent_pe_dek_version":    agentPEDEK,
				"expected_pe_dek_version": expectedPEDEK,
				"agent_tmk_version":       agentTMK,
				"expected_tmk_version":    expectedTMK,
			},
		})
		return status.Error(codes.PermissionDenied, "KEY_VERSION_STALE: agent version ahead of server")
	}

	// TMK has been rotated while endpoint was offline.
	if agentTMK < expectedTMK {
		metrics.KeyVersionMismatch.WithLabelValues(ai.TenantID, "tmk_stale").Inc()
		logger.InfoContext(ctx, "handshake: TMK version stale, requesting rekey",
			slog.String("tenant_id", ai.TenantID),
			slog.String("endpoint_id", ai.EndpointID),
			slog.Uint64("agent_tmk", uint64(agentTMK)),
			slog.Uint64("expected_tmk", uint64(expectedTMK)),
		)
		return fmt.Errorf("%w: TMK rotated; endpoint must re-enroll (reason=rekey)", errKeyVersionStale)
	}

	// PE-DEK has been rotated (DPO forced rotation, suspected compromise).
	if agentPEDEK < expectedPEDEK {
		metrics.KeyVersionMismatch.WithLabelValues(ai.TenantID, "pe_dek_stale").Inc()
		logger.InfoContext(ctx, "handshake: PE-DEK version stale, requesting rekey",
			slog.String("tenant_id", ai.TenantID),
			slog.String("endpoint_id", ai.EndpointID),
			slog.Uint64("agent_pe_dek", uint64(agentPEDEK)),
			slog.Uint64("expected_pe_dek", uint64(expectedPEDEK)),
		)
		return fmt.Errorf("%w: PE-DEK rotated; endpoint must re-enroll (reason=rekey)", errKeyVersionStale)
	}

	// All versions match.
	return nil
}

// errKeyVersionStale is a sentinel error indicating that the agent's key
// version is stale and it must re-enroll before the gateway will accept batches.
var errKeyVersionStale = fmt.Errorf("KEY_VERSION_STALE")

// isKeyVersionStale returns true if err wraps errKeyVersionStale.
func isKeyVersionStale(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), errKeyVersionStale.Error())
}
