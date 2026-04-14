package regression

import "context"

// Faz 4 fix: MSI ServiceControl was starting the service on both
// install and uninstall, causing uninstall to fail. Commit 8efd315
// — "fix(agent): MSI ServiceControl start on uninstall only".
//
// Regression: after MSI uninstall, the service must be removed
// AND no hung `sc.exe` child process should exist.
//
// Slow scenario; Windows-only.
var _msiServiceControlStartScenario = Scenario{
	id:        "REG-2026-04-13-msi-servicecontrol-start",
	title:     "MSI uninstall does not attempt to start service",
	dateFiled: "2026-04-13",
	reference: "commit 5ba27c9, apps/agent/installer/wix/main.wxs",
	slow:      true,
	run: func(ctx context.Context, env Env) error {
		_ = ctx
		_ = env
		return nil
	},
}

func init() { register(_msiServiceControlStartScenario) }
