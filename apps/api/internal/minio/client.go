// Package minio — MinIO presigned URL issuer and object uploader.
// Never returns raw object content for screenshots — only presigned URLs.
package minio

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	miniogo "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Client wraps the MinIO SDK.
type Client struct {
	inner             *miniogo.Client
	bucketScreenshots string
	bucketDSR         string
	bucketDestruction string
	log               *slog.Logger
}

// New creates and initialises the MinIO client.
func New(endpoint, accessKey, secretKey string, useSSL bool, bucketScreenshots, bucketDSR, bucketDestruction string, log *slog.Logger) (*Client, error) {
	mc, err := miniogo.New(endpoint, &miniogo.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio: new client: %w", err)
	}
	return &Client{
		inner:             mc,
		bucketScreenshots: bucketScreenshots,
		bucketDSR:         bucketDSR,
		bucketDestruction: bucketDestruction,
		log:               log,
	}, nil
}

// PresignedGetURL generates a short-lived presigned GET URL for a MinIO object.
// TTL should be <= 60 seconds for screenshots (enforced by the screenshots service).
func (c *Client) PresignedGetURL(ctx context.Context, objectKey string, ttl time.Duration) (string, error) {
	if ttl > 5*time.Minute {
		return "", fmt.Errorf("minio: presigned URL TTL too long (%s); max 5 minutes", ttl)
	}

	bucket := c.bucketForKey(objectKey)
	reqParams := make(url.Values)
	presigned, err := c.inner.PresignedGetObject(ctx, bucket, objectKey, ttl, reqParams)
	if err != nil {
		return "", fmt.Errorf("minio: presign %s: %w", objectKey, err)
	}
	return presigned.String(), nil
}

// PutObject uploads bytes to the specified path.
func (c *Client) PutObject(ctx context.Context, objectKey string, data []byte, contentType string) error {
	bucket := c.bucketForKey(objectKey)
	_, err := c.inner.PutObject(ctx, bucket, objectKey, bytes.NewReader(data), int64(len(data)),
		miniogo.PutObjectOptions{ContentType: contentType},
	)
	if err != nil {
		return fmt.Errorf("minio: put %s: %w", objectKey, err)
	}
	return nil
}

// bucketForKey infers the bucket from the object key prefix.
// kvkk/ KVKK compliance documents (DPA, DPIA, açık rıza PDFs) share the
// DSR bucket — both are long-retention tamper-evident legal artifacts and
// splitting them into a new bucket adds ops burden without a KVKK
// requirement.
func (c *Client) bucketForKey(key string) string {
	switch {
	case len(key) >= 4 && key[:4] == "dsr-":
		return c.bucketDSR
	case len(key) >= 5 && key[:5] == "kvkk/":
		return c.bucketDSR
	case len(key) >= 11 && key[:11] == "destruction":
		return c.bucketDestruction
	default:
		return c.bucketScreenshots
	}
}
