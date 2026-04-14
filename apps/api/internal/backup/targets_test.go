package backup

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

type targetFakeVault struct {
	encCalls int
	decCalls int
}

func (f *targetFakeVault) Encrypt(_ context.Context, key string, pt []byte) ([]byte, int, error) {
	f.encCalls++
	out := []byte("stub:" + key + ":" + base64.StdEncoding.EncodeToString(pt))
	return out, 7, nil
}

func (f *targetFakeVault) Decrypt(_ context.Context, key string, ct []byte) ([]byte, error) {
	f.decCalls++
	prefix := []byte("stub:" + key + ":")
	if len(ct) < len(prefix) {
		return nil, fmt.Errorf("bad ct")
	}
	return base64.StdEncoding.DecodeString(string(ct[len(prefix):]))
}

// --- maskTargetConfig ---

func TestMaskTargetConfigMasksSecretLikeKeys(t *testing.T) {
	in := map[string]any{
		"bucket":         "my-bucket",
		"region":         "eu-west-1",
		"access_key":     "AKIA...",
		"secret_access_key": "xxx",
		"endpoint":       "https://s3.example.com",
		"password":       "hunter2",
		"api_token":      "tok",
	}
	out := maskTargetConfig(in)
	if out["bucket"] != "my-bucket" {
		t.Errorf("bucket should pass through, got %v", out["bucket"])
	}
	if out["region"] != "eu-west-1" {
		t.Errorf("region should pass through, got %v", out["region"])
	}
	if out["endpoint"] != "https://s3.example.com" {
		t.Errorf("endpoint should pass through, got %v", out["endpoint"])
	}
	// secret-like fields
	for _, k := range []string{"access_key", "secret_access_key", "password", "api_token"} {
		if out[k] != MaskedValue {
			t.Errorf("%s should be masked, got %v", k, out[k])
		}
	}
}

// --- looksSecret ---

func TestLooksSecretHints(t *testing.T) {
	for _, c := range []struct {
		key    string
		expect bool
	}{
		{"password", true},
		{"PASSWORD", true},
		{"api_token", true},
		{"secret_access_key", true},
		{"credential_json", true},
		{"authorization", true},
		{"bucket", false},
		{"endpoint_url", false},
		{"region", false},
		{"path", false},
	} {
		if got := looksSecret(c.key); got != c.expect {
			t.Errorf("looksSecret(%q) = %v, want %v", c.key, got, c.expect)
		}
	}
}

// --- AllowedKinds gate ---

func TestCreateTargetUnknownKindRejected(t *testing.T) {
	svc := &TargetService{vault: &targetFakeVault{}}
	_, err := svc.CreateTarget(context.Background(), "actor", "tenant", CreateTargetRequest{
		Name: "weird", Kind: "ftp", Config: map[string]any{},
	})
	if !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("expected ErrUnknownKind, got %v", err)
	}
}

func TestCreateTargetRequiresName(t *testing.T) {
	svc := &TargetService{vault: &targetFakeVault{}}
	_, err := svc.CreateTarget(context.Background(), "actor", "tenant", CreateTargetRequest{
		Kind: "in_site_local", Config: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestCreateTargetNilVaultRejected(t *testing.T) {
	svc := &TargetService{vault: nil}
	_, err := svc.CreateTarget(context.Background(), "actor", "tenant", CreateTargetRequest{
		Name: "x", Kind: "offsite_s3", Config: map[string]any{},
	})
	if !errors.Is(err, ErrVaultUnavailable) {
		t.Fatalf("expected ErrVaultUnavailable, got %v", err)
	}
}

func TestTriggerRunUnknownKindRejected(t *testing.T) {
	svc := &TargetService{}
	_, err := svc.TriggerRun(context.Background(), "a", "t", [16]byte{}, "nightly")
	if err == nil {
		t.Fatal("expected error for unknown run kind")
	}
}

// --- decryptToMap round trip ---

func TestTargetDecryptToMapRoundTrip(t *testing.T) {
	fv := &targetFakeVault{}
	svc := &TargetService{vault: fv}
	original := map[string]any{"bucket": "x", "secret_access_key": "y"}
	pt, _ := json.Marshal(original)
	ct, _, err := fv.Encrypt(context.Background(), backupTargetsKey, pt)
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.decryptToMap(context.Background(), ct)
	if err != nil {
		t.Fatal(err)
	}
	if out["bucket"] != "x" || out["secret_access_key"] != "y" {
		t.Fatalf("round trip failed: %v", out)
	}
}
