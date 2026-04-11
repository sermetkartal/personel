# bpf/ — eBPF CO-RE source files (Phase 2.2)

This directory will contain the C source files for Personel's CO-RE
(Compile Once, Run Everywhere) eBPF programs. It is intentionally empty in
Phase 2.1.

## Planned programs

| File | Tracepoints / kprobes | Phase |
|------|-----------------------|-------|
| `process.bpf.c` | `sched/sched_process_exec`, `sched/sched_process_exit`, `sched/sched_process_fork` | 2.2 |
| `network.bpf.c` | `kprobe:tcp_connect`, `kprobe:tcp_close`, `kprobe:udp_sendmsg` | 2.2 |

## CO-RE approach

Programs are compiled with `clang -target bpf` against the BTF type
information embedded in a reference kernel. At load time `libbpf` performs
BPF CO-RE relocation against `/sys/kernel/btf/vmlinux` on the host, making
the same bytecode work across all supported kernel versions without
recompilation.

`build.rs` invokes `libbpf-cargo::SkeletonBuilder` to produce Rust skeleton
structs in `$OUT_DIR/bpf_skel.rs`. These skeletons are then `include!`-d in
`src/ebpf/process.rs` and `src/ebpf/network.rs`.

## Build prerequisites

```sh
# Debian/Ubuntu
apt-get install -y clang llvm bpftool libbpf-dev linux-headers-$(uname -r)

# RHEL/Rocky/Alma
dnf install -y clang llvm bpftool libbpf-devel kernel-headers
```

Minimum: `clang` >= 14 with BPF target, `bpftool` for skeleton generation.

## Kernel support matrix

| Distribution        | Kernel   | BTF shipped |
|---------------------|----------|-------------|
| Ubuntu 22.04 LTS    | 5.15     | Yes         |
| Ubuntu 24.04 LTS    | 6.8      | Yes         |
| RHEL 9 / Rocky 9    | 5.14     | Yes         |
| Debian 12           | 6.1      | Yes         |
| Fedora 37+          | 6.x      | Yes         |

Kernels below 5.15 (e.g. RHEL 8, Ubuntu 20.04) are not supported by the
eBPF path. The collector falls back to degraded-mode `/proc` polling on
unsupported kernels.
