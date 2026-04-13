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
    use tracing::{debug, info, trace, warn};

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
    const DEDUP_WINDOW: Duration = Duration::from_secs(10);

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

    #[derive(Clone, Copy, Debug)]
    struct FlowMeta {
        state: u32,
        first_seen: Instant,
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
        info!("network: starting (TCP/UDP poll @ 2 s, ETW DNS deferred)");
        healthy.store(true, Ordering::Relaxed);

        // Best-effort attempt at ETW. Today this resolves to a no-op that
        // logs once. When the ETW substrate lands we just flip the body.
        let dns_cache: Arc<Mutex<DnsCache>> = Arc::new(Mutex::new(DnsCache::default()));
        spawn_dns_etw(Arc::clone(&dns_cache));

        let mut prev: HashMap<FlowKey, FlowMeta> = HashMap::new();
        let mut proc_cache = ProcCache::default();
        let mut dedup: HashMap<(u32, IpAddr, u16, Proto), Instant> = HashMap::new();

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

            let now = ctx.clock.now_unix_nanos();
            let dns_snap = dns_cache.lock().ok();

            // New flows.
            for (key, meta) in &current {
                if prev.contains_key(key) {
                    continue;
                }
                handle_flow_event(
                    &ctx,
                    *key,
                    *meta,
                    /* event = */ "open",
                    now,
                    &mut proc_cache,
                    dns_snap.as_deref(),
                    &mut dedup,
                    &events,
                    &drops,
                );
            }

            // Closed flows.
            for (key, meta) in &prev {
                if current.contains_key(key) {
                    continue;
                }
                handle_flow_event(
                    &ctx,
                    *key,
                    *meta,
                    /* event = */ "close",
                    now,
                    &mut proc_cache,
                    dns_snap.as_deref(),
                    &mut dedup,
                    &events,
                    &drops,
                );
            }

            drop(dns_snap);
            prev = current;

            // Housekeeping every loop tick is cheap; both maps stay small.
            proc_cache.prune();
            if let Ok(mut g) = dns_cache.lock() {
                g.prune();
            }
            let cutoff = Instant::now();
            dedup.retain(|_, when| cutoff.duration_since(*when) < DEDUP_WINDOW * 2);
        }

        info!("network: stopped");
    }

    #[allow(clippy::too_many_arguments)]
    fn handle_flow_event(
        ctx: &CollectorCtx,
        key: FlowKey,
        meta: FlowMeta,
        event: &'static str,
        now: i64,
        proc_cache: &mut ProcCache,
        dns_snap: Option<&DnsCache>,
        dedup: &mut HashMap<(u32, IpAddr, u16, Proto), Instant>,
        events: &Arc<AtomicU64>,
        drops: &Arc<AtomicU64>,
    ) {
        // IP filter first — cheapest reject path.
        if !is_routable(&key.remote.ip()) {
            return;
        }

        let proc_name = proc_cache.lookup(key.pid);
        if !process_allowed(&proc_name) {
            return;
        }

        // 10 s dedup keyed on (pid, remote_ip, remote_port, proto).
        let dkey = (key.pid, key.remote.ip(), key.remote.port(), key.proto);
        let now_inst = Instant::now();
        if let Some(when) = dedup.get(&dkey) {
            if now_inst.duration_since(*when) < DEDUP_WINDOW {
                trace!(?dkey, "network: dedup hit");
                return;
            }
        }
        dedup.insert(dkey, now_inst);

        let remote_host = dns_snap.and_then(|c| c.lookup(&key.remote.ip()));

        let state_str = if matches!(key.proto, Proto::Tcp) {
            tcp_state_name(meta.state)
        } else {
            "stateless"
        };

        let payload = format!(
            r#"{{"event":"{event}","proto":"{proto}","state":"{state}","pid":{pid},"process_name":"{pname}","local":"{local}","remote":"{remote}","remote_host":{host},"first_seen_ms":{age}}}"#,
            event = event,
            proto = key.proto.as_str(),
            state = state_str,
            pid = key.pid,
            pname = json_escape(&proc_name),
            local = key.local,
            remote = key.remote,
            host = match remote_host {
                Some(h) => format!("\"{}\"", json_escape(&h)),
                None => "null".to_string(),
            },
            age = meta.first_seen.elapsed().as_millis(),
        );

        enqueue(ctx, EventKind::NetworkFlowSummary, Priority::Low, &payload, now, events, drops);
    }

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
}
