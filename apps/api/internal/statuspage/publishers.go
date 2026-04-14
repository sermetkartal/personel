// Package statuspage — external publisher adapters (stubs).
package statuspage

import (
	"context"
	"log/slog"
)

// StatuspageIOPublisher — STUB. Real implementation requires:
//   - statuspage.io REST API v1 client
//   - OAuth2 token
//   - Page ID + Component ID mapping
//   - Incident creation/update verbs mapped to Personel lifecycle
// Wire when a customer commits to statuspage.io.
type StatuspageIOPublisher struct {
	PageID   string
	APIToken string
	Log      *slog.Logger
}

func (p *StatuspageIOPublisher) Name() string { return "statuspage.io" }

func (p *StatuspageIOPublisher) Publish(_ context.Context, _ PublicStatus) error {
	if p.Log == nil {
		p.Log = slog.Default()
	}
	p.Log.Warn("statuspage: statuspage.io publisher — stub, pending implementation",
		slog.String("page_id", p.PageID))
	return ErrNotConfigured
}

// InstatusPublisher — STUB. Similar shape to statuspage.io.
type InstatusPublisher struct {
	PageID   string
	APIToken string
	Log      *slog.Logger
}

func (p *InstatusPublisher) Name() string { return "instatus" }

func (p *InstatusPublisher) Publish(_ context.Context, _ PublicStatus) error {
	if p.Log == nil {
		p.Log = slog.Default()
	}
	p.Log.Warn("statuspage: instatus publisher — stub", slog.String("page_id", p.PageID))
	return ErrNotConfigured
}
