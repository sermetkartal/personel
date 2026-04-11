package minio

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/minio/minio-go/v7/pkg/lifecycle"
)

// LifecycleRule maps bucket/prefix pairs to expiration days.
type LifecycleRule struct {
	Bucket     string
	Prefix     string
	ExpiryDays int
	RuleID     string
}

// DefaultLifecycleRules returns the retention policy rules per the
// data-retention-matrix.md (normal TTLs). Sensitive-flagged rules use
// shorter TTLs and the sensitive/ prefix.
func DefaultLifecycleRules() []LifecycleRule {
	return []LifecycleRule{
		// Normal retention
		{Bucket: BucketScreenshots, Prefix: "screenshots/", ExpiryDays: 30, RuleID: "screenshots-normal-30d"},
		{Bucket: BucketScreenclips, Prefix: "screenclips/", ExpiryDays: 14, RuleID: "screenclips-normal-14d"},
		{Bucket: BucketKeystrokeBlobs, Prefix: "keystroke/", ExpiryDays: 14, RuleID: "keystroke-normal-14d"},
		{Bucket: BucketClipboardBlobs, Prefix: "clipboard/", ExpiryDays: 30, RuleID: "clipboard-normal-30d"},

		// Sensitive-flagged (shorter TTL, sensitive/ prefix — KVKK m.6)
		{Bucket: BucketScreenshots, Prefix: "sensitive/screenshots/", ExpiryDays: 7, RuleID: "screenshots-sensitive-7d"},
		{Bucket: BucketScreenclips, Prefix: "sensitive/screenclips/", ExpiryDays: 7, RuleID: "screenclips-sensitive-7d"},
		{Bucket: BucketKeystrokeBlobs, Prefix: "sensitive/keystroke/", ExpiryDays: 7, RuleID: "keystroke-sensitive-7d"},
		{Bucket: BucketClipboardBlobs, Prefix: "sensitive/clipboard/", ExpiryDays: 7, RuleID: "clipboard-sensitive-7d"},
	}
}

// BootstrapLifecycle applies lifecycle rules to all buckets. Idempotent.
func (c *Client) BootstrapLifecycle(ctx context.Context) error {
	// Group rules by bucket.
	byBucket := make(map[string][]LifecycleRule)
	for _, r := range DefaultLifecycleRules() {
		byBucket[r.Bucket] = append(byBucket[r.Bucket], r)
	}

	for bucket, rules := range byBucket {
		cfg := lifecycle.NewConfiguration()
		for _, r := range rules {
			days := uint(r.ExpiryDays)
			cfg.Rules = append(cfg.Rules, lifecycle.Rule{
				ID:     r.RuleID,
				Status: "Enabled",
				RuleFilter: lifecycle.Filter{
					Prefix: r.Prefix,
				},
				Expiration: lifecycle.Expiration{
					Days: lifecycle.ExpirationDays(days),
				},
			})
		}
		if err := c.mc.SetBucketLifecycle(ctx, bucket, cfg); err != nil {
			return fmt.Errorf("minio lifecycle: set rules for bucket %q: %w", bucket, err)
		}
		c.logger.InfoContext(ctx, "minio lifecycle: applied rules",
			slog.String("bucket", bucket),
			slog.Int("rule_count", len(rules)),
		)
	}
	return nil
}
