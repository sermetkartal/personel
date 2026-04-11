//! eBPF program loader and CO-RE (Compile Once, Run Everywhere) scaffolding.
//!
//! This module is the Phase 2.1 skeleton for `libbpf-rs`-based eBPF program
//! management. In Phase 2.2, each sub-module (`process`, `network`) will load
//! a compiled `.bpf.o` skeleton from a `build.rs`-generated skeleton struct,
//! attach tracepoints, and stream events via a perf event buffer or BPF ring
//! buffer.
//!
//! # CO-RE approach
//!
//! CO-RE (Compile Once, Run Everywhere) compiles BPF C programs with
//! Clang/LLVM using BTF type information from a reference kernel. At load time
//! `libbpf` relocates the program against the *host* kernel's BTF (available
//! at `/sys/kernel/btf/vmlinux` on Linux 5.2+). This means we ship a single
//! set of BPF bytecode that runs across all supported kernel versions without
//! recompilation on the host.
//!
//! # Minimum kernel requirements
//!
//! | Kernel feature used       | Minimum version |
//! |---------------------------|-----------------|
//! | BTF at `/sys/kernel/btf/` | 5.2             |
//! | `sched_process_exec` TP   | 3.16            |
//! | BPF ring buffer           | 5.8             |
//! | `sock_ops`                | 4.13            |
//!
//! Supported distributions (ADR 0016): Ubuntu 22.04+ (5.15), RHEL 9+ (5.14),
//! Debian 12+ (6.1), Fedora 37+ (6.x). Ubuntu 20.04 and RHEL 8 are **not**
//! supported for eBPF path; they would fall back to degraded-mode polling.
//!
//! # Source layout
//!
//! BPF C source files live under `bpf/` in this crate root (placeholder for
//! Phase 2.2). `build.rs` will compile them with `libbpf-cargo::SkeletonBuilder`
//! and emit `OUT_DIR/bpf_skel.rs` for inclusion via `include!`.

pub mod network;
pub mod process;
