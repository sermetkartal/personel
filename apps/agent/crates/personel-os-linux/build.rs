//! Build script for `personel-os-linux`.
//!
//! **Phase 2.1 (scaffold):** detects whether `clang` and `bpftool` are present
//! in `PATH`. If both are found the script prints a notice that CO-RE eBPF
//! compilation is available; if either is missing it emits a `cargo:warning`
//! and continues — the crate still builds with stub implementations.
//!
//! **Phase 2.2:** this script will invoke `libbpf-cargo`'s `SkeletonBuilder`
//! to compile the `.bpf.c` files under `bpf/` into skeleton structs embedded
//! directly in the agent binary via `include!`. The CO-RE (Compile Once, Run
//! Everywhere) approach means we compile against a reference kernel's BTF and
//! the resulting BPF bytecode relocates itself at load time against the host
//! kernel's BTF. No kernel headers need to be present on the deployment host.
//!
//! # Prerequisites for Phase 2.2 compilation
//!
//! * `clang` >= 14 with BPF target support (`clang -target bpf`)
//! * `bpftool` (from the `linux-tools-*` or `bpftool` package)
//! * `libbpf-dev` (or equivalent) for `<bpf/bpf_helpers.h>`
//! * Kernel BTF at `/sys/kernel/btf/vmlinux` (Linux 5.2+) or a vmlinux BTF
//!   blob placed alongside the build
//!
//! The Debian/Ubuntu install one-liner:
//! ```sh
//! apt-get install -y clang llvm bpftool libbpf-dev linux-headers-$(uname -r)
//! ```
//!
//! # Kernel version support matrix
//!
//! | Distribution       | Minimum kernel | BTF shipped |
//! |--------------------|---------------|-------------|
//! | Ubuntu 22.04 LTS   | 5.15          | Yes         |
//! | Ubuntu 24.04 LTS   | 6.8           | Yes         |
//! | RHEL 9 / Rocky 9   | 5.14          | Yes         |
//! | Debian 12          | 6.1           | Yes         |
//! | Fedora 37+         | 6.x           | Yes         |
//!
//! Kernels below 5.15 (e.g. RHEL 8, Ubuntu 20.04) are **not supported** in
//! Phase 2. See ADR 0016 §Consequences.

fn main() {
    // Only run eBPF detection on Linux; on other host platforms (macOS CI,
    // Windows dev box) skip silently — the stub modules handle everything.
    #[cfg(target_os = "linux")]
    detect_bpf_toolchain();

    // Re-run this script whenever build.rs itself changes.
    println!("cargo:rerun-if-changed=build.rs");
    // Re-run when any BPF source changes (Phase 2.2 will populate bpf/).
    println!("cargo:rerun-if-changed=bpf/");
}

/// Probe for `clang` and `bpftool` in `PATH`.
///
/// Emits either an informational note or a `cargo:warning` but never aborts
/// the build — the crate compiles as a pure stub if the toolchain is absent.
#[cfg(target_os = "linux")]
fn detect_bpf_toolchain() {
    let clang_ok = tool_in_path("clang");
    let bpftool_ok = tool_in_path("bpftool");

    match (clang_ok, bpftool_ok) {
        (true, true) => {
            // Both tools present — Phase 2.2 can compile BPF programs.
            // TODO (Phase 2.2): invoke libbpf-cargo SkeletonBuilder here.
            println!(
                "cargo:warning=personel-os-linux: clang and bpftool found. \
                 eBPF CO-RE compilation available (Phase 2.2)."
            );
        }
        (false, _) => {
            println!(
                "cargo:warning=personel-os-linux: `clang` not found in PATH. \
                 eBPF programs will not be compiled. Install clang >= 14. \
                 Stub implementations will be used."
            );
        }
        (true, false) => {
            println!(
                "cargo:warning=personel-os-linux: `bpftool` not found in PATH. \
                 eBPF skeleton generation unavailable. \
                 Install bpftool (linux-tools-generic or bpftool package). \
                 Stub implementations will be used."
            );
        }
    }
}

/// Returns `true` if `tool` resolves to an executable via `PATH`.
fn tool_in_path(tool: &str) -> bool {
    std::process::Command::new("which")
        .arg(tool)
        .output()
        .map(|o| o.status.success())
        .unwrap_or(false)
}
