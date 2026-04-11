// build.rs — personel-os-macos
//
// Phase 2.1: This build script is a placeholder. No Swift compilation occurs
// yet. In Phase 2.2+ this script will:
//
//   1. Invoke `swiftc` to compile `swift/PersonelESHelper/` into a Mach-O
//      bundle that is placed next to the main agent binary inside the
//      `Personel.app` bundle.
//   2. Emit `cargo:rerun-if-changed=swift/` so Cargo reruns this script only
//      when the Swift source changes.
//   3. Emit `cargo:rustc-link-lib=framework=EndpointSecurity` (and other
//      frameworks) when the Swift helper requires it. Note: the Rust agent
//      itself does NOT link EndpointSecurity directly; the Swift shim does.
//      (See ADR 0015 §"ES daemon written in Swift".)
//
// macOS-only note: this file is unconditionally compiled by Cargo on all
// platforms; the target check inside ensures we emit no instructions on
// Linux/Windows.

fn main() {
    let target_os = std::env::var("CARGO_CFG_TARGET_OS").unwrap_or_default();

    if target_os == "macos" {
        // Phase 2.1 — no Swift compilation yet. Placeholder only.
        println!("cargo:rerun-if-changed=swift/");
        println!("cargo:rerun-if-changed=build.rs");

        // Future Phase 2.2 lines (commented out until Swift sources exist):
        // println!("cargo:rustc-link-lib=framework=CoreFoundation");
        // println!("cargo:rustc-link-lib=framework=IOKit");
        // println!("cargo:rustc-link-lib=framework=ScreenCaptureKit");
        // println!("cargo:rustc-link-lib=framework=NetworkExtension");
    }
}
