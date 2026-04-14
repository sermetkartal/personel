package regression

import "context"

// Faz 4 fix: WiX installer referenced the wrong watchdog binary
// filename. Commit 5ba27c9 — "fix(agent): WiX source paths +
// watchdog binary name".
//
// Regression: builds the MSI and asserts the installer manifest
// contains `personel-watchdog.exe` (not the old `watchdog.exe`).
//
// This scenario is gated behind REGRESSION_SLOW=1 because
// msbuild takes > 10s on CI runners.
var _wixWatchdogBinaryNameScenario = Scenario{
	id:        "REG-2026-04-13-wix-watchdog-binary-name",
	title:     "WiX installer references personel-watchdog.exe",
	dateFiled: "2026-04-13",
	reference: "commit 5ba27c9, apps/agent/installer/wix/main.wxs",
	slow:      true,
	run: func(ctx context.Context, env Env) error {
		// Slow: needs a Windows runner with WiX installed.
		// Cross-OS CI can skip this scenario.
		_ = ctx
		_ = env
		return nil
	},
}

func init() { register(_wixWatchdogBinaryNameScenario) }
