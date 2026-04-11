// Package report provides Phase 1 exit criteria report types and rendering.
//
// It is intentionally separate from the load-test runner so that the CI
// phase1-exit-report generator and any other consumer can import report types
// without pulling in the load simulator.
//
// Consumers of this package:
//   - test/load/runner.go  — populates RunResult and delegates rendering here
//   - cmd/audit-redteam    — appends SecurityResult to the report
//   - ci/scripts           — reads JSON output produced by WriteJSON
package report

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Core result types
// ---------------------------------------------------------------------------

// CriterionResult is the pass/fail outcome of a single Phase 1 exit criterion.
type CriterionResult struct {
	ID          string  `json:"id"`
	Description string  `json:"description"`
	Threshold   float64 `json:"threshold"`
	Actual      float64 `json:"actual"`   // -1 means "not measured"
	Unit        string  `json:"unit"`
	Passed      bool    `json:"passed"`
	Blocking    bool    `json:"blocking"` // true for EC-9 and any other hard gates
}

// SecurityResult records one attack-vector outcome from the red team.
type SecurityResult struct {
	AttackVector string `json:"attack_vector"`
	Description  string `json:"description"`
	// StatusCode is the HTTP status code returned by the endpoint (0 if not HTTP).
	StatusCode int  `json:"status_code,omitempty"`
	// Blocked is true if the attack was correctly rejected.
	Blocked bool `json:"blocked"`
	// Critical is true if a non-blocked result is an immediate Phase 1 blocker.
	Critical bool `json:"critical"`
	// Note is a human-readable explanation of the outcome.
	Note string `json:"note,omitempty"`
}

// SuiteResult is the top-level report produced for a Phase 1 run.
// It aggregates criteria outcomes, security red team results, and metadata.
type SuiteResult struct {
	GeneratedAt   time.Time         `json:"generated_at"`
	Suite         string            `json:"suite"` // e.g., "load-500", "security", "phase1-final"
	CommitSHA     string            `json:"commit_sha,omitempty"`
	Branch        string            `json:"branch,omitempty"`
	Environment   string            `json:"environment"` // "ci" | "staging" | "manual"
	DurationSec   float64           `json:"duration_sec"`
	Criteria      []CriterionResult `json:"criteria"`
	SecurityTests []SecurityResult  `json:"security_tests,omitempty"`
	Passed        bool              `json:"passed"`
	// BlockerFound is true if any blocking criterion failed or any critical
	// security test was not blocked.
	BlockerFound bool `json:"blocker_found"`
}

// ---------------------------------------------------------------------------
// Writer
// ---------------------------------------------------------------------------

// Writer renders SuiteResult to JSON and HTML files.
type Writer struct {
	// OutputDir is the directory where report files are written.
	OutputDir string
}

// Write renders both JSON and HTML outputs for result.
// Files are named: phase1-<suite>-<timestamp>.{json,html}
func (w *Writer) Write(result *SuiteResult) error {
	if err := os.MkdirAll(w.OutputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	ts := result.GeneratedAt.Format("20060102-150405")
	suiteName := sanitizeName(result.Suite)
	baseName := fmt.Sprintf("phase1-%s-%s", suiteName, ts)

	jsonPath := filepath.Join(w.OutputDir, baseName+".json")
	if err := w.WriteJSON(jsonPath, result); err != nil {
		return fmt.Errorf("write JSON: %w", err)
	}

	htmlPath := filepath.Join(w.OutputDir, baseName+".html")
	if err := w.WriteHTML(htmlPath, result); err != nil {
		return fmt.Errorf("write HTML: %w", err)
	}

	fmt.Printf("\nPhase 1 Exit Report (%s):\n", result.Suite)
	fmt.Printf("  JSON: %s\n", jsonPath)
	fmt.Printf("  HTML: %s\n", htmlPath)
	fmt.Printf("  Overall: %s\n", PassFailStr(result.Passed))
	if result.BlockerFound {
		fmt.Printf("  BLOCKER DETECTED — pipeline must stop\n")
	}
	return nil
}

// WriteJSON writes a JSON report to path.
func (w *Writer) WriteJSON(path string, result *SuiteResult) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

// WriteHTML writes an HTML report to path.
func (w *Writer) WriteHTML(path string, result *SuiteResult) error {
	tmpl, err := template.New("report").Funcs(template.FuncMap{
		"passClass": func(passed bool) string {
			if passed {
				return "pass"
			}
			return "fail"
		},
		"passStr": PassFailStr,
		"fmtActual": func(v float64) string {
			if v < 0 {
				return "N/A"
			}
			return fmt.Sprintf("%.4g", v)
		},
	}).Parse(htmlReportTemplate)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return tmpl.Execute(f, result)
}

// ---------------------------------------------------------------------------
// Console output
// ---------------------------------------------------------------------------

// PrintSummary writes a formatted table to stdout.
func (w *Writer) PrintSummary(result *SuiteResult) {
	fmt.Printf("\nPhase 1 Exit Criteria — %s\n", result.Suite)
	fmt.Println(strings.Repeat("=", 90))
	fmt.Printf("%-6s %-45s %-12s %-12s %-6s %s\n",
		"ID", "Description", "Threshold", "Actual", "Gate", "Result")
	fmt.Println(strings.Repeat("-", 90))

	for _, cr := range result.Criteria {
		actualStr := "N/A"
		if cr.Actual >= 0 {
			actualStr = fmt.Sprintf("%.4g %s", cr.Actual, cr.Unit)
		}
		gateStr := ""
		if cr.Blocking {
			gateStr = "[BLOCK]"
		}
		fmt.Printf("%-6s %-45s %-12s %-12s %-6s %s\n",
			cr.ID,
			truncate(cr.Description, 45),
			fmt.Sprintf("%.4g %s", cr.Threshold, cr.Unit),
			actualStr,
			gateStr,
			PassFailStr(cr.Passed),
		)
	}

	if len(result.SecurityTests) > 0 {
		fmt.Printf("\nSecurity Red Team Results:\n")
		fmt.Println(strings.Repeat("-", 90))
		for _, sr := range result.SecurityTests {
			status := "BLOCKED"
			if !sr.Blocked {
				status = "EXPOSED"
				if sr.Critical {
					status = "CRITICAL-EXPOSED"
				}
			}
			fmt.Printf("  %-20s %-40s %s\n",
				sr.AttackVector,
				truncate(sr.Description, 40),
				status,
			)
		}
	}

	fmt.Println(strings.Repeat("=", 90))
	overall := PassFailStr(result.Passed)
	if result.BlockerFound {
		overall += " [BLOCKER DETECTED]"
	}
	fmt.Printf("Overall: %s\n\n", overall)
}

// ---------------------------------------------------------------------------
// Builders
// ---------------------------------------------------------------------------

// NewSuiteResult creates an empty SuiteResult with timestamps set.
func NewSuiteResult(suite, environment string) *SuiteResult {
	return &SuiteResult{
		GeneratedAt: time.Now().UTC(),
		Suite:       suite,
		Environment: environment,
	}
}

// Finalise computes Passed and BlockerFound from the criterion and security
// results. Call this after populating all results.
func (r *SuiteResult) Finalise() {
	allPassed := true
	blockerFound := false

	for _, cr := range r.Criteria {
		if !cr.Passed {
			allPassed = false
			if cr.Blocking {
				blockerFound = true
			}
		}
	}
	for _, sr := range r.SecurityTests {
		if !sr.Blocked {
			allPassed = false
			if sr.Critical {
				blockerFound = true
			}
		}
	}

	r.Passed = allPassed
	r.BlockerFound = blockerFound
}

// AddCriterion appends a criterion result.
func (r *SuiteResult) AddCriterion(id, description, unit string, threshold, actual float64, passed, blocking bool) {
	r.Criteria = append(r.Criteria, CriterionResult{
		ID:          id,
		Description: description,
		Threshold:   threshold,
		Actual:      actual,
		Unit:        unit,
		Passed:      passed,
		Blocking:    blocking,
	})
}

// AddSecurityResult appends an attack-vector result.
func (r *SuiteResult) AddSecurityResult(vector, description string, statusCode int, blocked, critical bool, note string) {
	r.SecurityTests = append(r.SecurityTests, SecurityResult{
		AttackVector: vector,
		Description:  description,
		StatusCode:   statusCode,
		Blocked:      blocked,
		Critical:     critical,
		Note:         note,
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// PassFailStr returns "PASS" or "FAIL".
func PassFailStr(passed bool) string {
	if passed {
		return "PASS"
	}
	return "FAIL"
}

func sanitizeName(s string) string {
	out := make([]byte, 0, len(s))
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' {
			out = append(out, byte(c))
		} else {
			out = append(out, '-')
		}
	}
	return string(out)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

// ---------------------------------------------------------------------------
// HTML template
// ---------------------------------------------------------------------------

const htmlReportTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>Personel Phase 1 Exit Criteria — {{.Suite}}</title>
  <style>
    *{box-sizing:border-box}
    body{font-family:'Segoe UI',Arial,sans-serif;max-width:1100px;margin:2rem auto;padding:0 1rem;color:#212529}
    h1{color:#1a1a2e;border-bottom:2px solid #dee2e6;padding-bottom:.5rem}
    h2{color:#495057;margin-top:2rem}
    .badge{display:inline-block;padding:.25rem .75rem;border-radius:4px;font-weight:700;font-size:1.1rem}
    .badge-pass{background:#d4edda;color:#155724}
    .badge-fail{background:#f8d7da;color:#721c24}
    .badge-blocker{background:#fff3cd;color:#856404;margin-left:.5rem}
    .meta{color:#6c757d;font-size:.875rem;margin:.25rem 0}
    table{width:100%;border-collapse:collapse;margin-top:1rem}
    th,td{padding:.65rem .75rem;text-align:left;border-bottom:1px solid #dee2e6;font-size:.9rem}
    th{background:#f8f9fa;font-weight:600}
    tr:hover td{background:#f8f9fa}
    .pass{color:#155724;font-weight:700}
    .fail{color:#721c24;font-weight:700}
    .na{color:#856404}
    .blocking{color:#856404;font-size:.75rem;font-weight:600}
    .exposed{color:#721c24;font-weight:700}
    .critical{color:#dc3545;font-weight:700}
    .blocked{color:#155724;font-weight:700}
  </style>
</head>
<body>
  <h1>Personel Phase 1 Exit Criteria Report</h1>

  <p>
    <span class="badge {{if .Passed}}badge-pass{{else}}badge-fail{{end}}">
      {{if .Passed}}PASS{{else}}FAIL{{end}}
    </span>
    {{if .BlockerFound}}<span class="badge badge-blocker">BLOCKER DETECTED</span>{{end}}
  </p>

  <p class="meta">Suite: {{.Suite}}</p>
  <p class="meta">Environment: {{.Environment}}</p>
  <p class="meta">Generated: {{.GeneratedAt.Format "2006-01-02 15:04:05 UTC"}}</p>
  {{if .CommitSHA}}<p class="meta">Commit: {{.CommitSHA}} ({{.Branch}})</p>{{end}}
  <p class="meta">Duration: {{printf "%.1f" .DurationSec}}s</p>

  <h2>Exit Criteria</h2>
  <table>
    <thead>
      <tr>
        <th>ID</th>
        <th>Description</th>
        <th>Threshold</th>
        <th>Actual</th>
        <th>Gate</th>
        <th>Result</th>
      </tr>
    </thead>
    <tbody>
      {{range .Criteria}}
      <tr>
        <td>{{.ID}}</td>
        <td>{{.Description}}</td>
        <td>{{printf "%.4g" .Threshold}} {{.Unit}}</td>
        <td>{{fmtActual .Actual}} {{if ge .Actual 0.0}}{{.Unit}}{{end}}</td>
        <td>{{if .Blocking}}<span class="blocking">BLOCKING</span>{{end}}</td>
        <td><span class="{{passClass .Passed}}">{{passStr .Passed}}</span></td>
      </tr>
      {{end}}
    </tbody>
  </table>

  {{if .SecurityTests}}
  <h2>Security Red Team</h2>
  <table>
    <thead>
      <tr>
        <th>Vector</th>
        <th>Description</th>
        <th>Status Code</th>
        <th>Result</th>
        <th>Note</th>
      </tr>
    </thead>
    <tbody>
      {{range .SecurityTests}}
      <tr>
        <td>{{.AttackVector}}</td>
        <td>{{.Description}}</td>
        <td>{{if .StatusCode}}{{.StatusCode}}{{else}}—{{end}}</td>
        <td>
          {{if .Blocked}}
            <span class="blocked">BLOCKED</span>
          {{else if .Critical}}
            <span class="critical">CRITICAL — EXPOSED</span>
          {{else}}
            <span class="exposed">EXPOSED</span>
          {{end}}
        </td>
        <td>{{.Note}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>
  {{end}}
</body>
</html>`
