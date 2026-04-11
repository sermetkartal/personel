// Package storage — MinIO client for live view recording chunks.
//
// Per ADR 0019:
//   - Bucket: live-view-recordings
//   - Write service account: s3:PutObject only (no GetObject, no DeleteObject)
//   - Read service account: s3:GetObject scoped by application-layer playback approval
//   - Lifecycle: 30-day default; legal-hold suspends lifecycle
//
// This client runs as the write-side (ingest + export read-back only).
package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Client wraps the MinIO Go SDK client.
type Client struct {
	mc     *minio.Client
	bucket string
	log    *slog.Logger
}

// NewClient creates and returns a MinIO client, and ensures the recording
// bucket exists with the correct settings.
func NewClient(ctx context.Context, endpoint, accessKey, secretKey, bucket string, useTLS bool, log *slog.Logger) (*Client, error) {
	mc, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useTLS,
	})
	if err != nil {
		return nil, fmt.Errorf("minio: new client: %w", err)
	}

	c := &Client{mc: mc, bucket: bucket, log: log}

	if err := c.ensureBucket(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// ensureBucket creates the recording bucket if it does not exist.
// The 30-day lifecycle policy is applied at bucket creation (or left to the
// retention scheduler which manages individual object tags).
func (c *Client) ensureBucket(ctx context.Context) error {
	exists, err := c.mc.BucketExists(ctx, c.bucket)
	if err != nil {
		return fmt.Errorf("minio: bucket exists check: %w", err)
	}
	if exists {
		return nil
	}
	if err := c.mc.MakeBucket(ctx, c.bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("minio: make bucket %q: %w", c.bucket, err)
	}
	c.log.Info("minio: bucket created", slog.String("bucket", c.bucket))
	return nil
}

// PutChunk stores an encrypted chunk at the given object key.
// The data bytes are the raw ciphertext (nonce+ciphertext+tag from envelope.go).
// contentType is application/octet-stream.
func (c *Client) PutChunk(ctx context.Context, objectKey string, data []byte) error {
	_, err := c.mc.PutObject(ctx, c.bucket, objectKey, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if err != nil {
		return fmt.Errorf("minio: put chunk %q: %w", objectKey, err)
	}
	return nil
}

// GetChunk retrieves an encrypted chunk by object key.
// The caller is responsible for closing the returned ReadCloser.
func (c *Client) GetChunk(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	obj, err := c.mc.GetObject(ctx, c.bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("minio: get chunk %q: %w", objectKey, err)
	}
	return obj, nil
}

// GetChunkBytes retrieves an encrypted chunk and reads it fully into memory.
// For chunks up to 1 MiB (ADR 0019 chunk size) this is acceptable.
func (c *Client) GetChunkBytes(ctx context.Context, objectKey string) ([]byte, error) {
	rc, err := c.GetChunk(ctx, objectKey)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("minio: read chunk %q: %w", objectKey, err)
	}
	return data, nil
}

// ListChunks returns all object keys under the given prefix, sorted by key
// (which sorts by chunk index due to the naming scheme).
func (c *Client) ListChunks(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	for obj := range c.mc.ListObjects(ctx, c.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: false,
	}) {
		if obj.Err != nil {
			return nil, fmt.Errorf("minio: list objects prefix %q: %w", prefix, obj.Err)
		}
		keys = append(keys, obj.Key)
	}
	return keys, nil
}

// DeleteObject removes an object. Used by the TTL scheduler only after
// confirming no legal hold is active.
func (c *Client) DeleteObject(ctx context.Context, objectKey string) error {
	if err := c.mc.RemoveObject(ctx, c.bucket, objectKey, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("minio: delete %q: %w", objectKey, err)
	}
	return nil
}
