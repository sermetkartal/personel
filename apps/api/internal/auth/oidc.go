// Package auth — Keycloak OIDC verifier.
// Verifies JWTs issued by the Keycloak realm configured in KeycloakConfig.
// Extracts claims, populates request context with the verified principal.
package auth

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Principal is the verified identity extracted from the Keycloak JWT.
type Principal struct {
	UserID   string   // sub claim (Keycloak user UUID)
	TenantID string   // custom claim: tenant_id
	Username string   // preferred_username
	Email    string
	Roles    []Role   // from realm_access.roles or resource_access.<client>.roles
	// Raw token for pass-through to NATS / Vault if needed.
	AccessToken string
}

type principalKey struct{}

// WithPrincipal stores the principal in ctx.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFromContext retrieves the principal. Returns nil if not set.
func PrincipalFromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey{}).(*Principal)
	return p
}

// Verifier wraps go-oidc provider and verifier.
type Verifier struct {
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	clientID string
	log      *slog.Logger
}

// NewVerifier creates and initialises the OIDC verifier by fetching the
// Keycloak discovery document. ctx must allow network access.
func NewVerifier(ctx context.Context, issuerURL, clientID string, log *slog.Logger) (*Verifier, error) {
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc: discover provider %s: %w", issuerURL, err)
	}

	verifier := provider.Verifier(&oidc.Config{
		ClientID: clientID,
	})

	return &Verifier{
		provider: provider,
		verifier: verifier,
		clientID: clientID,
		log:      log,
	}, nil
}

// VerifyRequest extracts and verifies the Bearer token from r, returning the
// verified Principal. Returns an error if no token, invalid, or expired.
func (v *Verifier) VerifyRequest(r *http.Request) (*Principal, error) {
	rawToken := extractBearerToken(r)
	if rawToken == "" {
		return nil, ErrNoToken
	}
	return v.VerifyToken(r.Context(), rawToken)
}

// VerifyToken verifies a raw access token string.
func (v *Verifier) VerifyToken(ctx context.Context, rawToken string) (*Principal, error) {
	idToken, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	var claims keycloakClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("oidc: claims extract: %w", err)
	}

	if claims.Sub == "" {
		return nil, fmt.Errorf("%w: missing sub", ErrInvalidToken)
	}

	roles := extractRoles(claims, v.clientID)

	p := &Principal{
		UserID:      claims.Sub,
		TenantID:    claims.TenantID,
		Username:    claims.PreferredUsername,
		Email:       claims.Email,
		Roles:       roles,
		AccessToken: rawToken,
	}

	return p, nil
}

// keycloakClaims maps the Keycloak JWT structure.
type keycloakClaims struct {
	Sub               string   `json:"sub"`
	PreferredUsername string   `json:"preferred_username"`
	Email             string   `json:"email"`
	TenantID          string   `json:"tenant_id"` // custom claim added by Keycloak mapper
	Iat               int64    `json:"iat"`
	Exp               int64    `json:"exp"`

	RealmAccess struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`

	ResourceAccess map[string]struct {
		Roles []string `json:"roles"`
	} `json:"resource_access"`
}

// extractRoles returns all unique roles from realm_access and
// resource_access[clientID].
func extractRoles(c keycloakClaims, clientID string) []Role {
	seen := make(map[Role]struct{})
	for _, r := range c.RealmAccess.Roles {
		if role, ok := parseRole(r); ok {
			seen[role] = struct{}{}
		}
	}
	if ra, ok := c.ResourceAccess[clientID]; ok {
		for _, r := range ra.Roles {
			if role, ok := parseRole(r); ok {
				seen[role] = struct{}{}
			}
		}
	}
	roles := make([]Role, 0, len(seen))
	for r := range seen {
		roles = append(roles, r)
	}
	return roles
}

func extractBearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// TokenSource implements oauth2.TokenSource for downstream calls.
func (p *Principal) TokenSource() oauth2.TokenSource {
	return oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: p.AccessToken,
		Expiry:      time.Now().Add(5 * time.Minute), // approximate
	})
}

// Sentinel errors.
var (
	ErrNoToken     = &authError{"no bearer token in request"}
	ErrInvalidToken = &authError{"invalid or expired token"}
	ErrForbidden   = &authError{"insufficient permissions"}
)

type authError struct{ msg string }

func (e *authError) Error() string { return e.msg }
