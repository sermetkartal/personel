// Package auth — fine-grained policy checks beyond the RBAC matrix.
// Used when a decision requires more context than (role, op, resource).
package auth

import "net/http"

// RequireRole is a middleware that rejects requests from principals not
// holding at least one of the given roles.
func RequireRole(roles ...Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := PrincipalFromContext(r.Context())
			if p == nil {
				http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
				return
			}
			for _, required := range roles {
				if HasRole(p, required) {
					next.ServeHTTP(w, r)
					return
				}
			}
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		})
	}
}

// RequireCan is a middleware that rejects if Can(p, op, resource) is false.
func RequireCan(op Op, resource Resource) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := PrincipalFromContext(r.Context())
			if p == nil {
				http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
				return
			}
			if !Can(p, op, resource) {
				http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AssertApproverDiffersFromRequester returns an error if approverID == requesterID.
// This is the core of the live view dual-control requirement.
func AssertApproverDiffersFromRequester(approverID, requesterID string) error {
	if approverID == "" || requesterID == "" {
		return ErrForbidden
	}
	if approverID == requesterID {
		return &authError{"approver must be a different person than the requester"}
	}
	return nil
}

// ScopeToOwnData ensures an employee-role principal can only access their own
// data. Returns an error if p.UserID != targetUserID.
func ScopeToOwnData(p *Principal, targetUserID string) error {
	if p == nil {
		return ErrForbidden
	}
	if HasRole(p, RoleAdmin) || HasRole(p, RoleDPO) || HasRole(p, RoleAuditor) {
		return nil // privileged roles bypass self-scope check
	}
	if p.UserID != targetUserID {
		return ErrForbidden
	}
	return nil
}
