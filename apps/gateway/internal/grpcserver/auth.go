// Package grpcserver implements the gRPC server for the Ingest Gateway,
// including mTLS auth, key-version handshake, rate limiting, and backpressure.
package grpcserver

import (
	"context"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/personel/gateway/internal/observability"
	"github.com/personel/gateway/internal/postgres"
	"github.com/personel/gateway/internal/vault"
)

// authContextKey is the key type for storing AuthInfo in context.
type authContextKey struct{}

// AuthInfo holds the validated identity extracted from the mTLS client cert,
// injected into the request context by the auth interceptor.
type AuthInfo struct {
	TenantID   string
	EndpointID string
	CertSerial string
}

// authInterceptor returns a gRPC unary + stream interceptor pair that performs
// mTLS certificate validation and endpoint lookup.
//
// The interceptor:
//  1. Extracts the client certificate from the TLS peer info.
//  2. Reads the cert serial and looks up the endpoint in Postgres.
//  3. Checks the Vault deny list for immediate revocations.
//  4. Injects AuthInfo into the context for downstream handlers.
func authInterceptor(
	db *postgres.Pool,
	vc *vault.Client,
	metrics *observability.Metrics,
	logger *slog.Logger,
) (grpc.UnaryServerInterceptor, grpc.StreamServerInterceptor) {
	validate := func(ctx context.Context) (context.Context, error) {
		p, ok := peer.FromContext(ctx)
		if !ok {
			metrics.AuthFailures.WithLabelValues("no_peer").Inc()
			return ctx, status.Error(codes.Unauthenticated, "no peer info in context")
		}

		tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
		if !ok {
			metrics.AuthFailures.WithLabelValues("no_tls").Inc()
			return ctx, status.Error(codes.Unauthenticated, "connection is not mTLS")
		}

		state := tlsInfo.State
		if len(state.PeerCertificates) == 0 {
			metrics.AuthFailures.WithLabelValues("no_client_cert").Inc()
			return ctx, status.Error(codes.Unauthenticated, "client certificate required")
		}

		cert := state.PeerCertificates[0]
		serial := certSerialHex(cert)

		// Check the in-memory deny list first (< 5 minute propagation target).
		if vc.IsRevoked(serial) {
			metrics.AuthFailures.WithLabelValues("revoked").Inc()
			logger.WarnContext(ctx, "auth: revoked cert rejected",
				slog.String("serial", serial),
			)
			return ctx, status.Error(codes.PermissionDenied, "certificate has been revoked")
		}

		// Look up the endpoint record in Postgres.
		rec, err := db.GetEndpointByCertSerial(ctx, serial)
		if err != nil {
			if err == postgres.ErrEndpointNotFound {
				metrics.AuthFailures.WithLabelValues("unknown_cert").Inc()
				logger.WarnContext(ctx, "auth: cert serial not found",
					slog.String("serial", serial),
				)
				return ctx, status.Error(codes.PermissionDenied, "endpoint not enrolled")
			}
			logger.ErrorContext(ctx, "auth: postgres lookup failed",
				slog.String("serial", serial),
				slog.String("error", err.Error()),
			)
			return ctx, status.Error(codes.Internal, "endpoint lookup failed")
		}

		if rec.Revoked {
			metrics.AuthFailures.WithLabelValues("revoked_db").Inc()
			// Also add to the in-memory deny list so subsequent lookups are fast.
			vc.AddToDenyList(serial)
			return ctx, status.Error(codes.PermissionDenied, "endpoint certificate revoked")
		}

		ai := AuthInfo{
			TenantID:   rec.TenantID.String(),
			EndpointID: rec.EndpointID.String(),
			CertSerial: serial,
		}
		return context.WithValue(ctx, authContextKey{}, ai), nil
	}

	unary := func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		ctx, err := validate(ctx)
		if err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}

	stream := func(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, err := validate(ss.Context())
		if err != nil {
			return err
		}
		return handler(srv, &wrappedStream{ss, ctx})
	}

	return unary, stream
}

// AuthInfoFromContext retrieves AuthInfo from the context injected by the auth
// interceptor. Returns an error if no auth info is present (should not happen
// if the interceptor is wired correctly).
func AuthInfoFromContext(ctx context.Context) (AuthInfo, error) {
	ai, ok := ctx.Value(authContextKey{}).(AuthInfo)
	if !ok {
		return AuthInfo{}, fmt.Errorf("auth: no AuthInfo in context")
	}
	return ai, nil
}

// certSerialHex returns a lowercase hex string representation of a certificate
// serial number, matching the format used in Vault PKI and the deny list.
func certSerialHex(cert *x509.Certificate) string {
	return strings.ToLower(hex.EncodeToString(cert.SerialNumber.Bytes()))
}

// wrappedStream overrides Context() to inject the enriched context into
// a grpc.ServerStream so stream handlers can use AuthInfoFromContext.
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context { return w.ctx }
