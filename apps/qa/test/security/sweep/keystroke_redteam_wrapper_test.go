//go:build security

// Faz 14 #152 — Wrapper that runs the Faz 11 #118 hardened
// audit-redteam binary as part of the Go security suite with
// a pass/fail assertion.
//
// The wrapper shells out to `go run ./cmd/audit-redteam` (or a
// pre-built binary if `PERSONEL_REDTEAM_BIN` is set) and parses
// its exit code + JSON output.
//
// Exit 0 = all keystroke plaintext access attempts were blocked.
// Exit != 0 = at least one API path leaked plaintext — PHASE 1
//             BLOCKER.
//
// An empty existing file `keystroke_admin_blindness_test.go` lives
// next to this one; that file holds the in-process red team logic.
// This wrapper is the CI entrypoint.
package sweep

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type redteamReport struct {
	TotalAttempts   int      `json:"total_attempts"`
	BlockedCount    int      `json:"blocked_count"`
	LeakedCount     int      `json:"leaked_count"`
	LeakedEndpoints []string `json:"leaked_endpoints"`
	PassedOverall   bool     `json:"passed_overall"`
}

func TestKeystrokeRedteam_Wrapper_NoLeak(t *testing.T) {
	apiURL := getAPIURL(t)
	token := getAdminToken(t)

	bin := os.Getenv("PERSONEL_REDTEAM_BIN")
	var cmd *exec.Cmd
	if bin == "" {
		// Fallback: go run the cmd package
		repoRoot, err := findRepoRoot()
		if err != nil {
			t.Skipf("cannot locate repo root: %v", err)
		}
		cmd = exec.Command("go", "run", "./cmd/audit-redteam", "run",
			"--api", apiURL,
			"--tenant", os.Getenv("PERSONEL_TENANT_ID"),
			"--endpoint", os.Getenv("PERSONEL_ENDPOINT_ID"),
			"--json",
		)
		cmd.Dir = filepath.Join(repoRoot, "apps", "qa")
	} else {
		cmd = exec.Command(bin, "run",
			"--api", apiURL,
			"--tenant", os.Getenv("PERSONEL_TENANT_ID"),
			"--endpoint", os.Getenv("PERSONEL_ENDPOINT_ID"),
			"--json",
		)
	}
	if token != "" {
		cmd.Env = append(os.Environ(), "PERSONEL_ADMIN_TOKEN="+token)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	t.Logf("redteam stderr:\n%s", stderr.String())

	if stdout.Len() == 0 {
		// No JSON output — treat exit code as truth
		if err != nil {
			t.Fatalf("audit-redteam failed (exit != 0 and no json): %v", err)
		}
		return
	}

	var report redteamReport
	if jerr := json.Unmarshal(stdout.Bytes(), &report); jerr != nil {
		t.Logf("could not parse redteam JSON: %v\nraw: %s", jerr, stdout.String())
		if err != nil {
			t.Fatal("audit-redteam exit non-zero with unparseable output")
		}
		return
	}

	if !report.PassedOverall || report.LeakedCount > 0 {
		t.Fatalf("KEYSTROKE ADMIN-BLINDNESS VIOLATED — %d paths leaked plaintext: %v",
			report.LeakedCount, report.LeakedEndpoints)
	}
	t.Logf("redteam pass: %d attempts all blocked (%.1f%% coverage)",
		report.TotalAttempts,
		100.0*float64(report.BlockedCount)/float64(max(report.TotalAttempts, 1)))
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
