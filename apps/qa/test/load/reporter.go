// reporter.go generates Phase 1 Exit Criteria pass/fail reports in JSON and HTML.
package load

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Reporter writes load test results to disk.
type Reporter struct {
	OutputDir string
}

// Write generates JSON and HTML reports from a RunResult.
func (r *Reporter) Write(result *RunResult) error {
	if err := os.MkdirAll(r.OutputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	ts := result.StartedAt.Format("20060102-150405")
	baseName := fmt.Sprintf("phase1-exit-report-%s", ts)

	// Write JSON report.
	jsonPath := filepath.Join(r.OutputDir, baseName+".json")
	if err := r.writeJSON(jsonPath, result); err != nil {
		return fmt.Errorf("write JSON: %w", err)
	}

	// Write HTML report.
	htmlPath := filepath.Join(r.OutputDir, baseName+".html")
	if err := r.writeHTML(htmlPath, result); err != nil {
		return fmt.Errorf("write HTML: %w", err)
	}

	fmt.Printf("\nPhase 1 Exit Report:\n")
	fmt.Printf("  JSON: %s\n", jsonPath)
	fmt.Printf("  HTML: %s\n", htmlPath)
	fmt.Printf("  Overall: %s\n", passFailStr(result.Passed))

	return nil
}

// JSONReport is the JSON-serializable form of a RunResult.
type JSONReport struct {
	GeneratedAt     string              `json:"generated_at"`
	Scenario        string              `json:"scenario"`
	Duration        string              `json:"duration"`
	AgentsStarted   int64               `json:"agents_started"`
	AgentErrors     int64               `json:"agent_errors"`
	CriteriaResults []CriterionResult   `json:"criteria_results"`
	AllPassed       bool                `json:"all_passed"`
	Metrics         map[string]float64  `json:"raw_metrics"`
}

func (r *Reporter) writeJSON(path string, result *RunResult) error {
	report := JSONReport{
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		Scenario:        result.Scenario.Name,
		Duration:        result.FinishedAt.Sub(result.StartedAt).String(),
		AgentsStarted:   result.PoolStats.Started,
		AgentErrors:     result.PoolStats.Errors,
		CriteriaResults: result.CriteriaResults,
		AllPassed:       result.Passed,
		Metrics:         result.Metrics,
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>Personel Phase 1 Exit Criteria Report</title>
  <style>
    body { font-family: 'Segoe UI', Arial, sans-serif; max-width: 1000px; margin: 2rem auto; padding: 0 1rem; }
    h1 { color: #1a1a2e; }
    .summary { background: {{if .AllPassed}}#d4edda{{else}}#f8d7da{{end}};
               border: 1px solid {{if .AllPassed}}#c3e6cb{{else}}#f5c6cb{{end}};
               padding: 1rem; border-radius: 4px; margin: 1rem 0; }
    .overall { font-size: 1.5rem; font-weight: bold; color: {{if .AllPassed}}#155724{{else}}#721c24{{end}}; }
    table { width: 100%; border-collapse: collapse; margin-top: 1rem; }
    th, td { padding: 0.75rem; text-align: left; border-bottom: 1px solid #dee2e6; }
    th { background: #f8f9fa; font-weight: 600; }
    .pass { color: #155724; font-weight: bold; }
    .fail { color: #721c24; font-weight: bold; }
    .na { color: #856404; }
    .meta { color: #6c757d; font-size: 0.875rem; }
  </style>
</head>
<body>
  <h1>Personel Phase 1 Exit Criteria Report</h1>
  <div class="summary">
    <div class="overall">{{if .AllPassed}}PASS — All criteria met{{else}}FAIL — One or more criteria not met{{end}}</div>
    <p class="meta">Scenario: {{.Scenario}} | Duration: {{.Duration}} | Generated: {{.GeneratedAt}}</p>
    <p class="meta">Agents started: {{.AgentsStarted}} | Agent errors: {{.AgentErrors}}</p>
  </div>

  <h2>Exit Criteria Results</h2>
  <table>
    <thead>
      <tr>
        <th>#</th>
        <th>Description</th>
        <th>Threshold</th>
        <th>Actual</th>
        <th>Result</th>
      </tr>
    </thead>
    <tbody>
      {{range .CriteriaResults}}
      <tr>
        <td>{{.ID}}</td>
        <td>{{.Description}}</td>
        <td>{{printf "%.4g" .Threshold}} {{.Unit}}</td>
        <td>{{if lt .Actual 0.0}}<span class="na">N/A</span>{{else}}{{printf "%.4g" .Actual}} {{.Unit}}{{end}}</td>
        <td>{{if .Passed}}<span class="pass">PASS</span>{{else}}<span class="fail">FAIL</span>{{end}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>
</body>
</html>`

func (r *Reporter) writeHTML(path string, result *RunResult) error {
	tmpl, err := template.New("report").Parse(htmlTemplate)
	if err != nil {
		return err
	}

	data := struct {
		GeneratedAt     string
		Scenario        string
		Duration        string
		AgentsStarted   int64
		AgentErrors     int64
		CriteriaResults []CriterionResult
		AllPassed       bool
	}{
		GeneratedAt:     time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Scenario:        result.Scenario.Name,
		Duration:        result.FinishedAt.Sub(result.StartedAt).String(),
		AgentsStarted:   result.PoolStats.Started,
		AgentErrors:     result.PoolStats.Errors,
		CriteriaResults: result.CriteriaResults,
		AllPassed:       result.Passed,
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return tmpl.Execute(f, data)
}

func passFailStr(passed bool) string {
	if passed {
		return "PASS"
	}
	return "FAIL"
}

// PrintSummary writes a human-readable summary to stdout.
func (r *Reporter) PrintSummary(result *RunResult) {
	fmt.Printf("\n%-6s %-50s %-15s %-15s %s\n",
		"ID", "Description", "Threshold", "Actual", "Result")
	fmt.Println(strings.Repeat("-", 100))

	for _, cr := range result.CriteriaResults {
		actualStr := "N/A"
		if cr.Actual >= 0 {
			actualStr = fmt.Sprintf("%.4g %s", cr.Actual, cr.Unit)
		}
		fmt.Printf("%-6s %-50s %-15s %-15s %s\n",
			cr.ID,
			truncateStr(cr.Description, 50),
			fmt.Sprintf("%.4g %s", cr.Threshold, cr.Unit),
			actualStr,
			passFailStr(cr.Passed),
		)
	}
	fmt.Printf("\nOverall: %s\n", passFailStr(result.Passed))
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
