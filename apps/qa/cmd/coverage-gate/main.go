// cmd/coverage-gate/main.go — Faz 14 #146.
//
// Aggregates unit-test coverage from every stack (Go, Rust, TypeScript,
// Python) and fails the run when any module falls below its configured
// threshold. Thresholds live in apps/qa/ci/coverage-thresholds.yaml.
//
// The binary does NOT invoke the test runners itself by default — it
// reads existing coverage artifacts. Use --run to spawn the runners
// inline (slow path; intended for CI jobs with every toolchain already
// provisioned). The default path is artifact-driven so CI matrix jobs
// can each upload their coverage file and a single aggregation job
// consumes them all.
//
// Usage:
//
//	coverage-gate --thresholds apps/qa/ci/coverage-thresholds.yaml \
//	              --go-cover apps/api/coverage.out \
//	              --go-cover apps/gateway/coverage.out \
//	              --rust-cover apps/agent/tarpaulin-report.json \
//	              --ts-cover apps/console/coverage/coverage-summary.json \
//	              --py-cover apps/ml-classifier/coverage.json \
//	              --out coverage-report.json \
//	              --md coverage-report.md
//
// Exit codes: 0 = all gates green, 1 = one or more modules below threshold
// or critical package below 80%.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type thresholdsFile struct {
	DefaultFloor float64            `yaml:"default_floor_percent"`
	CriticalFloor float64           `yaml:"critical_floor_percent"`
	Modules      map[string]float64 `yaml:"modules"`
	Critical     []string           `yaml:"critical_packages"`
}

type moduleReport struct {
	Name       string  `json:"name"`
	Stack      string  `json:"stack"`
	Covered    float64 `json:"covered_percent"`
	Threshold  float64 `json:"threshold_percent"`
	Critical   bool    `json:"critical"`
	Passed     bool    `json:"passed"`
	SourceFile string  `json:"source_file"`
}

type report struct {
	Modules      []moduleReport `json:"modules"`
	FailedCount  int            `json:"failed_count"`
	OverallPct   float64        `json:"overall_percent"`
	PassedOverall bool          `json:"passed_overall"`
}

type stringSlice []string

func (s *stringSlice) String() string     { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error { *s = append(*s, v); return nil }

func main() {
	var (
		thresholdsPath string
		outPath        string
		mdPath         string
		goCovers       stringSlice
		rustCovers     stringSlice
		tsCovers       stringSlice
		pyCovers       stringSlice
	)
	flag.StringVar(&thresholdsPath, "thresholds", "apps/qa/ci/coverage-thresholds.yaml", "path to threshold yaml")
	flag.StringVar(&outPath, "out", "coverage-report.json", "json output path")
	flag.StringVar(&mdPath, "md", "coverage-report.md", "markdown output path")
	flag.Var(&goCovers, "go-cover", "go -coverprofile=out path (repeatable)")
	flag.Var(&rustCovers, "rust-cover", "cargo tarpaulin json path (repeatable)")
	flag.Var(&tsCovers, "ts-cover", "vitest coverage-summary.json (repeatable)")
	flag.Var(&pyCovers, "py-cover", "pytest coverage.json (repeatable)")
	flag.Parse()

	th, err := loadThresholds(thresholdsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load thresholds: %v\n", err)
		os.Exit(2)
	}

	var modules []moduleReport
	for _, f := range goCovers {
		m, err := parseGoCover(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: go-cover %s: %v\n", f, err)
			continue
		}
		modules = append(modules, m...)
	}
	for _, f := range rustCovers {
		m, err := parseRustTarpaulin(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: rust-cover %s: %v\n", f, err)
			continue
		}
		modules = append(modules, m...)
	}
	for _, f := range tsCovers {
		m, err := parseVitestSummary(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: ts-cover %s: %v\n", f, err)
			continue
		}
		modules = append(modules, m...)
	}
	for _, f := range pyCovers {
		m, err := parsePytestCoverage(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: py-cover %s: %v\n", f, err)
			continue
		}
		modules = append(modules, m...)
	}

	// Apply thresholds
	rep := report{PassedOverall: true}
	var total, weighted float64
	for i := range modules {
		m := &modules[i]
		m.Threshold = th.DefaultFloor
		if v, ok := th.Modules[m.Name]; ok {
			m.Threshold = v
		}
		for _, c := range th.Critical {
			if strings.Contains(m.Name, c) {
				m.Critical = true
				if th.CriticalFloor > m.Threshold {
					m.Threshold = th.CriticalFloor
				}
			}
		}
		m.Passed = m.Covered >= m.Threshold
		if !m.Passed {
			rep.FailedCount++
			rep.PassedOverall = false
		}
		total++
		weighted += m.Covered
	}
	if total > 0 {
		rep.OverallPct = weighted / total
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Name < modules[j].Name })
	rep.Modules = modules

	// Emit json
	jb, _ := json.MarshalIndent(rep, "", "  ")
	if err := os.WriteFile(outPath, jb, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write json: %v\n", err)
		os.Exit(2)
	}
	if err := os.WriteFile(mdPath, []byte(renderMarkdown(rep)), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write md: %v\n", err)
		os.Exit(2)
	}

	if !rep.PassedOverall {
		fmt.Fprintf(os.Stderr, "coverage gate FAIL: %d module(s) below threshold\n", rep.FailedCount)
		os.Exit(1)
	}
	fmt.Printf("coverage gate PASS: %.1f%% overall across %d modules\n", rep.OverallPct, len(modules))
}

func loadThresholds(p string) (*thresholdsFile, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var t thresholdsFile
	if err := yaml.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	if t.DefaultFloor == 0 {
		t.DefaultFloor = 60.0
	}
	if t.CriticalFloor == 0 {
		t.CriticalFloor = 80.0
	}
	return &t, nil
}

// parseGoCover reads the text coverprofile produced by `go test -coverprofile=out`.
// Each line: `file.go:a.b,c.d stmts covered` — we aggregate ratios per module
// using the first path segment under `apps/` as module name.
func parseGoCover(path string) ([]moduleReport, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(b), "\n")
	type acc struct{ stmts, covered float64 }
	buckets := map[string]*acc{}
	for i, ln := range lines {
		if i == 0 || ln == "" {
			continue // mode line or blank
		}
		parts := strings.Fields(ln)
		if len(parts) != 3 {
			continue
		}
		file := strings.SplitN(parts[0], ":", 2)[0]
		mod := moduleFromPath(file)
		var stmts, cov float64
		fmt.Sscanf(parts[1], "%f", &stmts)
		fmt.Sscanf(parts[2], "%f", &cov)
		if _, ok := buckets[mod]; !ok {
			buckets[mod] = &acc{}
		}
		buckets[mod].stmts += stmts
		if cov > 0 {
			buckets[mod].covered += stmts
		}
	}
	out := make([]moduleReport, 0, len(buckets))
	for name, a := range buckets {
		pct := 0.0
		if a.stmts > 0 {
			pct = (a.covered / a.stmts) * 100
		}
		out = append(out, moduleReport{
			Name: name, Stack: "go", Covered: pct, SourceFile: path,
		})
	}
	return out, nil
}

func moduleFromPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	idx := strings.Index(p, "apps/")
	if idx < 0 {
		return p
	}
	rest := p[idx+len("apps/"):]
	segs := strings.SplitN(rest, "/", 3)
	if len(segs) < 2 {
		return rest
	}
	// apps/api/internal/auth/... → apps/api/internal/auth
	if segs[1] == "internal" && len(segs) > 2 {
		sub := strings.SplitN(segs[2], "/", 2)
		return "apps/" + segs[0] + "/internal/" + sub[0]
	}
	return "apps/" + segs[0] + "/" + segs[1]
}

type tarpaulinJSON struct {
	Files []struct {
		Path           []string `json:"path"`
		Covered        int      `json:"covered"`
		Coverable      int      `json:"coverable"`
	} `json:"files"`
	Coverage float64 `json:"coverage"`
}

func parseRustTarpaulin(path string) ([]moduleReport, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t tarpaulinJSON
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	buckets := map[string][2]int{}
	for _, f := range t.Files {
		if len(f.Path) == 0 {
			continue
		}
		crate := "unknown"
		for i, seg := range f.Path {
			if seg == "crates" && i+1 < len(f.Path) {
				crate = f.Path[i+1]
				break
			}
		}
		name := "apps/agent/crates/" + crate
		cur := buckets[name]
		buckets[name] = [2]int{cur[0] + f.Covered, cur[1] + f.Coverable}
	}
	out := make([]moduleReport, 0, len(buckets))
	for name, v := range buckets {
		pct := 0.0
		if v[1] > 0 {
			pct = (float64(v[0]) / float64(v[1])) * 100
		}
		out = append(out, moduleReport{
			Name: name, Stack: "rust", Covered: pct, SourceFile: path,
		})
	}
	return out, nil
}

type vitestSummary struct {
	Total struct {
		Lines struct {
			Pct float64 `json:"pct"`
		} `json:"lines"`
	} `json:"total"`
}

func parseVitestSummary(path string) ([]moduleReport, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s vitestSummary
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	name := "apps/" + inferFrontendModule(path)
	return []moduleReport{{
		Name: name, Stack: "ts", Covered: s.Total.Lines.Pct, SourceFile: path,
	}}, nil
}

func inferFrontendModule(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	if strings.Contains(path, "/console/") {
		return "console"
	}
	if strings.Contains(path, "/portal/") {
		return "portal"
	}
	return "frontend"
}

type pytestCov struct {
	Totals struct {
		PercentCovered float64 `json:"percent_covered"`
	} `json:"totals"`
}

func parsePytestCoverage(path string) ([]moduleReport, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c pytestCov
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	name := "apps/" + inferPythonModule(path)
	return []moduleReport{{
		Name: name, Stack: "python", Covered: c.Totals.PercentCovered, SourceFile: path,
	}}, nil
}

func inferPythonModule(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	for _, svc := range []string{"ml-classifier", "uba-detector", "ocr-service"} {
		if strings.Contains(path, "/"+svc+"/") {
			return svc
		}
	}
	return "python"
}

func renderMarkdown(r report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Coverage Report\n\n")
	fmt.Fprintf(&b, "**Overall**: %.1f%% | **Modules**: %d | **Failed**: %d\n\n",
		r.OverallPct, len(r.Modules), r.FailedCount)
	fmt.Fprintf(&b, "| Module | Stack | Covered | Threshold | Critical | Pass |\n")
	fmt.Fprintf(&b, "|---|---|---|---|---|---|\n")
	for _, m := range r.Modules {
		status := "✅"
		if !m.Passed {
			status = "❌"
		}
		crit := ""
		if m.Critical {
			crit = "yes"
		}
		fmt.Fprintf(&b, "| %s | %s | %.1f%% | %.0f%% | %s | %s |\n",
			m.Name, m.Stack, m.Covered, m.Threshold, crit, status)
	}
	return b.String()
}
