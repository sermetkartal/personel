//! eBPF network flow collector.
//!
//! Attaches BPF programs to observe TCP and UDP socket activity:
//!
//! | Protocol | Hook                          | Data captured                        |
//! |----------|-------------------------------|--------------------------------------|
//! | TCP      | `kprobe:tcp_connect`          | src/dst addr+port, pid, comm         |
//! | TCP      | `kprobe:tcp_close`            | flow duration, bytes sent/recv       |
//! | UDP      | `kprobe:udp_sendmsg`          | dst addr+port, payload size (no data)|
//! | DNS      | parsed from port-53 TCP/UDP   | query name, response IPs             |
//!
//! DNS parsing is performed in user space from the raw socket events; the BPF
//! program only records the socket fd and buffer pointer, and the Rust side
//! reads the payload via `/proc/{pid}/fd/{fd}` (read-only, kernel 5.6+) or
//! via a BPF `bpf_probe_read` of the msghdr buffer. Phase 2 starts with
//! socket-level metadata (no payload); DNS parsing and UDP aggregation are
//! Phase 2.2+ work items.
//!
//! # Aggregation model
//!
//! To stay within CPU budget, UDP events are aggregated per (src, dst, port)
//! tuple in a BPF hash map and flushed to the ring buffer once per second.
//! TCP events are per-connection (connect + close pair).
//!
//! # Phase 2.2 implementation plan
//!
//! 1. Add `bpf/network.bpf.c` with kprobe/kretprobe on `tcp_connect`,
//!    `tcp_close`, `udp_sendmsg`.
//! 2. Update `build.rs` skeleton builder for the new source file.
//! 3. Replace stub body of [`NetworkCollector::load`] with skeleton init and
//!    ring-buffer consumer task.

use personel_core::error::{AgentError, Result};
use std::net::IpAddr;

/// A network flow event.
#[derive(Debug, Clone)]
pub struct NetworkEvent {
    /// Source IP address.
    pub src_addr: IpAddr,
    /// Source port.
    pub src_port: u16,
    /// Destination IP address.
    pub dst_addr: IpAddr,
    /// Destination port.
    pub dst_port: u16,
    /// PID of the process that initiated the connection.
    pub pid: u32,
    /// Process name (comm, up to 16 bytes).
    pub comm: String,
    /// Transport protocol.
    pub protocol: NetworkProtocol,
    /// Event kind (connect, close, or UDP send aggregate).
    pub kind: NetworkEventKind,
}

/// Transport-layer protocol.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NetworkProtocol {
    /// Transmission Control Protocol.
    Tcp,
    /// User Datagram Protocol.
    Udp,
}

/// Network event kind.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NetworkEventKind {
    /// TCP connection initiated (`tcp_connect` returned successfully).
    TcpConnect,
    /// TCP connection closed (`tcp_close`).
    TcpClose,
    /// Aggregated UDP send events flushed from BPF hash map.
    UdpSendAggregate,
}

/// eBPF network flow collector.
///
/// Phase 2.2 will load a CO-RE-compiled BPF skeleton with kprobes on
/// `tcp_connect`, `tcp_close`, and `udp_sendmsg`. Phase 2.1 returns
/// `Err(Unsupported)` from [`Self::load`].
pub struct NetworkCollector {
    _priv: (),
}

impl NetworkCollector {
    /// Loads and attaches the network kprobe BPF programs.
    ///
    /// On success, call [`Self::start_polling`] to begin receiving flow events.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
    ///
    /// Phase 2.2: returns [`AgentError::CollectorStart`] if:
    /// * kprobe attach is blocked by SELinux/AppArmor policy
    /// * `CAP_BPF` / `CAP_NET_ADMIN` are absent
    /// * BTF is unavailable
    pub fn load() -> Result<Self> {
        Err(AgentError::Unsupported {
            os: "linux",
            component: "ebpf::network::load",
        })
    }

    /// Starts the ring-buffer consumer task, forwarding events to `sender`.
    ///
    /// # Errors
    ///
    /// Returns [`AgentError::Unsupported`] in Phase 2.1 (scaffold).
    pub fn start_polling(
        self,
        _sender: tokio::sync::mpsc::Sender<NetworkEvent>,
    ) -> Result<tokio::task::JoinHandle<()>> {
        Err(AgentError::Unsupported {
            os: "linux",
            component: "ebpf::network::start_polling",
        })
    }
}
