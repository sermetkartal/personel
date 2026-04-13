//go:build windows

// cmd/footprint-bench — Windows-only agent footprint benchmark harness.
//
// Launches a real personel-agent.exe child process, samples its CPU% and
// RSS every --interval over a --duration window, computes avg / p50 / p95
// / p99 / max aggregates, compares against thresholds loaded from
// apps/qa/ci/thresholds.yaml (footprint block), prints a structured text
// + JSON report, terminates the child, and exits 0 on pass / 1 on fail.
//
// Phase 1 exit criteria addressed:
//
//	EC-2  Agent CPU average < 2 %
//	EC-3  Agent RSS peak    < 150 MB
//
// The harness is the CI substitute for the runtime throttle subsystem
// (apps/agent/crates/personel-agent/src/throttle.rs) — throttle enforces
// at runtime, footprint-bench verifies the envelope in CI.
//
// Usage:
//
//	footprint-bench \
//	    --agent ../agent/target/x86_64-pc-windows-msvc/release/personel-agent.exe \
//	    --duration 5m \
//	    --interval 2s \
//	    --thresholds ../qa/ci/thresholds.yaml \
//	    --report reports/footprint-bench-<ts>.json
//
// Agent process lifecycle:
//
//  1. Launch personel-agent.exe with --benchmark-mode (or default args if
//     the flag is not recognised — the harness captures stderr but does
//     not inspect it).
//  2. Wait 10 s for the agent to initialise its collectors.
//  3. Enter sample loop; each tick calls GetProcessTimes +
//     GetProcessMemoryInfo on the child handle.
//  4. If the agent process exits before --duration elapses, the harness
//     fails with "agent exited at sample N with status X".
//  5. On normal completion, the harness sends CTRL_BREAK to the child
//     (or taskkill /F if that fails) and waits for exit.
//
// Windows-only: a stub file (main_other.go) prints a clear error on
// other platforms.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"gopkg.in/yaml.v3"
)

// thresholdsFile is the structured YAML shape the harness reads. Only
// the footprint: block is consumed here; the rest of the file (EC list
// + simulator_thresholds) is ignored so other tools can coexist.
type thresholdsFile struct {
	Footprint FootprintThresholds `yaml:"footprint"`
}

// FootprintThresholds are the machine-readable caps the harness enforces.
// All values are inclusive upper bounds ("must be strictly less than").
type FootprintThresholds struct {
	CPUAvgMaxPercent float64 `yaml:"cpu_avg_max_percent" json:"cpu_avg_max_percent"`
	CPUP95MaxPercent float64 `yaml:"cpu_p95_max_percent" json:"cpu_p95_max_percent"`
	CPUMaxPercent    float64 `yaml:"cpu_max_percent" json:"cpu_max_percent"`
	RSSAvgMaxMB      float64 `yaml:"rss_avg_max_mb" json:"rss_avg_max_mb"`
	RSSMaxMB         float64 `yaml:"rss_max_mb" json:"rss_max_mb"`
}

// defaultThresholds fall back in place if --thresholds is not supplied
// or the file omits the footprint: block. They mirror EC-2 / EC-3.
func defaultThresholds() FootprintThresholds {
	return FootprintThresholds{
		CPUAvgMaxPercent: 2.0,
		CPUP95MaxPercent: 3.0,
		CPUMaxPercent:    5.0,
		RSSAvgMaxMB:      120,
		RSSMaxMB:         150,
	}
}

func loadThresholds(path string) (FootprintThresholds, error) {
	if path == "" {
		return defaultThresholds(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return FootprintThresholds{}, fmt.Errorf("read thresholds %s: %w", path, err)
	}
	var parsed thresholdsFile
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return FootprintThresholds{}, fmt.Errorf("parse thresholds %s: %w", path, err)
	}
	t := parsed.Footprint
	// Fill any zero values from defaults — this lets partial overrides
	// in thresholds.yaml work without having to list all five keys.
	d := defaultThresholds()
	if t.CPUAvgMaxPercent == 0 {
		t.CPUAvgMaxPercent = d.CPUAvgMaxPercent
	}
	if t.CPUP95MaxPercent == 0 {
		t.CPUP95MaxPercent = d.CPUP95MaxPercent
	}
	if t.CPUMaxPercent == 0 {
		t.CPUMaxPercent = d.CPUMaxPercent
	}
	if t.RSSAvgMaxMB == 0 {
		t.RSSAvgMaxMB = d.RSSAvgMaxMB
	}
	if t.RSSMaxMB == 0 {
		t.RSSMaxMB = d.RSSMaxMB
	}
	return t, nil
}

const usageText = `
Usage:
  footprint-bench --agent <path-to-personel-agent.exe> [flags] [-- agent-args...]

Flags:
  --agent        Absolute path to personel-agent.exe (required)
  --duration     Bench window after warmup (default 5m)
  --interval     Sample interval (default 2s)
  --warmup       Post-launch settle delay before sampling (default 10s)
  --thresholds   Path to apps/qa/ci/thresholds.yaml (optional)
  --report       Path to write JSON report (optional)
  --quiet        Suppress per-sample log lines
`

type cliArgs struct {
	agentExe       string
	agentArgs      []string
	duration       time.Duration
	interval       time.Duration
	warmup         time.Duration
	thresholdsPath string
	reportPath     string
	quiet          bool
}

func parseFlags() (*cliArgs, error) {
	args := &cliArgs{}
	fs := flag.NewFlagSet("footprint-bench", flag.ContinueOnError)
	fs.StringVar(&args.agentExe, "agent", "", "Absolute path to personel-agent.exe")
	fs.DurationVar(&args.duration, "duration", 5*time.Minute, "Bench window after warmup")
	fs.DurationVar(&args.interval, "interval", 2*time.Second, "Sample interval")
	fs.DurationVar(&args.warmup, "warmup", 10*time.Second, "Post-launch settle delay before sampling")
	fs.StringVar(&args.thresholdsPath, "thresholds", "", "Path to apps/qa/ci/thresholds.yaml (optional)")
	fs.StringVar(&args.reportPath, "report", "", "Path to write JSON report (optional)")
	fs.BoolVar(&args.quiet, "quiet", false, "Suppress per-sample log lines")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return nil, err
	}
	args.agentArgs = fs.Args()

	if args.agentExe == "" {
		return nil, errors.New("--agent is required")
	}
	abs, err := filepath.Abs(args.agentExe)
	if err != nil {
		return nil, fmt.Errorf("resolve --agent path: %w", err)
	}
	args.agentExe = abs
	if _, err := os.Stat(args.agentExe); err != nil {
		return nil, fmt.Errorf("agent binary not found at %s: %w", args.agentExe, err)
	}
	if args.duration <= 0 {
		return nil, errors.New("--duration must be positive")
	}
	if args.interval <= 0 {
		return nil, errors.New("--interval must be positive")
	}
	return args, nil
}

func main() {
	args, err := parseFlags()
	if err != nil {
		fmt.Fprintf(os.Stderr, "footprint-bench: %v\n", err)
		fmt.Fprintln(os.Stderr, usageText)
		os.Exit(2)
	}

	thresholds, err := loadThresholds(args.thresholdsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "footprint-bench: %v\n", err)
		os.Exit(2)
	}

	// Trap Ctrl+C so we still kill the child agent on operator abort.
	rootCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\nfootprint-bench: received interrupt, terminating agent")
		cancel()
	}()

	report, runErr := runBench(rootCtx, args, thresholds)

	// Always try to write the JSON report, even on error, so CI has
	// diagnostics. The harness populates report.Error on failure paths.
	if args.reportPath != "" {
		if err := writeJSONReport(args.reportPath, report); err != nil {
			fmt.Fprintf(os.Stderr, "footprint-bench: write report: %v\n", err)
		}
	}

	report.Print(os.Stdout)

	if runErr != nil {
		fmt.Fprintf(os.Stderr, "\nfootprint-bench: %v\n", runErr)
		os.Exit(1)
	}
	if !report.Pass {
		os.Exit(1)
	}
}

// runBench is the main orchestration: launch → warmup → sample loop →
// terminate → aggregate.
func runBench(ctx context.Context, args *cliArgs, thresholds FootprintThresholds) (*Report, error) {
	report := NewReport(args.agentExe, args.duration, args.interval, thresholds)

	// 1. Launch the child agent.
	cmd := exec.CommandContext(ctx, args.agentExe, args.agentArgs...)
	// CREATE_NEW_PROCESS_GROUP lets us send CTRL_BREAK_EVENT later.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP,
	}
	cmd.Stdout = os.Stderr // forward agent stdout/stderr to our stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		report.Error = fmt.Sprintf("launch agent: %v", err)
		return report, fmt.Errorf("launch agent: %w", err)
	}
	report.AgentPID = uint32(cmd.Process.Pid)

	// Ensure we always kill the child before returning.
	defer func() {
		terminateAgent(cmd)
	}()

	// 2. Open a process handle for sampling.
	handle, err := windows.OpenProcess(
		windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		report.Error = fmt.Sprintf("open process handle: %v", err)
		return report, fmt.Errorf("open process %d: %w", cmd.Process.Pid, err)
	}
	defer windows.CloseHandle(handle)

	// 3. Warmup: sleep but honour cancellation + premature exit.
	if args.warmup > 0 {
		fmt.Fprintf(os.Stderr, "footprint-bench: launched agent pid=%d, warming up %v\n",
			cmd.Process.Pid, args.warmup)
		select {
		case <-ctx.Done():
			return report, ctx.Err()
		case <-time.After(args.warmup):
		}
		if exited, status := hasExited(cmd); exited {
			msg := fmt.Sprintf("agent exited during warmup with status %d", status)
			report.Error = msg
			return report, errors.New(msg)
		}
	}

	// 4. Sample loop.
	sampler := NewSampler(handle)
	// Prime the CPU delta by taking a throwaway sample.
	if _, err := sampler.Sample(); err != nil {
		report.Error = fmt.Sprintf("prime sampler: %v", err)
		return report, fmt.Errorf("prime sampler: %w", err)
	}

	ticker := time.NewTicker(args.interval)
	defer ticker.Stop()
	deadline := time.After(args.duration)
	report.StartedAt = time.Now()

	sampleIdx := 0
	for {
		select {
		case <-ctx.Done():
			report.FinishedAt = time.Now()
			report.Aggregate()
			return report, ctx.Err()
		case <-deadline:
			report.FinishedAt = time.Now()
			report.Aggregate()
			goto finish
		case <-ticker.C:
			sampleIdx++
			if exited, status := hasExited(cmd); exited {
				report.FinishedAt = time.Now()
				report.Aggregate()
				msg := fmt.Sprintf("agent exited at sample %d with status %d", sampleIdx, status)
				report.Error = msg
				return report, errors.New(msg)
			}
			s, err := sampler.Sample()
			if err != nil {
				// One bad sample is tolerable; log and continue.
				if !args.quiet {
					fmt.Fprintf(os.Stderr, "footprint-bench: sample %d error: %v\n", sampleIdx, err)
				}
				continue
			}
			report.Samples = append(report.Samples, s)
			if !args.quiet {
				fmt.Fprintf(os.Stderr, "  sample %3d  cpu=%5.2f%%  rss=%6.1f MB\n",
					sampleIdx, s.CPUPercent, float64(s.RSSBytes)/1024/1024)
			}
		}
	}

finish:
	return report, nil
}

// hasExited returns true + exit code if the child is no longer running.
// It opens a fresh PROCESS_QUERY_LIMITED_INFORMATION handle by PID and
// calls GetExitCodeProcess — this is non-consuming, unlike cmd.Wait(),
// so the caller can still kill the child afterwards.
func hasExited(cmd *exec.Cmd) (bool, uint32) {
	if cmd.Process == nil {
		return true, 0
	}
	ph, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(cmd.Process.Pid))
	if err != nil {
		// If we can't open it, it likely exited.
		return true, 0
	}
	defer windows.CloseHandle(ph)
	var code uint32
	if err := windows.GetExitCodeProcess(ph, &code); err != nil {
		return true, 0
	}
	const STILL_ACTIVE = 259
	if code == STILL_ACTIVE {
		return false, 0
	}
	return true, code
}

// terminateAgent sends a graceful CTRL_BREAK_EVENT first, then hard-kills
// after a 5 s grace period. This mirrors what a Windows SCM stop would
// do for a service, but works on a console-mode child.
func terminateAgent(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if exited, _ := hasExited(cmd); exited {
		_, _ = cmd.Process.Wait()
		return
	}

	// Attempt GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, pid).
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	procGenerateConsoleCtrlEvent := kernel32.NewProc("GenerateConsoleCtrlEvent")
	const CTRL_BREAK_EVENT = 1
	r1, _, _ := procGenerateConsoleCtrlEvent.Call(uintptr(CTRL_BREAK_EVENT), uintptr(cmd.Process.Pid))
	if r1 == 0 {
		// Graceful signal failed — fall straight through to hard kill.
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return
	}

	// Wait up to 5 s for graceful exit.
	done := make(chan struct{})
	go func() {
		_, _ = cmd.Process.Wait()
		close(done)
	}()
	select {
	case <-done:
		return
	case <-time.After(5 * time.Second):
		// Last resort: taskkill /F via Process.Kill.
		_ = cmd.Process.Kill()
		<-done
	}
}

// writeJSONReport materialises the Report struct to disk.
func writeJSONReport(path string, r *Report) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir report dir: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// sortFloat is a helper used by the percentile computation to keep the
// sort dependency isolated from the aggregation logic.
func sortFloat(s []float64) {
	sort.Float64s(s)
}
