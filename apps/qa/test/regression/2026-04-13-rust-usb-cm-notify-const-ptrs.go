package regression

import "context"

// Faz 1 reality check: USB collector CM_NOTIFY callback signature
// required const ptrs on modern `windows` crate versions. Fix in
// apps/agent/crates/personel-collectors/src/usb.rs.
//
// Build-time regression — guarded by `cargo check`. Marker-only
// entry so operators can see it in the regression inventory.
var _rustUsbCmNotifyConstPtrsScenario = Scenario{
	id:        "REG-2026-04-13-rust-usb-cm-notify-const-ptrs",
	title:     "Rust USB collector CM_NOTIFY callback uses const ptrs",
	dateFiled: "2026-04-13",
	reference: "apps/agent/crates/personel-collectors/src/usb.rs",
	run: func(ctx context.Context, env Env) error {
		_ = ctx
		_ = env
		return nil
	},
}

func init() { register(_rustUsbCmNotifyConstPtrsScenario) }
