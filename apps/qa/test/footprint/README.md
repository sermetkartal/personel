# Agent Footprint Benchmark — Windows Instructions

## Purpose

Validates Phase 1 exit criteria:
- **EC-2**: Agent CPU < 2% average
- **EC-3**: Agent RSS < 150 MB
- **EC-4**: Agent disk footprint < 500 MB (including SQLite queue)

## Prerequisites

- Windows 10/11 or Windows Server 2019+
- Real Rust agent binary installed
- Go 1.22 on the test host
- Administrator privileges (required to open process handles)

## Steps

1. Install the Personel agent normally (or start it manually):
   ```
   personel-agent.exe --config agent.toml
   ```

2. Wait for the agent to reach steady state (~5 minutes after startup).

3. Note the agent PID:
   ```
   tasklist | findstr personel-agent
   ```

4. Build and run the footprint bench:
   ```
   cd apps/qa
   go build -o footprint-bench.exe ./cmd/footprint-bench/...
   footprint-bench.exe --pid <PID> --exe "C:\Program Files\Personel" --duration 30m
   ```

5. The bench samples CPU% and RSS every 30 seconds for 30 minutes.

6. Results are written to `footprint-report.json` and printed to stdout.

## Interpreting Results

```
Agent Footprint Benchmark Results
==================================
EC-2 CPU (avg):  1.4%    (target: <2%)    [PASS]
EC-2 CPU (max):  2.8%
EC-3 RAM (avg):  112 MB  (target: <150MB) [PASS]
EC-3 RAM (max):  128 MB
EC-4 Disk:       340 MB  (target: <500MB) [PASS]

Overall: PASS
```

## Notes on Measurement Methodology

- **CPU**: Measured via Windows Process Times API (GetProcessTimes).
  `CPU% = (kernel_time_delta + user_time_delta) / wall_time_delta * 100`.
  This accounts for multi-core — if the agent uses 2 CPUs at 1% each on a
  4-core machine, CPU% reported is ~0.5% (normalized to total available CPU).
  For Phase 1 compliance, we measure against total CPU capacity.

- **RSS**: Working Set Size from `GetProcessMemoryInfo`. This is the physical
  memory currently in use, not virtual memory.

- **Disk**: Sum of all files in the agent install directory plus the SQLite
  queue at `%AppData%\Personel\queue.db`. The SQLite queue will grow during
  network outages (up to the configured max, typically 48h of events).

## CI Integration

The footprint bench requires a Windows runner. Add to your GitHub Actions:

```yaml
footprint-bench:
  runs-on: windows-latest
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v5
      with:
        go-version: '1.22'
    - name: Build footprint bench
      run: go build -o footprint-bench.exe ./apps/qa/cmd/footprint-bench/...
    - name: Run footprint bench (requires real agent)
      run: |
        # Start real agent
        .\personel-agent.exe --config test-agent.toml &
        Start-Sleep 300  # wait for steady state
        $pid = (Get-Process personel-agent).Id
        .\footprint-bench.exe --pid $pid --duration 30m --report footprint-report.json
```
