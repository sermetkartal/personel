// cmd/regression/main.go — Faz 14 #156.
//
// Replays every registered regression scenario against a deployed
// stack. Each scenario is a file under
// apps/qa/test/regression/YYYY-MM-DD-<short>.go that exports a
// `Scenario` with a Run(ctx, client) error method and is
// registered via init().
//
// Usage:
//
//	regression \
//	  --api http://192.168.5.44:8000 \
//	  --out regression-report.json
//
// Exit 0 = all scenarios pass.
// Exit 1 = at least one scenario failed.
//
// CI wiring: runs on every PR via the staged regression workflow
// in infra/ci-scaffolds/regression.yml (once workflow scope is
// available).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	regression "github.com/personel/qa/test/regression"
)

type scenarioResult struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	DateFiled  string `json:"date_filed"`
	Reference  string `json:"reference"`
	Passed     bool   `json:"passed"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type report struct {
	GeneratedAt time.Time        `json:"generated_at"`
	APIURL      string           `json:"api_url"`
	Total       int              `json:"total"`
	Passed      int              `json:"passed"`
	Failed      int              `json:"failed"`
	Results     []scenarioResult `json:"results"`
}

func main() {
	var (
		apiURL      string
		outPath     string
		adminToken  string
	)
	flag.StringVar(&apiURL, "api", "http://192.168.5.44:8000", "API base URL")
	flag.StringVar(&outPath, "out", "regression-report.json", "json output")
	flag.StringVar(&adminToken, "admin-token", os.Getenv("PERSONEL_ADMIN_TOKEN"), "bearer token")
	flag.Parse()

	client := &http.Client{Timeout: 15 * time.Second}
	ctx := context.Background()

	env := regression.Env{
		APIURL:     apiURL,
		Client:     client,
		AdminToken: adminToken,
	}

	rep := report{
		GeneratedAt: time.Now().UTC(),
		APIURL:      apiURL,
	}
	for _, sc := range regression.All() {
		t0 := time.Now()
		err := sc.Run(ctx, env)
		res := scenarioResult{
			ID:         sc.ID(),
			Title:      sc.Title(),
			DateFiled:  sc.DateFiled(),
			Reference:  sc.Reference(),
			DurationMs: time.Since(t0).Milliseconds(),
			Passed:     err == nil,
		}
		if err != nil {
			res.Error = err.Error()
		}
		rep.Results = append(rep.Results, res)
		rep.Total++
		if res.Passed {
			rep.Passed++
		} else {
			rep.Failed++
		}
	}

	b, _ := json.MarshalIndent(rep, "", "  ")
	_ = os.WriteFile(outPath, b, 0o644)

	fmt.Printf("regression: %d pass / %d fail (%d total)\n",
		rep.Passed, rep.Failed, rep.Total)
	for _, r := range rep.Results {
		status := "PASS"
		if !r.Passed {
			status = "FAIL"
		}
		fmt.Printf("  [%s] %s — %s (%dms) %s\n",
			status, r.ID, r.Title, r.DurationMs, r.Error)
	}
	if rep.Failed > 0 {
		os.Exit(1)
	}
}
