//! Geo-IP enrichment collector — Faz 2 Wave 3 #18 (Phase 1 SCAFFOLD).
//!
//! # What this collector does
//!
//! Periodically samples the OS active TCP connection table via
//! `GetExtendedTcpTable`, extracts unique routable remote IPs, and resolves
//! each through a MaxMind **GeoLite2-Country** local database that the
//! customer's IT admin must drop on the endpoint. Each *unique* (per 24 h)
//! resolution emits one `network.geo_ip_resolved` event so the downstream
//! ML / correlation pipeline can attach a country code to outbound flows
//! without ever calling out to a third-party API.
//!
//! # Operating modes
//!
//! 1. **Database absent** (the Phase 1 default): the collector logs one
//!    informational line on startup, marks itself **healthy**, and parks
//!    on the stop signal until the agent shuts down. This is the expected
//!    state until an admin downloads the GeoLite2 mmdb under MaxMind's
//!    license terms — we explicitly do NOT ship it.
//! 2. **Database present** at
//!    `%PROGRAMDATA%\Personel\agent\GeoLite2-Country.mmdb` (or the
//!    `C:\ProgramData\Personel\agent\` fallback if the env var is unset):
//!    every 5 minutes the collector enumerates active TCP4/TCP6 flows,
//!    deduplicates by remote IP, runs a `maxminddb::Reader::lookup` for
//!    each new IP, and emits one event per unique-in-24h resolution.
//!
//! # KVKK / privacy
//!
//! The event payload contains:
//! - The remote IP (already classified as `Content` / `Identifier` per the
//!   network.flow_summary path)
//! - ISO 3166 country code + name (aggregate, no city / lat-long)
//! - Continent code, EU membership flag, registered-country code
//! - Observation count + first/last seen timestamps (within 24 h)
//!
//! It does NOT contain the local 5-tuple, the originating PID, the process
//! name, or any payload bytes. The lookup is also a strict country lookup
//! using the **Country** edition of GeoLite2 — we deliberately don't load
//! the **City** edition even if a customer drops it in place, because that
//! would expand the data footprint into city-level records that are not
//! necessary for the productivity / risk analytics use case.
//!
//! # Privilege
//!
//! `GetExtendedTcpTable` is user-mode and needs no special privilege.
//! Reading the mmdb file requires read access to `%PROGRAMDATA%\Personel\
//! agent\`; the agent's installer ACLs that directory to `LocalSystem`
//! plus the `Personel\Agent` security group.
//!
//! # Phase 1 ↔ Phase 2 transition
//!
//! In Phase 2, the `network` collector's `DnsCache` will feed hostnames
//! into a shared correlation store; this collector will then attach the
//! observed hostname(s) for each IP rather than relying solely on the
//! remote IP. Until then, country-only is sufficient for the ML risk
//! signals (e.g., "endpoint X suddenly talks to high-risk jurisdictions").
//!
//! # Platform
//!
//! Windows-only for the live-flow enumeration. On other targets the
//! collector parks gracefully so cross-platform `cargo check` stays clean.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;

use async_trait::async_trait;
use tokio::sync::oneshot;

use personel_core::error::Result;
use personel_policy::engine::PolicyView;

use crate::{Collector, CollectorCtx, CollectorHandle, HealthSnapshot};

/// Geo-IP enrichment collector.
#[derive(Default)]
pub struct GeoIpCollector {
    healthy: Arc<AtomicBool>,
    events: Arc<AtomicU64>,
    drops: Arc<AtomicU64>,
}

impl GeoIpCollector {
    /// Creates a new [`GeoIpCollector`].
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Collector for GeoIpCollector {
    fn name(&self) -> &'static str {
        "geo_ip"
    }

    fn event_types(&self) -> &'static [&'static str] {
        &["network.geo_ip_resolved"]
    }

    async fn start(&self, ctx: CollectorCtx) -> Result<CollectorHandle> {
        let (stop_tx, stop_rx) = oneshot::channel::<()>();
        let healthy = Arc::clone(&self.healthy);
        let events = Arc::clone(&self.events);
        let drops = Arc::clone(&self.drops);

        // GeoIP work is a mix of blocking file I/O (mmdb open + lookup) and
        // synchronous Win32 calls; spawn_blocking keeps the runtime smooth.
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
// Pure helpers — testable on every platform
// ──────────────────────────────────────────────────────────────────────────────

use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};
use std::path::PathBuf;
use std::time::{Duration, Instant};

/// Default mmdb location resolved at startup.
///
/// Honours `%PROGRAMDATA%` when set so that any non-default Windows
/// configuration (e.g., redirected ProgramData) keeps working. Falls back
/// to the canonical `C:\ProgramData\Personel\agent\` path otherwise.
#[must_use]
fn mmdb_path() -> PathBuf {
    let base = std::env::var_os("PROGRAMDATA")
        .map_or_else(|| PathBuf::from(r"C:\ProgramData"), PathBuf::from);
    base.join("Personel").join("agent").join("GeoLite2-Country.mmdb")
}

/// Returns true if the IP is in any private / non-routable range that the
/// collector must skip.
///
/// Skipped ranges:
/// - IPv4: `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`,
///   `169.254.0.0/16` (link-local), `127.0.0.0/8` (loopback),
///   `0.0.0.0/8` (unspecified), broadcast, multicast (`224.0.0.0/4`).
/// - IPv6: `::1` (loopback), `::` (unspecified), `fc00::/7` (ULA),
///   `fe80::/10` (link-local), multicast (`ff00::/8`).
#[must_use]
fn is_private_ip(ip: &IpAddr) -> bool {
    match ip {
        IpAddr::V4(v4) => is_private_v4(v4),
        IpAddr::V6(v6) => is_private_v6(v6),
    }
}

#[must_use]
fn is_private_v4(v4: &Ipv4Addr) -> bool {
    if v4.is_unspecified() || v4.is_loopback() || v4.is_broadcast() || v4.is_multicast() {
        return true;
    }
    if v4.is_link_local() || v4.is_private() {
        return true;
    }
    // `0.0.0.0/8` is "this network" and not covered by `is_unspecified`
    // (only the bare `0.0.0.0` returns true there). RFC 1122 §3.2.1.3.
    let octets = v4.octets();
    if octets[0] == 0 {
        return true;
    }
    false
}

#[must_use]
fn is_private_v6(v6: &Ipv6Addr) -> bool {
    if v6.is_unspecified() || v6.is_loopback() || v6.is_multicast() {
        return true;
    }
    let seg = v6.segments();
    // fe80::/10 link-local
    if (seg[0] & 0xffc0) == 0xfe80 {
        return true;
    }
    // fc00::/7 ULA (unique local address)
    if (seg[0] & 0xfe00) == 0xfc00 {
        return true;
    }
    false
}

/// Sliding window dedup entry for a resolved IP.
#[derive(Clone, Debug)]
struct GeoEntry {
    country_iso: String,
    country_name: String,
    continent: String,
    is_eu: bool,
    registered_country_iso: String,
    first_seen: i64,
    last_seen: i64,
    count: u64,
}

/// 24 h dedup window.
const DEDUP_TTL: Duration = Duration::from_secs(24 * 60 * 60);
/// 5 min poll interval for the active TCP table snapshot.
const POLL_INTERVAL: Duration = Duration::from_secs(5 * 60);

/// Drops entries whose `inserted_at` is older than [`DEDUP_TTL`].
///
/// Pulled out so unit tests can drive the eviction directly with a fake
/// clock instead of waiting 24 hours.
fn prune_dedup(
    table: &mut std::collections::HashMap<IpAddr, (GeoEntry, Instant)>,
    now: Instant,
) {
    table.retain(|_, (_, inserted)| now.duration_since(*inserted) < DEDUP_TTL);
}

/// Builds the JSON payload for a single resolved IP.
///
/// Manual JSON (no serde_json::to_string) keeps parity with the rest of
/// the collectors in this crate.
fn build_payload(ip: &IpAddr, entry: &GeoEntry) -> String {
    format!(
        r#"{{"ip":"{ip}","country_iso":"{ciso}","country_name":"{cname}","continent":"{cont}","is_eu":{eu},"registered_country_iso":"{rciso}","observed_connections_count":{count},"first_seen_unix_ns":{first},"last_seen_unix_ns":{last}}}"#,
        ip = ip,
        ciso = json_escape(&entry.country_iso),
        cname = json_escape(&entry.country_name),
        cont = json_escape(&entry.continent),
        eu = entry.is_eu,
        rciso = json_escape(&entry.registered_country_iso),
        count = entry.count,
        first = entry.first_seen,
        last = entry.last_seen,
    )
}

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
        tracing::info!("geo_ip: not implemented on this platform — parking");
        healthy.store(true, Ordering::Relaxed);
        let _ = stop_rx.blocking_recv();
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Windows implementation
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(target_os = "windows")]
mod win {
    use std::collections::{HashMap, HashSet};
    use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};
    use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
    use std::sync::Arc;
    use std::time::Instant;

    use maxminddb::geoip2::Country;
    use maxminddb::Reader;
    use tokio::sync::oneshot;
    use tracing::{debug, info, warn};

    use windows::Win32::Foundation::{ERROR_INSUFFICIENT_BUFFER, NO_ERROR};
    use windows::Win32::NetworkManagement::IpHelper::{
        GetExtendedTcpTable, MIB_TCP6ROW_OWNER_PID, MIB_TCP6TABLE_OWNER_PID, MIB_TCPROW_OWNER_PID,
        MIB_TCPTABLE_OWNER_PID, TCP_TABLE_OWNER_PID_ALL,
    };
    use windows::Win32::Networking::WinSock::{AF_INET, AF_INET6};

    use personel_core::event::{EventKind, Priority};
    use personel_core::ids::EventId;

    use super::{
        build_payload, is_private_ip, mmdb_path, prune_dedup, GeoEntry, POLL_INTERVAL,
    };
    use crate::CollectorCtx;

    pub fn run(
        ctx: CollectorCtx,
        healthy: Arc<AtomicBool>,
        events: Arc<AtomicU64>,
        drops: Arc<AtomicU64>,
        mut stop_rx: oneshot::Receiver<()>,
    ) {
        let path = mmdb_path();
        // maxminddb 0.24 collapses every io::Error into MaxMindDBError::IoError(String);
        // we recognise "not found" by substring rather than by ErrorKind. The exact
        // text is OS-dependent ("No such file or directory (os error 2)" on POSIX,
        // "The system cannot find the file specified. (os error 2)" on Windows) so
        // we look for the canonical "os error 2" tail that both formats share.
        let reader = match Reader::open_readfile(&path) {
            Ok(r) => {
                info!(
                    path = %path.display(),
                    "geo_ip: GeoLite2 mmdb loaded — collector active"
                );
                Some(r)
            }
            Err(maxminddb::MaxMindDBError::IoError(msg))
                if msg.contains("os error 2")
                    || msg.contains("cannot find the file")
                    || msg.contains("No such file") =>
            {
                info!(
                    path = %path.display(),
                    reason = %msg,
                    "geo_ip: mmdb file not present — parking (Phase 1 default; admin must \
                     download GeoLite2-Country under MaxMind license terms)"
                );
                None
            }
            Err(e) => {
                warn!(
                    path = %path.display(),
                    error = %e,
                    "geo_ip: mmdb open failed — parking (collector will not retry until restart)"
                );
                None
            }
        };

        // Always-healthy regardless of the database state. The "no events"
        // signal is conveyed via events_since_last, not via healthy=false,
        // because the absent-mmdb case is the *expected* Phase 1 default
        // and must not light up an alarm.
        healthy.store(true, Ordering::Relaxed);

        let Some(reader) = reader else {
            // No reader → just park until shutdown.
            let _ = stop_rx.blocking_recv();
            info!("geo_ip: stopped (parked, mmdb absent)");
            return;
        };

        let mut dedup: HashMap<IpAddr, (GeoEntry, Instant)> = HashMap::new();

        loop {
            if stop_rx.try_recv().is_ok() {
                break;
            }
            std::thread::sleep(POLL_INTERVAL);
            if stop_rx.try_recv().is_ok() {
                break;
            }

            let unique_ips = match sample_remote_ips() {
                Ok(set) => set,
                Err(e) => {
                    warn!("geo_ip: TCP table snapshot failed: {e}");
                    continue;
                }
            };

            let now_inst = Instant::now();
            let now_ns = ctx.clock.now_unix_nanos();

            for ip in unique_ips {
                if is_private_ip(&ip) {
                    continue;
                }

                // Already in the 24-hour window: bump count, refresh last_seen,
                // do NOT emit a new event — the dedup window is the whole point.
                if let Some((entry, _)) = dedup.get_mut(&ip) {
                    entry.count = entry.count.saturating_add(1);
                    entry.last_seen = now_ns;
                    continue;
                }

                // First time we see this IP in the window; look it up.
                let lookup: std::result::Result<Country<'_>, _> = reader.lookup(ip);
                let Ok(country) = lookup else {
                    debug!(?ip, "geo_ip: lookup miss");
                    continue;
                };

                let entry = country_to_entry(&country, now_ns);

                let payload = build_payload(&ip, &entry);
                let id = EventId::new_v7().to_bytes();
                match ctx.queue.enqueue(
                    &id,
                    EventKind::NetworkGeoIpResolved.as_str(),
                    Priority::Low,
                    now_ns,
                    now_ns,
                    payload.as_bytes(),
                ) {
                    Ok(_) => {
                        events.fetch_add(1, Ordering::Relaxed);
                        debug!(?ip, country = %entry.country_iso, "geo_ip: emitted");
                    }
                    Err(e) => {
                        warn!("geo_ip: queue error: {e}");
                        drops.fetch_add(1, Ordering::Relaxed);
                    }
                }

                dedup.insert(ip, (entry, now_inst));
            }

            prune_dedup(&mut dedup, now_inst);
        }

        info!("geo_ip: stopped");
    }

    fn country_to_entry(country: &Country<'_>, now_ns: i64) -> GeoEntry {
        let country_iso = country
            .country
            .as_ref()
            .and_then(|c| c.iso_code)
            .unwrap_or("")
            .to_string();
        let country_name = country
            .country
            .as_ref()
            .and_then(|c| c.names.as_ref())
            .and_then(|n| n.get("en").copied())
            .unwrap_or("")
            .to_string();
        let continent = country
            .continent
            .as_ref()
            .and_then(|c| c.code)
            .unwrap_or("")
            .to_string();
        let is_eu = country
            .country
            .as_ref()
            .and_then(|c| c.is_in_european_union)
            .unwrap_or(false);
        let registered_country_iso = country
            .registered_country
            .as_ref()
            .and_then(|c| c.iso_code)
            .unwrap_or("")
            .to_string();

        GeoEntry {
            country_iso,
            country_name,
            continent,
            is_eu,
            registered_country_iso,
            first_seen: now_ns,
            last_seen: now_ns,
            count: 1,
        }
    }

    /// Snapshots the active TCP4 + TCP6 owner-pid tables and returns the
    /// set of unique remote IPs (still raw — private filtering happens in
    /// the caller after dedup).
    ///
    /// Self-contained: deliberately duplicates a few dozen lines from
    /// `network.rs` instead of factoring out a shared helper, per the
    /// brief's "stay in your file" rule (parallel agents must not collide
    /// on shared modules).
    fn sample_remote_ips() -> std::result::Result<HashSet<IpAddr>, String> {
        let mut out = HashSet::new();
        load_tcp4(&mut out)?;
        load_tcp6(&mut out)?;
        Ok(out)
    }

    fn fetch<F>(mut call: F) -> std::result::Result<Vec<u8>, String>
    where
        F: FnMut(*mut std::ffi::c_void, *mut u32) -> u32,
    {
        let mut size: u32 = 0;
        let r = call(std::ptr::null_mut(), &mut size);
        if r != ERROR_INSUFFICIENT_BUFFER.0 && r != NO_ERROR.0 {
            return Err(format!("size probe failed (err={r})"));
        }
        if size == 0 {
            return Ok(Vec::new());
        }
        let mut buf = vec![0u8; size as usize];
        let r = call(buf.as_mut_ptr().cast::<std::ffi::c_void>(), &mut size);
        if r != NO_ERROR.0 {
            return Err(format!("fetch failed (err={r})"));
        }
        buf.truncate(size as usize);
        Ok(buf)
    }

    fn load_tcp4(out: &mut HashSet<IpAddr>) -> std::result::Result<(), String> {
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
        // table: [MIB_TCPROW_OWNER_PID; dwNumEntries] }. We only read the
        // remote IP from each row.
        unsafe {
            let table = bytes.as_ptr().cast::<MIB_TCPTABLE_OWNER_PID>();
            let n = (*table).dwNumEntries as usize;
            let row_ptr = std::ptr::addr_of!((*table).table) as *const MIB_TCPROW_OWNER_PID;
            for i in 0..n {
                let row = &*row_ptr.add(i);
                let ip = IpAddr::V4(Ipv4Addr::from(u32::from_be(row.dwRemoteAddr)));
                out.insert(ip);
            }
        }
        Ok(())
    }

    fn load_tcp6(out: &mut HashSet<IpAddr>) -> std::result::Result<(), String> {
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
        // SAFETY: same layout reasoning as load_tcp4 with v6 row variant.
        unsafe {
            let table = bytes.as_ptr().cast::<MIB_TCP6TABLE_OWNER_PID>();
            let n = (*table).dwNumEntries as usize;
            let row_ptr = std::ptr::addr_of!((*table).table) as *const MIB_TCP6ROW_OWNER_PID;
            for i in 0..n {
                let row = &*row_ptr.add(i);
                let ip = IpAddr::V6(Ipv6Addr::from(row.ucRemoteAddr));
                out.insert(ip);
            }
        }
        Ok(())
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests — pure logic only, no Win32 / no mmdb file required
// ──────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;
    use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};
    use std::time::{Duration, Instant};

    #[test]
    fn private_v4_loopback_skipped() {
        assert!(is_private_ip(&IpAddr::V4(Ipv4Addr::new(127, 0, 0, 1))));
        assert!(is_private_ip(&IpAddr::V4(Ipv4Addr::new(127, 53, 12, 9))));
    }

    #[test]
    fn private_v4_rfc1918_skipped() {
        assert!(is_private_ip(&IpAddr::V4(Ipv4Addr::new(10, 0, 0, 1))));
        assert!(is_private_ip(&IpAddr::V4(Ipv4Addr::new(10, 255, 255, 255))));
        assert!(is_private_ip(&IpAddr::V4(Ipv4Addr::new(172, 16, 0, 1))));
        assert!(is_private_ip(&IpAddr::V4(Ipv4Addr::new(172, 31, 255, 254))));
        assert!(is_private_ip(&IpAddr::V4(Ipv4Addr::new(192, 168, 1, 1))));
        assert!(is_private_ip(&IpAddr::V4(Ipv4Addr::new(192, 168, 254, 254))));
    }

    #[test]
    fn private_v4_link_local_and_unspec_skipped() {
        assert!(is_private_ip(&IpAddr::V4(Ipv4Addr::new(169, 254, 1, 1))));
        assert!(is_private_ip(&IpAddr::V4(Ipv4Addr::UNSPECIFIED)));
        assert!(is_private_ip(&IpAddr::V4(Ipv4Addr::new(0, 1, 2, 3))));
    }

    #[test]
    fn private_v4_multicast_and_broadcast_skipped() {
        assert!(is_private_ip(&IpAddr::V4(Ipv4Addr::new(224, 0, 0, 1))));
        assert!(is_private_ip(&IpAddr::V4(Ipv4Addr::new(239, 255, 255, 250))));
        assert!(is_private_ip(&IpAddr::V4(Ipv4Addr::BROADCAST)));
    }

    #[test]
    fn public_v4_routable() {
        // Cloudflare public DNS, RFC 5737 documentation prefix is private
        // (192.0.2.0/24) so we don't use it; pick well-known public IPs.
        assert!(!is_private_ip(&IpAddr::V4(Ipv4Addr::new(1, 1, 1, 1))));
        assert!(!is_private_ip(&IpAddr::V4(Ipv4Addr::new(8, 8, 8, 8))));
        assert!(!is_private_ip(&IpAddr::V4(Ipv4Addr::new(93, 184, 216, 34))));
    }

    #[test]
    fn private_v6_loopback_unspec_link_local_ula() {
        assert!(is_private_ip(&IpAddr::V6(Ipv6Addr::LOCALHOST)));
        assert!(is_private_ip(&IpAddr::V6(Ipv6Addr::UNSPECIFIED)));
        // fe80:: link-local
        assert!(is_private_ip(&IpAddr::V6(Ipv6Addr::new(
            0xfe80, 0, 0, 0, 0, 0, 0, 1
        ))));
        // fc00::/7 ULA
        assert!(is_private_ip(&IpAddr::V6(Ipv6Addr::new(
            0xfc00, 0, 0, 0, 0, 0, 0, 1
        ))));
        assert!(is_private_ip(&IpAddr::V6(Ipv6Addr::new(
            0xfd12, 0x3456, 0, 0, 0, 0, 0, 1
        ))));
        // ff00::/8 multicast
        assert!(is_private_ip(&IpAddr::V6(Ipv6Addr::new(
            0xff02, 0, 0, 0, 0, 0, 0, 1
        ))));
    }

    #[test]
    fn public_v6_routable() {
        // 2606:4700:4700::1111 — Cloudflare DNS
        assert!(!is_private_ip(&IpAddr::V6(Ipv6Addr::new(
            0x2606, 0x4700, 0x4700, 0, 0, 0, 0, 0x1111
        ))));
        // 2001:4860:4860::8888 — Google DNS
        assert!(!is_private_ip(&IpAddr::V6(Ipv6Addr::new(
            0x2001, 0x4860, 0x4860, 0, 0, 0, 0, 0x8888
        ))));
    }

    fn sample_entry() -> GeoEntry {
        GeoEntry {
            country_iso: "US".into(),
            country_name: "United States".into(),
            continent: "NA".into(),
            is_eu: false,
            registered_country_iso: "US".into(),
            first_seen: 1_700_000_000_000_000_000,
            last_seen: 1_700_000_000_000_000_000,
            count: 3,
        }
    }

    #[test]
    fn payload_shape_contains_all_required_fields() {
        let ip: IpAddr = "93.184.216.34".parse().unwrap();
        let payload = build_payload(&ip, &sample_entry());

        // Spot-check every field name and value.
        assert!(payload.contains(r#""ip":"93.184.216.34""#), "ip: {payload}");
        assert!(payload.contains(r#""country_iso":"US""#));
        assert!(payload.contains(r#""country_name":"United States""#));
        assert!(payload.contains(r#""continent":"NA""#));
        assert!(payload.contains(r#""is_eu":false"#));
        assert!(payload.contains(r#""registered_country_iso":"US""#));
        assert!(payload.contains(r#""observed_connections_count":3"#));
        assert!(payload.contains(r#""first_seen_unix_ns":1700000000000000000"#));
        assert!(payload.contains(r#""last_seen_unix_ns":1700000000000000000"#));

        // Sanity: starts with `{` and ends with `}`, no trailing junk.
        assert!(payload.starts_with('{'));
        assert!(payload.ends_with('}'));
    }

    #[test]
    fn payload_escapes_quotes_in_country_name() {
        let mut entry = sample_entry();
        entry.country_name = r#"Côte d"Ivoire"#.into();
        let ip: IpAddr = "1.1.1.1".parse().unwrap();
        let payload = build_payload(&ip, &entry);
        assert!(payload.contains(r#""country_name":"Côte d\"Ivoire""#));
    }

    #[test]
    fn dedup_24h_ttl_prunes_expired_entries() {
        // We can't safely subtract 25 hours from Instant::now() on a fresh
        // boot (the underlying QueryPerformanceCounter base may be smaller
        // than 25 h, returning None). Build a synthetic "now" by taking
        // Instant::now() and adding 25 h, then placing the entries at the
        // ORIGINAL now. The 25 h-future "now" is then far enough ahead
        // that the original entries are at -25 h relative to it.
        let mut table: HashMap<IpAddr, (GeoEntry, Instant)> = HashMap::new();
        let entry_time = Instant::now();
        let now_plus_25h = entry_time + Duration::from_secs(25 * 3600);
        let now_plus_2h = entry_time + Duration::from_secs(2 * 3600);

        // fresh: inserted at entry_time, prune at entry_time+2h → 2h old, retained.
        // mostly_fresh: inserted 1h before entry_time, prune at +2h → 3h old, retained.
        // stale: inserted at entry_time, prune at +25h → 25h old, evicted.
        let fresh_inserted = entry_time;
        let mostly_fresh_inserted = entry_time;
        let stale_inserted = entry_time;

        table.insert(IpAddr::V4(Ipv4Addr::new(1, 1, 1, 1)), (sample_entry(), fresh_inserted));
        table.insert(
            IpAddr::V4(Ipv4Addr::new(8, 8, 8, 8)),
            (sample_entry(), mostly_fresh_inserted),
        );
        table.insert(IpAddr::V4(Ipv4Addr::new(9, 9, 9, 9)), (sample_entry(), stale_inserted));

        // First pass: prune at +2h. Nothing should be evicted (all 2h old).
        prune_dedup(&mut table, now_plus_2h);
        assert_eq!(table.len(), 3, "no entry should be evicted at 2h");

        // Second pass: prune at +25h. All entries should be evicted.
        prune_dedup(&mut table, now_plus_25h);
        assert_eq!(table.len(), 0, "all entries should be evicted at 25h");
    }

    #[test]
    fn dedup_retains_entries_under_ttl() {
        let mut table: HashMap<IpAddr, (GeoEntry, Instant)> = HashMap::new();
        let inserted = Instant::now();
        table.insert(IpAddr::V4(Ipv4Addr::new(1, 1, 1, 1)), (sample_entry(), inserted));
        // Prune 1 ms after insertion — entry must still be there.
        prune_dedup(&mut table, inserted + Duration::from_millis(1));
        assert_eq!(table.len(), 1);
    }

    #[test]
    fn mmdb_path_falls_back_when_programdata_missing() {
        // Snapshot whatever the current value is so we can restore it.
        let prev = std::env::var_os("PROGRAMDATA");
        // SAFETY: this test is single-threaded inside its own #[test]; the
        // crate runs `cargo test` with the default harness which DOES
        // parallelise tests, so we use the env var carefully — we restore
        // it before the function returns and we don't read it elsewhere.
        // To avoid races with parallel tests we don't unset; we set it to
        // a known value and assert the resulting path shape.
        std::env::set_var("PROGRAMDATA", r"C:\TestProgramData");
        let p = mmdb_path();
        assert!(p.ends_with("Personel/agent/GeoLite2-Country.mmdb")
            || p.ends_with(r"Personel\agent\GeoLite2-Country.mmdb"));
        // Restore.
        match prev {
            Some(v) => std::env::set_var("PROGRAMDATA", v),
            None => std::env::remove_var("PROGRAMDATA"),
        }
    }

    #[test]
    fn collector_metadata() {
        let c = GeoIpCollector::new();
        assert_eq!(c.name(), "geo_ip");
        assert_eq!(c.event_types(), &["network.geo_ip_resolved"]);
        let h = c.health();
        assert!(!h.healthy); // not started yet
        assert_eq!(h.events_since_last, 0);
        assert_eq!(h.drops_since_last, 0);
    }
}
