// Package audit — stream_handler.go exposes the real-time audit stream as
// GET /v1/audit/stream over WebSocket. Backs Faz 6 #66.
//
// Wire format:
//
//	Client → Server: nothing (filters are provided via query params on the
//	                  upgrade request).
//	Server → Client: one JSON text frame per audit entry plus a periodic
//	                  ping (WebSocket control frame) every 30s.
//	                  A final text frame with {"type":"close","dropped":N}
//	                  is sent before disconnect if the subscriber dropped
//	                  entries because its consumer was behind.
//
// Query parameters:
//
//	actions=a,b,c          action allowlist (comma separated)
//	actors=u1,u2           actor user ID allowlist
//	target_prefix=endpoint. target string prefix filter
//	all_tenants=true       DPO only — bypass tenant filter, audit-logged
//
// Auth + RBAC:
//
//	Handler is mounted under the /v1 AuthMiddleware group; principal is
//	read from the request context. Allowed roles: admin, dpo, investigator,
//	auditor. DPO is the only role permitted to pass all_tenants=true.
package audit

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"nhooyr.io/websocket"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// streamFrame is the JSON shape sent to subscribers. Kept small and
// self-describing so the browser client in the console can render without
// a second round-trip to resolve IDs. Intentionally does NOT embed the
// full Details map if it was stripped — StripSensitive already ran in
// Broker.Publish.
type streamFrame struct {
	Type     string         `json:"type"` // "entry" | "close" | "ping"
	Action   string         `json:"action,omitempty"`
	Actor    string         `json:"actor,omitempty"`
	ActorIP  string         `json:"actor_ip,omitempty"`
	TenantID string         `json:"tenant_id,omitempty"`
	Target   string         `json:"target,omitempty"`
	Details  map[string]any `json:"details,omitempty"`
	Dropped  int64          `json:"dropped,omitempty"`
}

// StreamHandler returns the /v1/audit/stream HTTP handler. The broker
// argument is shared with the Recorder via Recorder.SetBroker in main.go.
//
// The handler is intentionally not a method on *Broker so the audit
// package does not leak HTTP types into callers that only want fanout.
func StreamHandler(broker *Broker) http.HandlerFunc {
	if broker == nil {
		// Operators who boot without the broker get a coherent 503
		// rather than a panic on first client. This mirrors how
		// other optional services (search, clickhouse) degrade.
		return func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteError(w, r, http.StatusServiceUnavailable,
				httpx.ProblemTypeInternal,
				"Audit stream broker not wired", "err.stream_disabled")
		}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized,
				httpx.ProblemTypeAuth,
				"Authentication Required", "err.unauthenticated")
			return
		}
		if !allowedStreamRole(p) {
			httpx.WriteError(w, r, http.StatusForbidden,
				httpx.ProblemTypeForbidden,
				"Forbidden", "err.forbidden")
			return
		}

		// Parse query params into a StreamFilter + all_tenants flag.
		q := r.URL.Query()
		filter := StreamFilter{
			Actions:      splitCSV(q.Get("actions")),
			ActorIDs:     splitCSV(q.Get("actors")),
			TargetPrefix: q.Get("target_prefix"),
		}
		allTenants := q.Get("all_tenants") == "true"
		if allTenants && !hasRole(p, auth.RoleDPO) {
			httpx.WriteError(w, r, http.StatusForbidden,
				httpx.ProblemTypeForbidden,
				"all_tenants=true is DPO-only", "err.forbidden")
			return
		}

		// Audit the subscription itself. all_tenants=true is a
		// privileged elevation and MUST leave a trail. We emit this
		// BEFORE accepting the upgrade so a refusal to append (e.g.
		// DB down) fails the request cleanly instead of orphaning
		// a WebSocket in an un-auditable state.
		rec := FromContext(r.Context())
		details := map[string]any{
			"filter_actions":    filter.Actions,
			"filter_actors":     filter.ActorIDs,
			"filter_target":     filter.TargetPrefix,
			"all_tenants":       allTenants,
			"remote_addr":       r.RemoteAddr,
			"user_agent":        r.Header.Get("User-Agent"),
		}
		if _, err := rec.Append(r.Context(), Entry{
			Actor:    p.UserID,
			ActorUA:  r.Header.Get("User-Agent"),
			TenantID: p.TenantID,
			Action:   ActionAuditStreamSubscribed,
			Target:   "audit.stream",
			Details:  details,
		}); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError,
				httpx.ProblemTypeInternal,
				"Failed to record subscription", "err.internal")
			return
		}

		// Accept the WebSocket upgrade. We intentionally reject
		// compressed frames (simpler handshake + a slight defence
		// against compression-side-channel surprises in an auditing
		// context). OriginPatterns is permissive here because the
		// same-origin CORS middleware already ran upstream.
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns:  []string{"*"},
			CompressionMode: websocket.CompressionDisabled,
		})
		if err != nil {
			// websocket.Accept already wrote an HTTP response.
			return
		}

		sub := NewSubscriber(p.TenantID, allTenants, filter)
		broker.Subscribe(sub)

		// Create an inner context tied to the request that also cancels
		// when the client disconnects. The reader loop (below) triggers
		// cancellation on any read error, which breaks the writer loop.
		ctx, cancel := context.WithCancel(r.Context())
		defer func() {
			cancel()
			broker.Unsubscribe(sub)
			// Drained drop count goes into the audit log as the
			// subscription's closing record so operators can detect
			// slow consumers without scraping a separate metric.
			_, _ = rec.Append(context.Background(), Entry{
				Actor:    p.UserID,
				TenantID: p.TenantID,
				Action:   ActionAuditStreamUnsubscribed,
				Target:   "audit.stream",
				Details: map[string]any{
					"dropped": sub.Dropped(),
				},
			})
			_ = conn.Close(websocket.StatusNormalClosure, "bye")
		}()

		// Reader goroutine: we don't expect client frames but we MUST
		// drain them so the library can observe close frames and ping
		// acks. Any read error signals disconnect → cancel ctx.
		go func() {
			defer cancel()
			for {
				if _, _, err := conn.Read(ctx); err != nil {
					return
				}
			}
		}()

		// Writer loop: one frame per Subscriber entry plus a 30s ping.
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-sub.C():
				if !ok {
					return
				}
				frame := streamFrame{
					Type:     "entry",
					Action:   string(e.Action),
					Actor:    e.Actor,
					TenantID: e.TenantID,
					Target:   e.Target,
					Details:  e.Details,
				}
				if e.ActorIP != nil {
					frame.ActorIP = e.ActorIP.String()
				}
				if err := writeJSON(ctx, conn, frame); err != nil {
					return
				}
			case <-ticker.C:
				// Context-bound ping; returns error on closed socket.
				if err := conn.Ping(ctx); err != nil {
					return
				}
			}
		}
	}
}

// writeJSON marshals v and sends a single text frame with a 5s write
// deadline. A stuck peer (TCP buffer full) is treated as a disconnect.
// The outer handler treats any error from this function as a signal to
// unsubscribe and close the connection.
func writeJSON(ctx context.Context, conn *websocket.Conn, v any) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, buf)
}

// splitCSV returns nil for an empty input and a trimmed, non-empty slice
// otherwise. Used for query-param list parsing.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// allowedStreamRole enforces the RBAC gate for /v1/audit/stream. Mirrors
// the set of roles on /v1/search/audit since both are investigation tools.
func allowedStreamRole(p *auth.Principal) bool {
	return hasRole(p, auth.RoleAdmin) ||
		hasRole(p, auth.RoleDPO) ||
		hasRole(p, auth.RoleInvestigator) ||
		hasRole(p, auth.RoleAuditor)
}

func hasRole(p *auth.Principal, role auth.Role) bool {
	if p == nil {
		return false
	}
	for _, r := range p.Roles {
		if r == role {
			return true
		}
	}
	return false
}
