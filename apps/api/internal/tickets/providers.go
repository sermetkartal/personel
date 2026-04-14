// Package tickets — provider adapters.
//
// Each provider implementation lives in this file for Phase 1 to keep
// the scaffold compact. If a customer adopts a specific provider and
// we need real HTTP clients, each will get its own file (jira.go,
// zendesk.go, freshdesk.go) and the stub here will be deleted.
package tickets

import (
	"context"
	"encoding/json"
	"log/slog"
)

// Provider name constants — keep in sync with the `tickets.provider`
// column CHECK constraint in migration 0034 (informal: CHECK is
// open, but we rely on these names in the Service registry).
const (
	ProviderNameInternal   = "internal"
	ProviderNameJira       = "jira"
	ProviderNameZendesk    = "zendesk"
	ProviderNameFreshdesk  = "freshdesk"
)

// InternalProvider is a zero-op provider that leaves the local
// shadow row as the only record. It is always registered so the
// Service can operate without any external integration.
type InternalProvider struct{}

func (InternalProvider) Name() string { return ProviderNameInternal }

func (InternalProvider) Create(_ context.Context, _ *Ticket) (string, error) {
	return "", nil
}

func (InternalProvider) Update(_ context.Context, _ *Ticket) error {
	return nil
}

func (InternalProvider) HandleWebhook(_ context.Context, _ []byte) (*Ticket, error) {
	// Internal provider doesn't have a real webhook path.
	return nil, ErrNotConfigured
}

// ------------------------------------------------------------------
// JiraProvider — STUB. Real implementation requires:
//   - Jira Cloud REST API v3 client (go-jira or bare http)
//   - OAuth2 basic auth header (email + api_token)
//   - Project key + issue type mapping
//   - Severity → priority mapping (P1→Highest, P2→High, P3→Medium, P4→Low)
//   - Webhook signature verification (HMAC)
// Wire when a customer commits to Jira.
// ------------------------------------------------------------------
type JiraProvider struct {
	BaseURL  string
	Email    string
	APIToken string
	Project  string
	Log      *slog.Logger
}

func (p *JiraProvider) Name() string { return ProviderNameJira }

func (p *JiraProvider) Create(_ context.Context, t *Ticket) (string, error) {
	p.logOrDefault().Warn("tickets: Jira Create — stub, pending implementation",
		slog.String("ticket_id", t.ID.String()),
		slog.String("base_url", p.BaseURL))
	return "", ErrNotConfigured
}

func (p *JiraProvider) Update(_ context.Context, t *Ticket) error {
	p.logOrDefault().Warn("tickets: Jira Update — stub",
		slog.String("ticket_id", t.ID.String()))
	return ErrNotConfigured
}

// HandleWebhook parses a Jira Cloud webhook payload. The stub
// recognises the "issue_updated" event shape enough to find the
// issue key, but does NOT verify the HMAC signature — add that
// check before ever enabling this provider in production.
func (p *JiraProvider) HandleWebhook(_ context.Context, payload []byte) (*Ticket, error) {
	var webhook struct {
		WebhookEvent string `json:"webhookEvent"`
		Issue        struct {
			Key    string `json:"key"`
			Fields struct {
				Status struct {
					Name string `json:"name"`
				} `json:"status"`
			} `json:"fields"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(payload, &webhook); err != nil {
		return nil, err
	}
	return &Ticket{
		ProviderID: webhook.Issue.Key,
		State:      jiraStatusToState(webhook.Issue.Fields.Status.Name),
	}, nil
}

func (p *JiraProvider) logOrDefault() *slog.Logger {
	if p.Log == nil {
		return slog.Default()
	}
	return p.Log
}

func jiraStatusToState(s string) State {
	switch s {
	case "Open", "To Do", "Backlog":
		return StateOpen
	case "In Progress", "Doing":
		return StateInProgress
	case "Done", "Resolved":
		return StateResolved
	case "Closed":
		return StateClosed
	default:
		return ""
	}
}

// ------------------------------------------------------------------
// ZendeskProvider — STUB. Real implementation requires:
//   - Zendesk REST API v2 client
//   - API token auth (email/token)
//   - Brand + group routing
//   - Severity → priority mapping
//   - Webhook HMAC signature verification
// ------------------------------------------------------------------
type ZendeskProvider struct {
	Subdomain string
	Email     string
	APIToken  string
	Log       *slog.Logger
}

func (p *ZendeskProvider) Name() string { return ProviderNameZendesk }

func (p *ZendeskProvider) Create(_ context.Context, t *Ticket) (string, error) {
	p.logOrDefault().Warn("tickets: Zendesk Create — stub",
		slog.String("ticket_id", t.ID.String()))
	return "", ErrNotConfigured
}

func (p *ZendeskProvider) Update(_ context.Context, _ *Ticket) error {
	return ErrNotConfigured
}

func (p *ZendeskProvider) HandleWebhook(_ context.Context, _ []byte) (*Ticket, error) {
	return nil, ErrNotConfigured
}

func (p *ZendeskProvider) logOrDefault() *slog.Logger {
	if p.Log == nil {
		return slog.Default()
	}
	return p.Log
}

// ------------------------------------------------------------------
// FreshdeskProvider — STUB. Real implementation requires:
//   - Freshdesk REST API v2 client
//   - API key basic auth
//   - Agent assignment + group routing
//   - Webhook verification
// ------------------------------------------------------------------
type FreshdeskProvider struct {
	Domain string
	APIKey string
	Log    *slog.Logger
}

func (p *FreshdeskProvider) Name() string { return ProviderNameFreshdesk }

func (p *FreshdeskProvider) Create(_ context.Context, t *Ticket) (string, error) {
	p.logOrDefault().Warn("tickets: Freshdesk Create — stub",
		slog.String("ticket_id", t.ID.String()))
	return "", ErrNotConfigured
}

func (p *FreshdeskProvider) Update(_ context.Context, _ *Ticket) error {
	return ErrNotConfigured
}

func (p *FreshdeskProvider) HandleWebhook(_ context.Context, _ []byte) (*Ticket, error) {
	return nil, ErrNotConfigured
}

func (p *FreshdeskProvider) logOrDefault() *slog.Logger {
	if p.Log == nil {
		return slog.Default()
	}
	return p.Log
}
