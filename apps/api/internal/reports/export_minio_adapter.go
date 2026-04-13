// export_minio_adapter.go — production wiring helper for the exporter's
// MinioUploader interface against the shared minio-go client.
//
// The existing `internal/minio.Client` infers the bucket from the object key
// prefix, which doesn't work for a new `reports-exports` bucket introduced
// by this package. Rather than broaden that client's surface, we keep the
// export-specific SDK usage here, scoped to a single file, and wire a fresh
// miniogo.Client into the Exporter at startup.
//
// This file depends only on the stock minio-go SDK — it does not touch the
// existing `internal/minio` package.
package reports

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"time"

	miniogo "github.com/minio/minio-go/v7"
)

// MinioExportClient is a thin adapter wrapping a miniogo.Client for the
// reports export bucket.
type MinioExportClient struct {
	inner *miniogo.Client
}

// NewMinioExportClient constructs the adapter. Callers supply an
// already-configured miniogo.Client (same endpoint / creds as the screenshots
// client, just used with a distinct bucket).
func NewMinioExportClient(inner *miniogo.Client) *MinioExportClient {
	return &MinioExportClient{inner: inner}
}

// EnsureBucket creates the reports-exports bucket if it does not exist.
// Safe to call on every startup — idempotent.
func (m *MinioExportClient) EnsureBucket(ctx context.Context, bucket, region string) error {
	ok, err := m.inner.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("reports: bucket exists check: %w", err)
	}
	if ok {
		return nil
	}
	if err := m.inner.MakeBucket(ctx, bucket, miniogo.MakeBucketOptions{Region: region}); err != nil {
		return fmt.Errorf("reports: create bucket %s: %w", bucket, err)
	}
	return nil
}

// PutObject uploads bytes to the specified bucket / key.
func (m *MinioExportClient) PutObject(ctx context.Context, bucket, key string, data []byte, contentType string) error {
	_, err := m.inner.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)),
		miniogo.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("reports: put %s/%s: %w", bucket, key, err)
	}
	return nil
}

// PresignedGetURL generates a short-lived presigned GET URL. TTL is capped
// at ExportPresignedTTL by the Exporter — no need to double-check here.
func (m *MinioExportClient) PresignedGetURL(ctx context.Context, bucket, key string, ttl time.Duration) (string, error) {
	u, err := m.inner.PresignedGetObject(ctx, bucket, key, ttl, url.Values{})
	if err != nil {
		return "", fmt.Errorf("reports: presign %s/%s: %w", bucket, key, err)
	}
	return u.String(), nil
}
