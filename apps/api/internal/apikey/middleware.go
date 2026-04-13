// Package apikey — HTTP middleware that accepts the `Authorization:
// ApiKey <plaintext>` scheme as an alternative to Keycloak OIDC.
package apikey

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// authMethodKeyType is unexported so only this package can set the
// "auth_method" tag on a request context.
type authMethodKeyType struct{}

// scopesKeyType tags the verified scopes onto the context for
// downstream RequireScope checks.
type scopesKeyType struct{}

var (
	authMethodKey = authMethodKeyType{}
	scopesKey     = scopesKeyType{}
)

// AuthMethod returns "api_key" if the request was authenticated via
// ApiKey scheme, "oidc" if via a Keycloak JWT, or "" if no method
// has been attached to ctx.
func AuthMethod(ctx context.Context) string {
	v, _ := ctx.Value(authMethodKey).(string)
	return v
}

// Scopes returns the scope strings attached to an ApiKey-authenticated
// request. Returns nil for OIDC requests.
func Scopes(ctx context.Context) []string {
	v, _ := ctx.Value(scopesKey).([]string)
	return v
}

// Middleware accepts `Authorization: ApiKey <plaintext>` and rejects
// requests with any other scheme. On success it populates:
//
//   - auth.Principal (with AuthMethod tag = "api_key", TenantID from
//     the key row, Roles empty — scope check, not role check, is
//     what gates ApiKey endpoints)
//   - context value "auth_method" = "api_key"
//   - context value "scopes" = the key's scope list
//
// On failure every branch returns 401 with the same generic body so
// a caller cannot learn whether a key was unknown, revoked, or
// expired from the response alone.
func Middleware(svc *Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			plaintext := extractApiKey(r)
			if plaintext == "" {
				httpx.WriteError(w, r, http.StatusUnauthorized, httpx.ProblemTypeAuth, "Authentication Required", "err.unauthenticated")
				return
			}
			verified, err := svc.Verify(r.Context(), plaintext)
			if err != nil {
				// Uniform rejection — do not differentiate on errors.Is.
				_ = errors.Is(err, ErrInvalidKey)
				httpx.WriteError(w, r, http.StatusUnauthorized, httpx.ProblemTypeAuth, "Authentication Required", "err.unauthenticated")
				return
			}

			tenant := ""
			if verified.TenantID != nil {
				tenant = *verified.TenantID
			}
			p := &auth.Principal{
				UserID:   "service:" + verified.ID,
				TenantID: tenant,
				Username: verified.Name,
				Roles:    nil, // ApiKey callers are scope-gated, not role-gated
			}
			ctx := auth.WithPrincipal(r.Context(), p)
			ctx = context.WithValue(ctx, authMethodKey, "api_key")
			ctx = context.WithValue(ctx, scopesKey, verified.Scopes)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// extractApiKey parses `Authorization: ApiKey <plaintext>`. The scheme
// name is matched case-insensitively to tolerate clients that send
// "apikey" in lowercase. Returns "" when the header is missing or
// uses a different scheme.
func extractApiKey(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 {
		return ""
	}
	if !strings.EqualFold(parts[0], "ApiKey") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// RequireScope is a helper used by handlers mounted behind Middleware.
// It returns nil when the request's verified scopes include the
// requested one, or ErrMissingScope otherwise.
//
// Usage inside a handler:
//
//	if err := apikey.RequireScope(r.Context(), "events:ingest"); err != nil {
//	    httpx.WriteError(w, r, 403, ...)
//	    return
//	}
func RequireScope(ctx context.Context, required string) error {
	scopes := Scopes(ctx)
	for _, s := range scopes {
		if s == required {
			return nil
		}
	}
	return ErrMissingScope
}
