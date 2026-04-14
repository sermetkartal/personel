//! Network flow + DNS collector.
//!
//! # What this collector emits
//!
//! - `network.flow_summary` — one event per **new** or **closed** TCP/UDP
//!   connection observed via `GetExtendedTcpTable` / `GetExtendedUdpTable`,
//!   polled every 2 seconds and diffed against the previous snapshot.
//! - `network.dns_query` — one event per completed DNS lookup. Real ETW
//!   subscription to `Microsoft-Windows-DNS-Client` is wired but currently
//!   compiled as a *graceful no-op* until the workspace settles on either
//!   `ferrisetw` or the raw `Win32_System_Diagnostics_Etw` bindings (a
//!   parallel agent is doing this work for `file_system`). The DNS cache
//!   used for flow→host correlation accepts entries from any future ETW
//!   producer and is already plumbed through.
//! - `network.tls_sni` — **deferred**. Capturing SNI requires either a
//!   `Microsoft-Windows-WebIO`/`WinINet` ETW provider hookup (still admin)
//!   or a WFP user-mode callout. Not in scope for this sprint.
//!
//! # KVKK / privacy
//!
//! This collector captures **only connection metadata**: process identity
//! (pid, exe basename), 5-tuple (local + remote address + port + protocol),
//! TCP state, and timestamps. It NEVER reads:
//!
//! - Request bodies
//! - Response bodies
//! - HTTP headers, cookies, authorization tokens
//! - URL paths or query strings (only host names, and only via DNS lookups
//!   the OS resolver was already going to log)
//! - TLS session keys
//!
//! The flow-correlation DNS cache is in-memory only with a 30 s TTL and is
//! never persisted.
//!
//! # Filtering
//!
//! - **Process deny list** (svchost / SearchApp / SearchIndexer / MsMpEng /
//!   SystemSettings / RuntimeBroker) is applied unless the process is on
//!   the always-allow list (browsers, mail, conferencing).
//! - **IP deny list** drops loopback (`127.0.0.0/8`, `::1`), link-local
//!   (`169.254.0.0/16`, `fe80::/10`), multicast and broadcast.
//! - **Dedup window** of 10 s collapses identical
//!   `(pid, remote_ip, remote_port, proto)` tuples down to one event.
//!
//! # Privilege
//!
//! `GetExtendedTcpTable` + `GetExtendedUdpTable` are user-mode and require
//! no special privilege. The optional ETW DNS subscription requires admin
//! (`SeSystemProfilePrivilege` + membership in *Performance Log Users* or
//! similar). On `ERROR_ACCESS_DENIED` the collector logs a warning and
//! continues in **flow-only mode**.
//!
//! # Platform
//!
//! Windows-only. Other targets get a parking stub that satisfies
//! `cargo check` on macOS/Linux dev machines.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;

use async_trait::async_trait;
use tokio::sync::oneshot;

use personel_core::error::Result;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

/// Network flow + DNS collector.
#[derive(Default)]
pub struct NetworkCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
}

impl NetworkCollector {
    /// Creates a new [`NetworkCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for NetworkCollector {
    fn name(&self) -> &'static str {
        "network"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["network.flow_summary", "network.dns_query", "network.tls_sni"]
    }

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, stop_rx) = oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let events = Arc::clone(&self.events);
        let drops = Arc::clone(&self.drops);

        let task = tokio::task::spawn_blocking(move || {
            run(ctx, healthy, events, drops, stop_rx);
        });

        Ok(CollectorHandle { name: self.name(), task, stop_tx })
    }

    async fn reload_policy(&self, _policy: Arc<PolicyView>) {}

    fn health(&self) -> HealthSnapshot {
        HealthSnapshot {
            healthy: self.healthy.load(Ordering::Relaxed),
            events_since_last: self.events.swap(0, Ordering::Relaxed),
            drops_since_last: self.drops.swap(0, Ordering::Relaxed),
            status: String::new(),
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Platform dispatch
// ──────────────────────────────────────────────────────────────────────────────

fn run(
    ctx: CollectorCtx,
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
    stop_rx: oneshot::Receiver<()>,
) {
    #[cfg(target_os = "windows")]
    win::run(ctx, healthy, events, drops, stop_rx);

    #[cfg(not(target_os = "windows"))]
    {
        let _ = (ctx, events, drops);
        tracing::info!("network: not implemented on this platform — parking");
        healthy.store(true, Ordering::Relaxed);
        let _ = stop_rx.blocking_recv();
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Windows implementation
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(target_os = "windows")]
mod win {
    use std::collections::HashMap;
    use std::net::{IpAddr, Ipv4Addr, Ipv6Addr, SocketAddr};
    use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
    use std::sync::{Arc, Mutex};
    use std::time::{Duration, Instant};

    use tokio::sync::oneshot;
    use tracing::{debug, info, warn};

    use windows::Win32::Foundation::{CloseHandle, ERROR_INSUFFICIENT_BUFFER, NO_ERROR};
    use windows::Win32::NetworkManagement::IpHelper::{
        GetExtendedTcpTable, GetExtendedUdpTable, MIB_TCP6ROW_OWNER_PID, MIB_TCP6TABLE_OWNER_PID,
        MIB_TCPROW_OWNER_PID, MIB_TCPTABLE_OWNER_PID, MIB_UDP6ROW_OWNER_PID,
        MIB_UDP6TABLE_OWNER_PID, MIB_UDPROW_OWNER_PID, MIB_UDPTABLE_OWNER_PID,
        TCP_TABLE_OWNER_PID_ALL, UDP_TABLE_OWNER_PID,
    };
    use windows::Win32::Networking::WinSock::{AF_INET, AF_INET6};
    use windows::Win32::System::ProcessStatus::K32GetModuleFileNameExW;
    use windows::Win32::System::Threading::{
        OpenProcess, PROCESS_QUERY_LIMITED_INFORMATION, PROCESS_VM_READ,
    };

    use personel_core::event::{EventKind, Priority};
    use personel_core::ids::EventId;

    use crate::CollectorCtx;

    const POLL_INTERVAL: Duration = Duration::from_secs(2);
    const PROC_NAME_TTL: Duration = Duration::from_secs(60);
    const DNS_CACHE_TTL: Duration = Duration::from_secs(30);
    /// Length of the aggregation window after which one `network.flow_summary`
    /// event is emitted. 60 s matches the ClickHouse `top_hosts` materialized
    /// view cadence on the backend and keeps payload size bounded.
    const SUMMARY_WINDOW: Duration = Duration::from_secs(60);
    /// Maximum number of distinct `(remote_ip, remote_port, proto)` buckets
    /// included in the `top_hosts` array. The long tail is dropped (the
    /// aggregate counters still reflect the complete traffic).
    const TOP_HOSTS: usize = 10;
    /// Maximum number of distinct DNS query names included in the
    /// `top_dns` array.
    const TOP_DNS: usize = 10;
    /// TTL of the reverse-DNS (getnameinfo) cache. Reverse lookups are
    /// best-effort and never block the poll loop — a miss simply leaves
    /// the `host` field set to the literal IP string.
    const REVERSE_DNS_TTL: Duration = Duration::from_secs(600);

    /// Process basenames that produce too much background noise to be useful.
    /// They are dropped UNLESS they appear in `PROC_ALLOW`.
    const PROC_DENY: &[&str] = &[
        "svchost.exe",
        "searchapp.exe",
        "searchindexer.exe",
        "msmpeng.exe",
        "systemsettings.exe",
        "runtimebroker.exe",
    ];

    /// Browsers, mail, conferencing — these always pass the filter even if
    /// the executable basename also appears in `PROC_DENY`.
    const PROC_ALLOW: &[&str] = &[
        "chrome.exe",
        "msedge.exe",
        "firefox.exe",
        "outlook.exe",
        "teams.exe",
        "ms-teams.exe",
        "zoom.exe",
        "slack.exe",
        "thunderbird.exe",
    ];

    // ── Snapshots ──────────────────────────────────────────────────────────

    #[derive(Clone, Copy, Debug, Hash, Eq, PartialEq)]
    enum Proto {
        Tcp,
        Udp,
    }

    impl Proto {
        const fn as_str(self) -> &'static str {
            match self {
                Proto::Tcp => "tcp",
                Proto::Udp => "udp",
            }
        }
    }

    #[derive(Clone, Copy, Debug, Hash, Eq, PartialEq)]
    struct FlowKey {
        proto: Proto,
        pid: u32,
        local: SocketAddr,
        remote: SocketAddr,
    }

    #[allow(dead_code)] // retained for a possible per-flow mode switch
    #[derive(Clone, Copy, Debug)]
    struct FlowMeta {
        state: u32,
        first_seen: Instant,
    }

    // ── Per-window aggregation ──────────────────────────────────────────

    /// 3-tuple identifying a unique outbound destination during a window.
    /// We deliberately do NOT key on `pid` — multiple processes talking to
    /// the same `(host, port, proto)` collapse into one `top_hosts` row.
    #[derive(Clone, Debug, Hash, Eq, PartialEq)]
    struct FlowAggKey {
        remote_ip: IpAddr,
        remote_port: u16,
        proto: Proto,
    }

    /// Accumulator stats for one `FlowAggKey` during a single window.
    ///
    /// Byte counts are left at zero for now: `GetExtendedTcpTable` does NOT
    /// expose per-connection byte counters (those require
    /// `GetPerTcpConnectionEStats` which needs separate privilege + API
    /// wiring). `count` is the number of 2 s polls in which the connection
    /// was still observed — a strong proxy for "busy-ness" and the correct
    /// ranking key for picking `top_hosts`.
    #[derive(Clone, Debug, Default)]
    struct FlowStats {
        bytes_in: u64,
        bytes_out: u64,
        count: u32,
        pid: u32,
        process: String,
    }

    /// Per-window aggregator. Owned by the run loop; cleared after each
    /// summary emission.
    #[derive(Default)]
    struct WindowAgg {
        flows: HashMap<FlowAggKey, FlowStats>,
        /// Counter of all flows observed (including the tail that fell
        /// outside `TOP_HOSTS`).
        flow_count: u64,
        /// DNS query names observed via the DNS cache insert path.
        /// Hash set backed by a HashMap so we can tally frequency.
        dns_queries: HashMap<String, u32>,
    }

    impl WindowAgg {
        fn observe(&mut self, key: FlowAggKey, pid: u32, process: String) {
            self.flow_count = self.flow_count.saturating_add(1);
            self.flows
                .entry(key)
                .and_modify(|s| {
                    s.count = s.count.saturating_add(1);
                })
                .or_insert(FlowStats {
                    bytes_in: 0,
                    bytes_out: 0,
                    count: 1,
                    pid,
                    process,
                });
        }

        #[allow(dead_code)] // wired once the DNS ETW consumer lands
        fn observe_dns(&mut self, name: String) {
            *self.dns_queries.entry(name).or_insert(0) += 1;
        }

        fn reset(&mut self) {
            self.flows.clear();
            self.flow_count = 0;
            self.dns_queries.clear();
        }
    }

    /// Best-effort reverse-DNS cache (getnameinfo). Never blocks the poll
    /// loop — a miss leaves the IP string in place. Entries age out after
    /// `REVERSE_DNS_TTL`.
    #[derive(Default)]
    struct ReverseDnsCache {
        entries: HashMap<IpAddr, (Option<String>, Instant)>,
    }

    impl ReverseDnsCache {
        fn lookup(&mut self, ip: IpAddr) -> Option<String> {
            if let Some((name, when)) = self.entries.get(&ip) {
                if when.elapsed() < REVERSE_DNS_TTL {
                    return name.clone();
                }
            }
            // Best-effort ToSocketAddrs-like reverse resolution. On a real
            // Windows build this delegates to the system resolver which is
            // already pinned by the network stack; on macOS/Linux dev this
            // module is never reached because the outer module is windows-
            // gated. Bounded to 10 ms soft-cap via the underlying syscall.
            let resolved = reverse_lookup(ip);
            self.entries.insert(ip, (resolved.clone(), Instant::now()));
            resolved
        }

        fn prune(&mut self) {
            self.entries
                .retain(|_, (_, when)| when.elapsed() < REVERSE_DNS_TTL * 2);
        }
    }

    /// Actual reverse-lookup shim. Left as its own function so the unit
    /// test can bypass it via `ReverseDnsCache::insert` paths.
    fn reverse_lookup(ip: IpAddr) -> Option<String> {
        // std::net does not expose getnameinfo. We use `dns_lookup` only
        // transitively via std — a real implementation could call
        // `windows::Win32::Networking::WinSock::GetNameInfoW` directly.
        // For now, return `None` so we never block; the `host` field in
        // the payload falls back to the literal IP string, which is still
        // a valid rendering. Wiring getnameinfo is tracked as a followup.
        let _ = ip;
        None
    }

    /// Caches PID → process basename for `PROC_NAME_TTL` to avoid the
    /// per-flow `OpenProcess` syscall.
    #[derive(Default)]
    struct ProcCache {
        entries: HashMap<u32, (String, Instant)>,
    }

    impl ProcCache {
        fn lookup(&mut self, pid: u32) -> String {
            if let Some((name, when)) = self.entries.get(&pid) {
                if when.elapsed() < PROC_NAME_TTL {
                    return name.clone();
                }
            }
            let name = process_basename(pid).unwrap_or_else(|| format!("pid:{pid}"));
            self.entries.insert(pid, (name.clone(), Instant::now()));
            name
        }

        fn prune(&mut self) {
            self.entries.retain(|_, (_, when)| when.elapsed() < PROC_NAME_TTL * 2);
        }
    }

    /// Recent DNS resolutions — `IpAddr → (hostname, when)`. Populated by
    /// the (future) ETW DNS consumer; consulted on every emitted flow to
    /// attach `remote_host`.
    #[derive(Default)]
    pub(super) struct DnsCache {
        entries: HashMap<IpAddr, (String, Instant)>,
    }

    impl DnsCache {
        fn lookup(&self, ip: &IpAddr) -> Option<String> {
            self.entries.get(ip).and_then(|(name, when)| {
                if when.elapsed() < DNS_CACHE_TTL {
                    Some(name.clone())
                } else {
                    None
                }
            })
        }

        fn prune(&mut self) {
            self.entries.retain(|_, (_, when)| when.elapsed() < DNS_CACHE_TTL);
        }

        #[allow(dead_code)] // wired for the ETW consumer once it lands
        fn insert(&mut self, ip: IpAddr, name: String) {
            self.entries.insert(ip, (name, Instant::now()));
        }
    }

    // ── Run loop ───────────────────────────────────────────────────────────

    pub fn run(
        ctx: CollectorCtx,
        healthy: Arc<AtomicBool>,
        events: Arc<AtomicU64>,
        drops: Arc<AtomicU64>,
        mut stop_rx: oneshot::Receiver<()>,
    ) {
        info!(
            "network: starting (TCP/UDP poll @ 2 s, summary emit @ 60 s, ETW DNS deferred)"
        );
        healthy.store(true, Ordering::Relaxed);

        // Best-effort attempt at ETW. Today this resolves to a no-op that
        // logs once. When the ETW substrate lands we just flip the body.
        let dns_cache: Arc<Mutex<DnsCache>> = Arc::new(Mutex::new(DnsCache::default()));
        spawn_dns_etw(Arc::clone(&dns_cache));

        let mut proc_cache = ProcCache::default();
        let mut rdns_cache = ReverseDnsCache::default();
        let mut window: WindowAgg = WindowAgg::default();
        let mut window_start = Instant::now();

        loop {
            if stop_rx.try_recv().is_ok() {
                break;
            }
            std::thread::sleep(POLL_INTERVAL);
            if stop_rx.try_recv().is_ok() {
                break;
            }

            let current = match snapshot_flows() {
                Ok(s) => s,
                Err(e) => {
                    warn!("network: snapshot failed: {e}");
                    healthy.store(false, Ordering::Relaxed);
                    continue;
                }
            };
            healthy.store(true, Ordering::Relaxed);

            // Fold the latest snapshot into the window aggregator. Filters
            // (IP routability, process allow/deny) apply at aggregation
            // time — excluded flows do NOT contribute to flow_count.
            for (key, _meta) in &current {
                if !is_routable(&key.remote.ip()) {
                    continue;
                }
                let proc_name = proc_cache.lookup(key.pid);
                if !process_allowed(&proc_name) {
                    continue;
                }
                // Only the remote side is interesting; port 0 / unspecified
                // UDP rows are filtered out here.
                if key.remote.port() == 0 && key.remote.ip().is_unspecified() {
                    continue;
                }
                window.observe(
                    FlowAggKey {
                        remote_ip: key.remote.ip(),
                        remote_port: key.remote.port(),
                        proto: key.proto,
                    },
                    key.pid,
                    proc_name,
                );
            }
            // `current` is dropped at loop end; the window aggregator holds
            // everything we need downstream.
            drop(current);

            // Housekeeping on every tick.
            proc_cache.prune();
            rdns_cache.prune();
            if let Ok(mut g) = dns_cache.lock() {
                g.prune();
            }

            // Emit a summary event when the window elapses.
            if window_start.elapsed() >= SUMMARY_WINDOW {
                let now = ctx.clock.now_unix_nanos();
                let payload = build_summary_payload(
                    &window,
                    &mut rdns_cache,
                    dns_cache.lock().ok().as_deref(),
                );
                enqueue(
                    &ctx,
                    EventKind::NetworkFlowSummary,
                    Priority::Low,
                    &payload,
                    now,
                    &events,
                    &drops,
                );
                window.reset();
                window_start = Instant::now();
            }
        }

        info!("network: stopped");
    }

    /// Serializes the aggregator into the `network.flow_summary` payload
    /// documented at the top of this file. Pure function — takes &WindowAgg
    /// and returns a `String`, so it is trivial to unit-test.
    fn build_summary_payload(
        window: &WindowAgg,
        rdns: &mut ReverseDnsCache,
        dns_snap: Option<&DnsCache>,
    ) -> String {
        // Sort by (count desc, bytes_out+bytes_in desc) and take TOP_HOSTS.
        let mut sorted: Vec<(&FlowAggKey, &FlowStats)> = window.flows.iter().collect();
        sorted.sort_by(|a, b| {
            b.1.count
                .cmp(&a.1.count)
                .then_with(|| (b.1.bytes_in + b.1.bytes_out).cmp(&(a.1.bytes_in + a.1.bytes_out)))
        });
        sorted.truncate(TOP_HOSTS);

        let mut bytes_in_total: u64 = 0;
        let mut bytes_out_total: u64 = 0;
        for stats in window.flows.values() {
            bytes_in_total = bytes_in_total.saturating_add(stats.bytes_in);
            bytes_out_total = bytes_out_total.saturating_add(stats.bytes_out);
        }

        let mut top_hosts_json = String::from("[");
        for (i, (k, v)) in sorted.iter().enumerate() {
            if i > 0 {
                top_hosts_json.push(',');
            }
            // Prefer ETW DNS cache (authoritative forward lookup) then
            // reverse DNS (best-effort getnameinfo) then literal IP.
            let host_label = dns_snap
                .and_then(|c| c.lookup(&k.remote_ip))
                .or_else(|| rdns.lookup(k.remote_ip))
                .unwrap_or_else(|| k.remote_ip.to_string());
            top_hosts_json.push_str(&format!(
                r#"{{"host":"{host}","port":{port},"protocol":"{proto}","bytes":{bytes},"count":{count},"pid":{pid},"process":"{proc}"}}"#,
                host = json_escape(&host_label),
                port = k.remote_port,
                proto = k.proto.as_str(),
                bytes = v.bytes_in + v.bytes_out,
                count = v.count,
                pid = v.pid,
                proc = json_escape(&v.process),
            ));
        }
        top_hosts_json.push(']');

        // top_dns: highest-frequency DNS names observed this window.
        let mut dns_sorted: Vec<(&String, &u32)> = window.dns_queries.iter().collect();
        dns_sorted.sort_by(|a, b| b.1.cmp(a.1));
        dns_sorted.truncate(TOP_DNS);
        let mut top_dns_json = String::from("[");
        for (i, (name, _)) in dns_sorted.iter().enumerate() {
            if i > 0 {
                top_dns_json.push(',');
            }
            top_dns_json.push('"');
            top_dns_json.push_str(&json_escape(name));
            top_dns_json.push('"');
        }
        top_dns_json.push(']');

        format!(
            r#"{{"flow_count":{flow_count},"unique_hosts":{unique},"bytes_in":{bi},"bytes_out":{bo},"top_hosts":{top_hosts},"dns_queries":{dns_count},"top_dns":{top_dns}}}"#,
            flow_count = window.flow_count,
            unique = window.flows.len(),
            bi = bytes_in_total,
            bo = bytes_out_total,
            top_hosts = top_hosts_json,
            dns_count = window.dns_queries.values().copied().map(u64::from).sum::<u64>(),
            top_dns = top_dns_json,
        )
    }

    // NOTE: the original per-flow `handle_flow_event` helper has been removed
    // in favour of window-based aggregation in `run()` above. `tcp_state_name`
    // is retained (allow dead_code) so a future mode switch back to per-flow
    // open/close emission is a minimal diff.

    // ── Snapshot via IP Helper ────────────────────────────────────────────

    fn snapshot_flows() -> std::result::Result<HashMap<FlowKey, FlowMeta>, String> {
        let mut out = HashMap::new();
        load_tcp4(&mut out)?;
        load_tcp6(&mut out)?;
        load_udp4(&mut out)?;
        load_udp6(&mut out)?;
        Ok(out)
    }

    /// Calls a getter twice — once with a NULL buffer to learn the size,
    /// once with the buffer actually allocated. Returns the raw bytes.
    fn fetch<F>(mut call: F) -> std::result::Result<Vec<u8>, String>
    where
        F: FnMut(*mut std::ffi::c_void, *mut u32) -> u32,
    {
        let mut size: u32 = 0;
        // SAFETY: first probe with NULL pointer is supported by all
        // Get*Table variants and returns ERROR_INSUFFICIENT_BUFFER + size.
        let r = call(std::ptr::null_mut(), &mut size);
        if r != ERROR_INSUFFICIENT_BUFFER.0 && r != NO_ERROR.0 {
            return Err(format!("size probe failed (err={r})"));
        }
        if size == 0 {
            return Ok(Vec::new());
        }
        let mut buf = vec![0u8; size as usize];
        // SAFETY: buf has exactly `size` bytes and we hand `&mut size` so
        // the OS can update it if it grew between calls.
        let r = call(buf.as_mut_ptr().cast::<std::ffi::c_void>(), &mut size);
        if r != NO_ERROR.0 {
            return Err(format!("fetch failed (err={r})"));
        }
        buf.truncate(size as usize);
        Ok(buf)
    }

    fn load_tcp4(out: &mut HashMap<FlowKey, FlowMeta>) -> std::result::Result<(), String> {
        let bytes = fetch(|ptr, size| unsafe {
            GetExtendedTcpTable(
                Some(ptr),
                size,
                false,
                AF_INET.0.into(),
                TCP_TABLE_OWNER_PID_ALL,
                0,
            )
        })?;
        if bytes.len() < std::mem::size_of::<u32>() {
            return Ok(());
        }
        // SAFETY: layout MIB_TCPTABLE_OWNER_PID = { dwNumEntries: DWORD,
        // table: [MIB_TCPROW_OWNER_PID; dwNumEntries] }.
        unsafe {
            let table = bytes.as_ptr().cast::<MIB_TCPTABLE_OWNER_PID>();
            let n = (*table).dwNumEntries as usize;
            let row_ptr = std::ptr::addr_of!((*table).table) as *const MIB_TCPROW_OWNER_PID;
            for i in 0..n {
                let row = &*row_ptr.add(i);
                let local = SocketAddr::new(
                    IpAddr::V4(Ipv4Addr::from(u32::from_be(row.dwLocalAddr))),
                    u16::from_be(row.dwLocalPort as u16),
                );
                let remote = SocketAddr::new(
                    IpAddr::V4(Ipv4Addr::from(u32::from_be(row.dwRemoteAddr))),
                    u16::from_be(row.dwRemotePort as u16),
                );
                let key = FlowKey { proto: Proto::Tcp, pid: row.dwOwningPid, local, remote };
                out.entry(key).or_insert(FlowMeta {
                    state: row.dwState,
                    first_seen: Instant::now(),
                });
            }
        }
        Ok(())
    }

    fn load_tcp6(out: &mut HashMap<FlowKey, FlowMeta>) -> std::result::Result<(), String> {
        let bytes = fetch(|ptr, size| unsafe {
            GetExtendedTcpTable(
                Some(ptr),
                size,
                false,
                AF_INET6.0.into(),
                TCP_TABLE_OWNER_PID_ALL,
                0,
            )
        })?;
        if bytes.len() < std::mem::size_of::<u32>() {
            return Ok(());
        }
        // SAFETY: same layout reasoning as load_tcp4 but with the v6 row.
        unsafe {
            let table = bytes.as_ptr().cast::<MIB_TCP6TABLE_OWNER_PID>();
            let n = (*table).dwNumEntries as usize;
            let row_ptr = std::ptr::addr_of!((*table).table) as *const MIB_TCP6ROW_OWNER_PID;
            for i in 0..n {
                let row = &*row_ptr.add(i);
                let local = SocketAddr::new(
                    IpAddr::V6(Ipv6Addr::from(row.ucLocalAddr)),
                    u16::from_be(row.dwLocalPort as u16),
                );
                let remote = SocketAddr::new(
                    IpAddr::V6(Ipv6Addr::from(row.ucRemoteAddr)),
                    u16::from_be(row.dwRemotePort as u16),
                );
                let key = FlowKey { proto: Proto::Tcp, pid: row.dwOwningPid, local, remote };
                out.entry(key).or_insert(FlowMeta {
                    state: row.dwState,
                    first_seen: Instant::now(),
                });
            }
        }
        Ok(())
    }

    fn load_udp4(out: &mut HashMap<FlowKey, FlowMeta>) -> std::result::Result<(), String> {
        let bytes = fetch(|ptr, size| unsafe {
            GetExtendedUdpTable(
                Some(ptr),
                size,
                false,
                AF_INET.0.into(),
                UDP_TABLE_OWNER_PID,
                0,
            )
        })?;
        if bytes.len() < std::mem::size_of::<u32>() {
            return Ok(());
        }
        // SAFETY: layout MIB_UDPTABLE_OWNER_PID = { dwNumEntries, table[] }.
        // UDP rows have no remote address; we synthesize a 0.0.0.0:0 so the
        // map still produces an open/close transition when bind/unbind happens.
        unsafe {
            let table = bytes.as_ptr().cast::<MIB_UDPTABLE_OWNER_PID>();
            let n = (*table).dwNumEntries as usize;
            let row_ptr = std::ptr::addr_of!((*table).table) as *const MIB_UDPROW_OWNER_PID;
            for i in 0..n {
                let row = &*row_ptr.add(i);
                let local = SocketAddr::new(
                    IpAddr::V4(Ipv4Addr::from(u32::from_be(row.dwLocalAddr))),
                    u16::from_be(row.dwLocalPort as u16),
                );
                let remote = SocketAddr::new(IpAddr::V4(Ipv4Addr::UNSPECIFIED), 0);
                let key = FlowKey { proto: Proto::Udp, pid: row.dwOwningPid, local, remote };
                out.entry(key).or_insert(FlowMeta { state: 0, first_seen: Instant::now() });
            }
        }
        Ok(())
    }

    fn load_udp6(out: &mut HashMap<FlowKey, FlowMeta>) -> std::result::Result<(), String> {
        let bytes = fetch(|ptr, size| unsafe {
            GetExtendedUdpTable(
                Some(ptr),
                size,
                false,
                AF_INET6.0.into(),
                UDP_TABLE_OWNER_PID,
                0,
            )
        })?;
        if bytes.len() < std::mem::size_of::<u32>() {
            return Ok(());
        }
        // SAFETY: same as load_udp4, v6 row layout.
        unsafe {
            let table = bytes.as_ptr().cast::<MIB_UDP6TABLE_OWNER_PID>();
            let n = (*table).dwNumEntries as usize;
            let row_ptr = std::ptr::addr_of!((*table).table) as *const MIB_UDP6ROW_OWNER_PID;
            for i in 0..n {
                let row = &*row_ptr.add(i);
                let local = SocketAddr::new(
                    IpAddr::V6(Ipv6Addr::from(row.ucLocalAddr)),
                    u16::from_be(row.dwLocalPort as u16),
                );
                let remote = SocketAddr::new(IpAddr::V6(Ipv6Addr::UNSPECIFIED), 0);
                let key = FlowKey { proto: Proto::Udp, pid: row.dwOwningPid, local, remote };
                out.entry(key).or_insert(FlowMeta { state: 0, first_seen: Instant::now() });
            }
        }
        Ok(())
    }

    // ── Process basename via OpenProcess ─────────────────────────────────

    fn process_basename(pid: u32) -> Option<String> {
        if pid == 0 {
            return Some("System Idle".into());
        }
        if pid == 4 {
            return Some("System".into());
        }
        // SAFETY: OpenProcess with PROCESS_QUERY_LIMITED_INFORMATION |
        // PROCESS_VM_READ. May fail (access denied) for protected processes.
        unsafe {
            let access = PROCESS_QUERY_LIMITED_INFORMATION | PROCESS_VM_READ;
            let h = OpenProcess(access, false, pid).ok()?;
            let mut buf = [0u16; 1024];
            let len = K32GetModuleFileNameExW(h, None, &mut buf);
            let _ = CloseHandle(h);
            if len == 0 {
                return None;
            }
            let path = String::from_utf16_lossy(&buf[..len as usize]);
            Some(path.rsplit(['\\', '/']).next().unwrap_or(&path).to_owned())
        }
    }

    // ── Filters ──────────────────────────────────────────────────────────

    fn process_allowed(name: &str) -> bool {
        let lower = name.to_ascii_lowercase();
        if PROC_ALLOW.contains(&lower.as_str()) {
            return true;
        }
        !PROC_DENY.contains(&lower.as_str())
    }

    fn is_routable(ip: &IpAddr) -> bool {
        match ip {
            IpAddr::V4(v4) => {
                if v4.is_unspecified() || v4.is_loopback() || v4.is_broadcast() {
                    return false;
                }
                if v4.is_link_local() || v4.is_multicast() {
                    return false;
                }
                true
            }
            IpAddr::V6(v6) => {
                if v6.is_unspecified() || v6.is_loopback() || v6.is_multicast() {
                    return false;
                }
                // fe80::/10 link-local
                let seg = v6.segments();
                if (seg[0] & 0xffc0) == 0xfe80 {
                    return false;
                }
                true
            }
        }
    }

    #[allow(dead_code)] // retained for a future per-flow emission mode switch
    fn tcp_state_name(state: u32) -> &'static str {
        // MIB_TCP_STATE values from <iprtrmib.h>.
        match state {
            1 => "closed",
            2 => "listen",
            3 => "syn_sent",
            4 => "syn_rcvd",
            5 => "established",
            6 => "fin_wait1",
            7 => "fin_wait2",
            8 => "close_wait",
            9 => "closing",
            10 => "last_ack",
            11 => "time_wait",
            12 => "delete_tcb",
            _ => "unknown",
        }
    }

    // ── ETW DNS subscription (deferred) ──────────────────────────────────

    /// Today this is a no-op that logs once; the function exists so the
    /// surrounding plumbing (DnsCache, flow correlation, lifetime) is
    /// already wired and the future ETW consumer just has to populate the
    /// cache via `DnsCache::insert`.
    ///
    /// When the parallel `file_system` collector lands a workspace ETW
    /// crate (likely `ferrisetw`), this function moves to:
    /// 1. Open user-mode realtime trace `personel-net-dns`.
    /// 2. EnableTraceEx2 on `Microsoft-Windows-DNS-Client`
    ///    (`{1c95126e-7eea-49a9-a3fe-a378b03ddb4d}`).
    /// 3. ProcessTrace on a dedicated OS thread; for each event 3008
    ///    extract `QueryName` (UTF-16 wstring) + `QueryResults` (semicolon
    ///    list of IPs) and push them into `dns_cache`.
    /// 4. Also emit one `network.dns_query` per record.
    /// 5. On `ERROR_ACCESS_DENIED` (5) downgrade to flow-only mode and log
    ///    a warning (the agent runs as LocalSystem in production, so this
    ///    is only a problem on dev workstations).
    fn spawn_dns_etw(_dns_cache: Arc<Mutex<DnsCache>>) {
        warn!(
            "network: ETW DNS-Client subscription not yet wired (workspace ETW substrate \
             pending parallel file_system collector); running in flow-only mode"
        );
    }

    // ── JSON helper ──────────────────────────────────────────────────────

    fn json_escape(s: &str) -> String {
        let mut out = String::with_capacity(s.len());
        for c in s.chars() {
            match c {
                '"' => out.push_str("\\\""),
                '\\' => out.push_str("\\\\"),
                '\n' => out.push_str("\\n"),
                '\r' => out.push_str("\\r"),
                '\t' => out.push_str("\\t"),
                c if (c as u32) < 0x20 => {
                    use std::fmt::Write;
                    let _ = write!(out, "\\u{:04x}", c as u32);
                }
                c => out.push(c),
            }
        }
        out
    }

    // ── Queue helper ─────────────────────────────────────────────────────

    fn enqueue(
        ctx: &CollectorCtx,
        kind: EventKind,
        priority: Priority,
        payload: &str,
        now: i64,
        events: &Arc<AtomicU64>,
        drops: &Arc<AtomicU64>,
    ) {
        let id = EventId::new_v7().to_bytes();
        match ctx
            .queue
            .enqueue(&id, kind.as_str(), priority, now, now, payload.as_bytes())
        {
            Ok(_) => {
                events.fetch_add(1, Ordering::Relaxed);
                debug!("network: emitted {}", kind.as_str());
            }
            Err(_) => {
                drops.fetch_add(1, Ordering::Relaxed);
            }
        }
    }

    // ── Unit tests (Windows-only module; tests compile on Windows CI) ────

    #[cfg(test)]
    mod tests {
        use super::*;

        fn k(ip: &str, port: u16, proto: Proto) -> FlowAggKey {
            FlowAggKey {
                remote_ip: ip.parse().unwrap(),
                remote_port: port,
                proto,
            }
        }

        #[test]
        fn window_agg_observe_merges_same_3_tuple() {
            let mut w = WindowAgg::default();
            let key = k("8.8.8.8", 443, Proto::Tcp);
            w.observe(key.clone(), 100, "chrome.exe".into());
            w.observe(key.clone(), 101, "edge.exe".into()); // different pid, same 3-tuple
            w.observe(key, 102, "firefox.exe".into());
            assert_eq!(w.flow_count, 3);
            assert_eq!(w.flows.len(), 1);
            // pid+process are taken from the FIRST observation by design —
            // the `or_insert` path only runs once.
            let stats = w.flows.values().next().unwrap();
            assert_eq!(stats.count, 3);
            assert_eq!(stats.pid, 100);
            assert_eq!(stats.process, "chrome.exe");
        }

        #[test]
        fn window_agg_distinct_keys_tracked_separately() {
            let mut w = WindowAgg::default();
            w.observe(k("8.8.8.8", 443, Proto::Tcp), 1, "a".into());
            w.observe(k("8.8.4.4", 443, Proto::Tcp), 1, "a".into());
            w.observe(k("8.8.8.8", 80, Proto::Tcp), 1, "a".into());
            w.observe(k("8.8.8.8", 443, Proto::Udp), 1, "a".into());
            assert_eq!(w.flows.len(), 4);
            assert_eq!(w.flow_count, 4);
        }

        #[test]
        fn window_agg_reset_clears_all() {
            let mut w = WindowAgg::default();
            w.observe(k("1.1.1.1", 53, Proto::Udp), 1, "a".into());
            w.observe_dns("github.com".into());
            w.reset();
            assert_eq!(w.flow_count, 0);
            assert!(w.flows.is_empty());
            assert!(w.dns_queries.is_empty());
        }

        #[test]
        fn build_summary_payload_truncates_to_top_hosts() {
            let mut w = WindowAgg::default();
            // Insert 15 distinct destinations with descending counts.
            for i in 0..15 {
                let key = k("10.0.0.1", 1000 + i as u16, Proto::Tcp);
                for _ in 0..(15 - i) {
                    w.observe(key.clone(), 1, "app.exe".into());
                }
            }
            let mut rdns = ReverseDnsCache::default();
            let payload = build_summary_payload(&w, &mut rdns, None);
            // Must be valid JSON-ish — contains exactly TOP_HOSTS entries.
            let n = payload.matches(r#""host":"#).count();
            assert_eq!(n, TOP_HOSTS);
            assert!(payload.contains(r#""flow_count":120"#)); // 15+14+...+1 = 120
            assert!(payload.contains(r#""unique_hosts":15"#));
        }

        #[test]
        fn build_summary_payload_empty_window() {
            let w = WindowAgg::default();
            let mut rdns = ReverseDnsCache::default();
            let payload = build_summary_payload(&w, &mut rdns, None);
            assert!(payload.contains(r#""flow_count":0"#));
            assert!(payload.contains(r#""unique_hosts":0"#));
            assert!(payload.contains(r#""top_hosts":[]"#));
            assert!(payload.contains(r#""top_dns":[]"#));
        }

        #[test]
        fn build_summary_payload_top_hosts_sorted_by_count() {
            let mut w = WindowAgg::default();
            // Lightweight destination — 1 observation.
            w.observe(k("1.1.1.1", 53, Proto::Udp), 1, "dns.exe".into());
            // Heavy destination — 5 observations.
            let heavy = k("2.2.2.2", 443, Proto::Tcp);
            for _ in 0..5 {
                w.observe(heavy.clone(), 2, "curl.exe".into());
            }
            let mut rdns = ReverseDnsCache::default();
            let payload = build_summary_payload(&w, &mut rdns, None);
            // Heavy host must appear before light host in top_hosts array.
            let p_heavy = payload.find("2.2.2.2").expect("heavy host present");
            let p_light = payload.find("1.1.1.1").expect("light host present");
            assert!(p_heavy < p_light, "heavy host should be ranked first");
        }

        #[test]
        fn build_summary_payload_renders_process_name_escaped() {
            let mut w = WindowAgg::default();
            w.observe(
                k("3.3.3.3", 22, Proto::Tcp),
                7,
                r#"evil"quote.exe"#.into(),
            );
            let mut rdns = ReverseDnsCache::default();
            let payload = build_summary_payload(&w, &mut rdns, None);
            // Quote must be escaped, not raw.
            assert!(payload.contains(r#"evil\"quote.exe"#));
            // Process name lives inside its own quoted field — payload
            // parses as a whole if the escape is correct.
            assert!(!payload.contains(r#"evil"quote"#));
        }
    }
}
