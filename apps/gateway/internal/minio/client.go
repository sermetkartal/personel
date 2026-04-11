// Package minio wraps the MinIO Go client for blob storage operations.
package minio

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/personel/gateway/internal/config"
)

// Bucket name constants matching the architecture docs.
const (
	BucketScreenshots       = "screenshots"
	BucketScreenclips       = "screenclips"
	BucketKeystrokeBlobs    = "keystroke-blobs"
	BucketClipboardBlobs    = "clipboard-blobs"
	BucketDSRResponses      = "dsr-responses"
	BucketDestructionReports = "destruction-reports"
	BucketBackups           = "backups"

	// Sensitive prefix — shorter TTL lifecycle rules apply under this prefix.
	PrefixSensitive = "sensitive/"
)

// Client wraps minio.Client with domain helper methods.
type Client struct {
	mc     *minio.Client
	logger *slog.Logger
}

// New creates a new MinIO client and ensures required buckets exist.
func New(ctx context.Context, cfg config.MinIOConfig, logger *slog.Logger) (*Client, error) {
	mc, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("minio: new client: %w", err)
	}

	c := &Client{mc: mc, logger: logger}
	if err := c.ensureBuckets(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// ensureBuckets creates all required buckets if they do not exist.
func (c *Client) ensureBuckets(ctx context.Context) error {
	buckets := []string{
		BucketScreenshots,
		BucketScreenclips,
		BucketKeystrokeBlobs,
		BucketClipboardBlobs,
		BucketDSRResponses,
		BucketDestructionReports,
		BucketBackups,
	}
	for _, b := range buckets {
		exists, err := c.mc.BucketExists(ctx, b)
		if err != nil {
			return fmt.Errorf("minio: check bucket %q: %w", b, err)
		}
		if !exists {
			if err := c.mc.MakeBucket(ctx, b, minio.MakeBucketOptions{}); err != nil {
				return fmt.Errorf("minio: create bucket %q: %w", b, err)
			}
			c.logger.InfoContext(ctx, "minio: created bucket", slog.String("bucket", b))
		}
	}
	return nil
}

// ObjectExists returns true if the object at the given bucket/path exists.
func (c *Client) ObjectExists(ctx context.Context, bucket, objectKey string) (bool, error) {
	_, err := c.mc.StatObject(ctx, bucket, objectKey, minio.StatObjectOptions{})
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			return false, nil
		}
		return false, fmt.Errorf("minio: stat %s/%s: %w", bucket, objectKey, err)
	}
	return true, nil
}

// GetObjectURL returns the object path in the canonical minio:// format used
// in event payloads (e.g., minio://screenshots/tenant/endpoint/date/ulid.webp).
func GetObjectURL(bucket, objectKey string) string {
	return fmt.Sprintf("minio://%s/%s", bucket, objectKey)
}

// BucketForBlob returns the correct bucket name and object key prefix for a
// blob reference, taking the sensitive flag into account.
func BucketForBlob(blobType string, sensitive bool) (bucket, prefix string) {
	if sensitive {
		prefix = PrefixSensitive
	}
	switch blobType {
	case "screenshot":
		return BucketScreenshots, prefix + "screenshots/"
	case "screenclip":
		return BucketScreenclips, prefix + "screenclips/"
	case "keystroke":
		return BucketKeystrokeBlobs, prefix + "keystroke/"
	case "clipboard":
		return BucketClipboardBlobs, prefix + "clipboard/"
	default:
		return BucketScreenshots, prefix
	}
}
