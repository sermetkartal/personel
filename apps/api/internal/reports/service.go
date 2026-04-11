// Package reports — service wrapping ClickHouse queries.
package reports

import (
	"context"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// Service wraps the Querier and provides business-level report operations.
type Service struct {
	q *Querier
}

// NewService creates the reports service.
func NewService(ch clickhouse.Conn) *Service {
	return &Service{q: NewQuerier(ch)}
}

// ProductivityTimeline delegates to the querier.
func (s *Service) ProductivityTimeline(ctx context.Context, tenantID string, from, to time.Time, endpointIDs []string) ([]ProductivityRow, error) {
	return s.q.ProductivityTimeline(ctx, tenantID, from, to, endpointIDs)
}

// TopApps delegates to the querier.
func (s *Service) TopApps(ctx context.Context, tenantID string, from, to time.Time, limit int) ([]TopAppRow, error) {
	return s.q.TopApps(ctx, tenantID, from, to, limit)
}

// IdleActive delegates to the querier.
func (s *Service) IdleActive(ctx context.Context, tenantID string, from, to time.Time, endpointIDs []string) ([]IdleActiveRow, error) {
	return s.q.IdleActive(ctx, tenantID, from, to, endpointIDs)
}

// EndpointActivitySummary delegates to the querier.
func (s *Service) EndpointActivitySummary(ctx context.Context, tenantID string, from, to time.Time) ([]map[string]any, error) {
	return s.q.EndpointActivitySummary(ctx, tenantID, from, to)
}

// AppBlocks delegates to the querier.
func (s *Service) AppBlocks(ctx context.Context, tenantID string, from, to time.Time) ([]AppBlockRow, error) {
	return s.q.AppBlocks(ctx, tenantID, from, to)
}
