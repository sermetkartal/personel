//go:build e2e

// dlp_opt_in_smoke_test.go — Phase 1 exit criterion #18 smoke test stub.
//
// ADR 0013: DLP Service Disabled by Default — opt-in ceremony scripts audit.
//
// Bu dosya Docker stack veya Vault gerektirmeden çalışan statik smoke
// testlerini içerir:
//   - Script varlığı + çalıştırılabilirlik
//   - bash -n syntax kontrol
//   - --help çıkışı (exit 0)
//   - Eksik zorunlu argüman → non-zero exit + stderr mesajı
//   - ADR dokümantasyon dosyası varlığı
//   - Compliance form şablonu varlığı
//
// This file contains static smoke tests that run without a Docker stack or
// Vault. They guard against regressions in the ceremony script shell
// implementation before the full integration test (TestPhase1Exit18_DLPOptInCeremony
// in dlp_opt_in_test.go) is executed on a staging environment.
//
// Run with:
//
//	go test -tags e2e -run TestDLPOptIn ./test/e2e/... -v
//
// The following scenarios are intentionally NOT covered here (they require a
// live Docker stack with Vault, Postgres, MinIO, and the DLP container image):
//
//   - Vault unsealed precondition verification
//   - AppRole Secret ID one-time issuance and accessor storage
//   - docker compose --profile dlp up -d execution
//   - Container health polling (90-second timeout loop)
//   - POST /v1/system/dlp-bootstrap-keys PE-DEK bootstrap for enrolled endpoints (ADR 0013 A2)
//   - POST /v1/system/dlp-transition audit event with form SHA256 (dlp.enabled)
//   - GET /v1/system/dlp-state post-ceremony state = "enabled" validation
//   - Rollback trap: Secret ID revocation + container stop + dlp.enable_failed audit (ADR 0013 A3)
//   - Transparency portal banner update (DLP aktif edildi)
//   - ADR 0013 A4: keystroke ciphertext NOT destroyed on dlp-disable.sh (14-day TTL)
//   - ADR 0013 A5: policy signer rejects dlp_enabled=false + keystroke.content_enabled=true
//   - Audit hash chain integrity across the enable/disable transition
//   - WORM sink (MinIO audit-worm bucket) cross-check (ADR 0014)
//   - Phase 1 exit #18 wall-clock budget enforcement (1-hour deadline)
//
// All skipped scenarios are covered by TestPhase1Exit18_DLPOptInCeremony
// (dlp_opt_in_test.go) which must be executed on a staging environment with
// QA_INTEGRATION=1 set before Phase 1 pilot sign-off.
package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// smokeRepoRoot walks up from the test binary's working directory until it
// finds a directory containing CLAUDE.md (the repository root sentinel).
// It is a standalone helper to avoid coupling to the harness package for
// smoke tests that must work without a running stack.
func smokeRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err, "smokeRepoRoot: getwd failed")
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "CLAUDE.md")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("smokeRepoRoot: could not find CLAUDE.md walking up from " + dir)
		}
		dir = parent
	}
}

// TestDLPOptInSmoke is the entry point for Phase 1 exit criterion #18 smoke
// checks. It delegates to a set of sub-tests so that failures are isolated
// and each sub-test is individually addressable with -run.
//
// This test intentionally skips the real Docker + Vault ceremony execution.
// That is covered by TestPhase1Exit18_DLPOptInCeremony in dlp_opt_in_test.go,
// which requires QA_INTEGRATION=1 and a staging stack.
func TestDLPOptIn(t *testing.T) {
	t.Skip(
		"Smoke sub-tests are exposed as TestDLPOptIn/... — run with -run TestDLPOptIn/. " +
			"The parent function itself skips; use -run TestDLPOptIn/<subtest> directly. " +
			"Real Docker + Vault e2e: TestPhase1Exit18_DLPOptInCeremony (QA_INTEGRATION=1 required).",
	)
}

// TestDLPOptIn_EnableScriptExists verifies that infra/scripts/dlp-enable.sh
// is present in the repository and has the executable bit set.
func TestDLPOptIn_EnableScriptExists(t *testing.T) {
	root := smokeRepoRoot(t)
	path := filepath.Join(root, "infra", "scripts", "dlp-enable.sh")

	info, err := os.Stat(path)
	require.NoError(t, err, "dlp-enable.sh must exist at %s", path)
	require.False(t, info.IsDir(), "dlp-enable.sh must be a file, not a directory")

	// Executable bit check (owner-execute sufficient; script is run via bash)
	require.NotZero(t, info.Mode()&0o111,
		"dlp-enable.sh must have at least one executable bit set (got mode %s)", info.Mode())
}

// TestDLPOptIn_DisableScriptExists verifies that infra/scripts/dlp-disable.sh
// is present and executable.
func TestDLPOptIn_DisableScriptExists(t *testing.T) {
	root := smokeRepoRoot(t)
	path := filepath.Join(root, "infra", "scripts", "dlp-disable.sh")

	info, err := os.Stat(path)
	require.NoError(t, err, "dlp-disable.sh must exist at %s", path)
	require.False(t, info.IsDir(), "dlp-disable.sh must be a file, not a directory")

	require.NotZero(t, info.Mode()&0o111,
		"dlp-disable.sh must have at least one executable bit set (got mode %s)", info.Mode())
}

// TestDLPOptIn_EnableScriptSyntax runs `bash -n` on dlp-enable.sh to verify
// the shell syntax is clean on the current platform (macOS BSD bash or Linux
// GNU bash). A syntax error here means the ceremony will fail at runtime.
func TestDLPOptIn_EnableScriptSyntax(t *testing.T) {
	root := smokeRepoRoot(t)
	path := filepath.Join(root, "infra", "scripts", "dlp-enable.sh")

	var stderr bytes.Buffer
	cmd := exec.Command("bash", "-n", path)
	cmd.Stderr = &stderr
	err := cmd.Run()
	require.NoError(t, err,
		"bash -n dlp-enable.sh reported syntax errors:\n%s", stderr.String())
	assert.Empty(t, stderr.String(), "bash -n must produce no stderr output for a clean script")
}

// TestDLPOptIn_DisableScriptSyntax runs `bash -n` on dlp-disable.sh.
func TestDLPOptIn_DisableScriptSyntax(t *testing.T) {
	root := smokeRepoRoot(t)
	path := filepath.Join(root, "infra", "scripts", "dlp-disable.sh")

	var stderr bytes.Buffer
	cmd := exec.Command("bash", "-n", path)
	cmd.Stderr = &stderr
	err := cmd.Run()
	require.NoError(t, err,
		"bash -n dlp-disable.sh reported syntax errors:\n%s", stderr.String())
	assert.Empty(t, stderr.String(), "bash -n must produce no stderr output for a clean script")
}

// TestDLPOptIn_EnableScriptHelp verifies that dlp-enable.sh --help exits with
// code 0 and prints non-empty output. The --help path must not require any
// external commands (Vault, Docker, curl) and must be usable by operators
// to understand the ceremony without triggering any side-effects.
func TestDLPOptIn_EnableScriptHelp(t *testing.T) {
	root := smokeRepoRoot(t)
	path := filepath.Join(root, "infra", "scripts", "dlp-enable.sh")

	var stdout, stderr bytes.Buffer
	cmd := exec.Command("bash", path, "--help")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	// --help must exit 0
	require.NoError(t, err,
		"dlp-enable.sh --help must exit 0; stderr: %s", stderr.String())
	require.NotEmpty(t, stdout.String(),
		"dlp-enable.sh --help must print usage text to stdout")

	// Sanity: output should mention the script or usage
	combined := stdout.String() + stderr.String()
	require.True(t,
		strings.Contains(combined, "dlp-enable") || strings.Contains(combined, "--form"),
		"--help output must reference the script name or --form flag; got: %q", combined)
}

// TestDLPOptIn_DisableScriptHelp verifies dlp-disable.sh --help exits 0.
func TestDLPOptIn_DisableScriptHelp(t *testing.T) {
	root := smokeRepoRoot(t)
	path := filepath.Join(root, "infra", "scripts", "dlp-disable.sh")

	var stdout, stderr bytes.Buffer
	cmd := exec.Command("bash", path, "--help")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	require.NoError(t, err,
		"dlp-disable.sh --help must exit 0; stderr: %s", stderr.String())
	require.NotEmpty(t, stdout.String(),
		"dlp-disable.sh --help must print usage text to stdout")

	combined := stdout.String() + stderr.String()
	require.True(t,
		strings.Contains(combined, "dlp-disable") || strings.Contains(combined, "--actor-id"),
		"--help output must reference the script name or --actor-id flag; got: %q", combined)
}

// TestDLPOptIn_EnableScriptMissingForm verifies that dlp-enable.sh invoked
// without --form (or with --form but without --dpo-email / --actor-id) exits
// with a non-zero status code and writes a descriptive error to stderr.
// This validates the argument validation guard at the top of the ceremony
// before any side-effects can occur.
func TestDLPOptIn_EnableScriptMissingForm(t *testing.T) {
	root := smokeRepoRoot(t)
	path := filepath.Join(root, "infra", "scripts", "dlp-enable.sh")

	cases := []struct {
		name string
		args []string
	}{
		{
			name: "no_args",
			args: []string{},
		},
		{
			name: "form_only_missing_others",
			args: []string{"--form", "/tmp/nonexistent-opt-in.pdf"},
		},
		{
			name: "form_and_dpo_missing_actor",
			args: []string{"--form", "/tmp/nonexistent.pdf", "--dpo-email", "dpo@test.local"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			cmd := exec.Command("bash", append([]string{path}, tc.args...)...)
			cmd.Stderr = &stderr
			err := cmd.Run()

			// Must exit non-zero (any non-zero code is acceptable)
			require.Error(t, err,
				"dlp-enable.sh with args %v must exit non-zero when required args are missing", tc.args)

			exitErr, ok := err.(*exec.ExitError)
			require.True(t, ok, "error must be an ExitError")
			require.NotEqual(t, 0, exitErr.ExitCode(),
				"exit code must be non-zero; got %d", exitErr.ExitCode())

			// Must print an error message to stderr
			stderrStr := stderr.String()
			require.NotEmpty(t, stderrStr,
				"dlp-enable.sh must write an error message to stderr when args are missing")
			require.True(t,
				strings.Contains(stderrStr, "ERROR") || strings.Contains(stderrStr, "required"),
				"stderr must contain 'ERROR' or 'required'; got: %q", stderrStr)
		})
	}
}

// TestDLPOptIn_DisableScriptMissingArgs verifies that dlp-disable.sh invoked
// without required args exits non-zero and writes to stderr.
func TestDLPOptIn_DisableScriptMissingArgs(t *testing.T) {
	root := smokeRepoRoot(t)
	path := filepath.Join(root, "infra", "scripts", "dlp-disable.sh")

	cases := []struct {
		name string
		args []string
	}{
		{
			name: "no_args",
			args: []string{},
		},
		{
			name: "actor_only_missing_reason",
			args: []string{"--actor-id", "dpo-001"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			cmd := exec.Command("bash", append([]string{path}, tc.args...)...)
			cmd.Stderr = &stderr
			err := cmd.Run()

			require.Error(t, err,
				"dlp-disable.sh with args %v must exit non-zero when required args are missing", tc.args)

			exitErr, ok := err.(*exec.ExitError)
			require.True(t, ok, "error must be an ExitError")
			require.NotEqual(t, 0, exitErr.ExitCode(),
				"exit code must be non-zero; got %d", exitErr.ExitCode())

			stderrStr := stderr.String()
			require.NotEmpty(t, stderrStr,
				"dlp-disable.sh must write an error message to stderr when args are missing")
			require.True(t,
				strings.Contains(stderrStr, "ERROR") || strings.Contains(stderrStr, "required"),
				"stderr must contain 'ERROR' or 'required'; got: %q", stderrStr)
		})
	}
}

// TestDLPOptIn_ADRDocumentExists verifies that the ADR 0013 document is
// present in the repository. The ceremony scripts reference ADR 0013 in their
// comments; losing the document would break the compliance audit trail.
func TestDLPOptIn_ADRDocumentExists(t *testing.T) {
	root := smokeRepoRoot(t)
	adrPath := filepath.Join(root, "docs", "adr", "0013-dlp-disabled-by-default.md")

	info, err := os.Stat(adrPath)
	require.NoError(t, err,
		"ADR 0013 document must exist at %s", adrPath)
	require.False(t, info.IsDir(), "ADR 0013 path must be a file, not a directory")
	require.Greater(t, info.Size(), int64(0), "ADR 0013 document must not be empty")
}

// TestDLPOptIn_ComplianceFormTemplateExists verifies that the DLP opt-in
// form template exists at docs/compliance/dlp-opt-in-form.md. ADR 0013
// §Opt-In Ceremony step 2 requires DPO + IT Security + Legal Counsel to
// sign a one-page form using this template. If the template is missing, the
// ceremony documentation chain is broken.
func TestDLPOptIn_ComplianceFormTemplateExists(t *testing.T) {
	root := smokeRepoRoot(t)
	formPath := filepath.Join(root, "docs", "compliance", "dlp-opt-in-form.md")

	info, err := os.Stat(formPath)
	require.NoError(t, err,
		"DLP opt-in form template must exist at %s — ADR 0013 §Opt-In Ceremony step 2 requires it", formPath)
	require.False(t, info.IsDir(), "dlp-opt-in-form.md must be a file, not a directory")
	require.Greater(t, info.Size(), int64(0), "dlp-opt-in-form.md must not be empty")
}
