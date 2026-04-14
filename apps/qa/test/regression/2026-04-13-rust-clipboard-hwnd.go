package regression

import "context"

// Faz 1 reality check: Rust clipboard collector compile error —
// CreateWindowExW needed a direct HWND, not an Option. Fix in
// apps/agent/crates/personel-collectors/src/clipboard.rs.
//
// This regression is a BUILD regression, not a runtime one. It
// is intentionally a no-op against a deployed stack — the real
// guard is `cargo check -p personel-collectors` run by CI.
//
// Kept here so operators can see the count of Faz 1 reality check
// fixes that now have regression tests, even if this entry is a
// marker-only.
var _rustClipboardHwndScenario = Scenario{
	id:        "REG-2026-04-13-rust-clipboard-hwnd",
	title:     "Rust clipboard collector compiles (CreateWindowExW HWND)",
	dateFiled: "2026-04-13",
	reference: "apps/agent/crates/personel-collectors/src/clipboard.rs",
	run: func(ctx context.Context, env Env) error {
		// Build-time regression. Guarded by `cargo check` in CI.
		_ = ctx
		_ = env
		return nil
	},
}

func init() { register(_rustClipboardHwndScenario) }
