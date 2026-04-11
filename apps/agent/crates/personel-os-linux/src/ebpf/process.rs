//! eBPF process lifecycle collector.
//!
//! Attaches to the following kernel tracepoints via `libbpf-rs`:
//! * `sched/sched_process_exec` — new process execve (captures binary path,
//!   argv, uid, cgroup)
//! * `sched/sched_process_exit` — process termination (captures exit code,
//!   runtime duration)
//! * `sched/sched_process_fork` — fork events (captures parent/child PID pair)
//!
//! Events are read from a BPF ring buffer (`BPF_MAP_TYPE_RINGBUF`, kernel 5.8+)
//! and forwarded to the Personel event pipeline as [`ProcessEvent`] values.
//!
//! # Phase 2.2 implementation plan
//!
//! 1. Add `bpf/process.bpf.c` with `SEC("tp/sched/sched_process_exec")` etc.
//! 2. Update `build.rs` to invoke `libbpf-cargo::SkeletonBuilder` on that file.
//! 3. Replace stub body of [`ProcessCollector::load`] with skeleton
//!    instantiation, ring buffer setup, and background polling task.
//!
//! # BPF program design notes
//!
//! * Use `bpf_get_current_task()` to read `task_struct->comm` for the process
//!   name (16 bytes, always safe) and `task_struct->pid` / `tgid`.
//! * `bpf_d_path` (5.9+) retrieves the full executable path from the
//!   `struct path` in the `sched_process_exec` context.
//! * Arguments (`argv`) are captured via `bpf_probe_read_user_str` limited to
//!   a fixed-size buffer per argument to stay within the BPF stack limit.

use personel_core::error::{AgentError, Result};

/// A process lifecycle event.
#[derive(Debug, Clone)]
pub struct ProcessEvent {
    /// Kernel PID (= thread group leader TID for single-threaded processes).
    pub pid: u32,
    /// Parent PID.
    pub ppid: u32,
    /// Process name (comm, up to 16 bytes, kernel-truncated).
    pub comm: String,
    /// Absolute path to the executed binary, if available.
    pub exe_path: Option<String>,
    /// Kind of lifecycle event.
    pub kind: ProcessEventKind,
}

/// Distinguishes process lifecycle event types.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ProcessEventKind {
    /// `sched_process_exec` — a new program image was loaded.
    Exec,
    /// `sched_process_exit` — process terminated.
    Exit,
    /// `sched_process_fork` — a new process was forked (pre-exec).
    Fork,
}

/// eBPF process lifecycle collector.
///
/// Phase 2.2 will load a CO-RE-compiled BPF skeleton and attach it to kernel
/// tracepoints. Phase 2.1 returns `Err(Unsupported)` from [`Self::load`].
pub struct ProcessCollector {
    _priv: (),
}

impl ProcessCollector {
    /// Loads and attaches the process tracepoint BPF programs.
    ///
    /// On success, call [`Self::start_polling`] to begin receiving events.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
    ///
    /// Phase 2.2: returns [`AgentError::CollectorStart`] if:
    /// * `/sys/kernel/btf/vmlinux` is absent (BTF not available on old kernel)
    /// * `CAP_BPF` / `CAP_PERFMON` are not in the effective capability set
    /// * The tracepoint does not exist on this kernel version
    pub fn load() -> Result<Self> {
        Err(AgentError::Unsupported {
            os: "linux",
            component: "ebpf::process::load",
        })
    }

    /// Starts the ring-buffer polling task and forwards events to `sender`.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
    pub fn start_polling(
        self,
        _sender: tokio::sync::mpsc::Sender<ProcessEvent>,
    ) -> Result<tokio::task::JoinHandle<()>> {
        Err(AgentError::Unsupported {
            os: "linux",
            component: "ebpf::process::start_polling",
        })
    }
}
