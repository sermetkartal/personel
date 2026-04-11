//! Agent-side event envelope and event kind enumeration.
//!
//! This module mirrors the event taxonomy from `docs/architecture/event-taxonomy.md`
//! and maps directly to the `events.proto` `Event` message. The Rust enum
//! variant names use `CamelCase` translations of the dotted proto names.
//!
//! Events are serialised to protobuf bytes for queue storage and transport.
//! The [`EventEnvelope`] struct holds the [`EventMeta`] (IDs, timestamps,
//! PII class, etc.) alongside the event-specific payload encoded as raw proto
//! bytes. This avoids a large in-memory enum during transport and keeps
//! queue enqueue/dequeue O(1) regardless of payload size.

use serde::{Deserialize, Serialize};

use crate::ids::{EndpointId, EventId, TenantId};

// ──────────────────────────────────────────────────────────────────────────────
// Priority
// ──────────────────────────────────────────────────────────────────────────────

/// Queue priority as defined in the local SQLite schema.
///
/// Lower numbers are higher priority (never evicted first).
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
#[repr(u8)]
pub enum Priority {
    /// Critical (tamper events) — never evicted.
    Critical = 0,
    /// High priority (live-view events, health heartbeats).
    High = 1,
    /// Normal operational events.
    Normal = 2,
    /// Low priority (verbose file read events, DNS queries).
    Low = 3,
}

impl Priority {
    /// Returns the integer representation stored in SQLite.
    #[must_use]
    pub fn as_i32(self) -> i32 {
        self as i32
    }

    /// Converts an integer priority back to the enum.
    #[must_use]
    pub fn from_i32(v: i32) -> Self {
        match v {
            0 => Self::Critical,
            1 => Self::High,
            3 => Self::Low,
            _ => Self::Normal,
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// PII classification (mirrors proto enum)
// ──────────────────────────────────────────────────────────────────────────────

/// PII classification per KVKK taxonomy. Mirrors `personel.v1.PiiClass`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[repr(i32)]
pub enum PiiClass {
    /// No personal data.
    None = 1,
    /// Technical identifier linkable to a person.
    Identifier = 2,
    /// Personal behavioural data.
    Behavioral = 3,
    /// Content of communication or work product.
    Content = 4,
    /// High-sensitivity data (keystrokes, screen pixels).
    Sensitive = 5,
}

// ──────────────────────────────────────────────────────────────────────────────
// EventKind — every event type from the taxonomy
// ──────────────────────────────────────────────────────────────────────────────

/// Every event type in the taxonomy.
///
/// This enum is used as a routing key for the collector registry and queue
/// eviction decisions. It is NOT the full proto payload — payloads are
/// stored as raw `bytes` in [`EventEnvelope::payload_pb`].
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[non_exhaustive]
pub enum EventKind {
    // Process
    /// `process.start`
    ProcessStart,
    /// `process.stop`
    ProcessStop,
    /// `process.foreground_change`
    ProcessForegroundChange,
    // Window
    /// `window.title_changed`
    WindowTitleChanged,
    /// `window.focus_lost`  (metadata only; no dedicated proto payload)
    WindowFocusLost,
    // Session
    /// `session.idle_start`
    SessionIdleStart,
    /// `session.idle_end`
    SessionIdleEnd,
    /// `session.lock`
    SessionLock,
    /// `session.unlock`
    SessionUnlock,
    // Screen
    /// `screenshot.captured`
    ScreenshotCaptured,
    /// `screenclip.captured`
    ScreenclipCaptured,
    // File
    /// `file.created`
    FileCreated,
    /// `file.read`
    FileRead,
    /// `file.written`
    FileWritten,
    /// `file.deleted`
    FileDeleted,
    /// `file.renamed`
    FileRenamed,
    /// `file.copied`
    FileCopied,
    // Clipboard
    /// `clipboard.metadata`
    ClipboardMetadata,
    /// `clipboard.content_encrypted`
    ClipboardContentEncrypted,
    // Print
    /// `print.job_submitted`
    PrintJobSubmitted,
    // USB
    /// `usb.device_attached`
    UsbDeviceAttached,
    /// `usb.device_removed`
    UsbDeviceRemoved,
    /// `usb.mass_storage_policy_block`
    UsbMassStoragePolicyBlock,
    // Network
    /// `network.flow_summary`
    NetworkFlowSummary,
    /// `network.dns_query`
    NetworkDnsQuery,
    /// `network.tls_sni`
    NetworkTlsSni,
    // Keystroke
    /// `keystroke.window_stats`
    KeystrokeWindowStats,
    /// `keystroke.content_encrypted`
    KeystrokeContentEncrypted,
    // Policy enforcement
    /// `app.blocked_by_policy`
    AppBlockedByPolicy,
    /// `web.blocked_by_policy`
    WebBlockedByPolicy,
    // Agent health
    /// `agent.health_heartbeat`
    AgentHealthHeartbeat,
    /// `agent.policy_applied`
    AgentPolicyApplied,
    /// `agent.update_installed`
    AgentUpdateInstalled,
    /// `agent.tamper_detected`
    AgentTamperDetected,
    // Live view audit
    /// `live_view.started`
    LiveViewStarted,
    /// `live_view.stopped`
    LiveViewStopped,
}

impl EventKind {
    /// Returns the canonical dotted event type name used in proto `EventMeta.event_type`.
    #[must_use]
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::ProcessStart => "process.start",
            Self::ProcessStop => "process.stop",
            Self::ProcessForegroundChange => "process.foreground_change",
            Self::WindowTitleChanged => "window.title_changed",
            Self::WindowFocusLost => "window.focus_lost",
            Self::SessionIdleStart => "session.idle_start",
            Self::SessionIdleEnd => "session.idle_end",
            Self::SessionLock => "session.lock",
            Self::SessionUnlock => "session.unlock",
            Self::ScreenshotCaptured => "screenshot.captured",
            Self::ScreenclipCaptured => "screenclip.captured",
            Self::FileCreated => "file.created",
            Self::FileRead => "file.read",
            Self::FileWritten => "file.written",
            Self::FileDeleted => "file.deleted",
            Self::FileRenamed => "file.renamed",
            Self::FileCopied => "file.copied",
            Self::ClipboardMetadata => "clipboard.metadata",
            Self::ClipboardContentEncrypted => "clipboard.content_encrypted",
            Self::PrintJobSubmitted => "print.job_submitted",
            Self::UsbDeviceAttached => "usb.device_attached",
            Self::UsbDeviceRemoved => "usb.device_removed",
            Self::UsbMassStoragePolicyBlock => "usb.mass_storage_policy_block",
            Self::NetworkFlowSummary => "network.flow_summary",
            Self::NetworkDnsQuery => "network.dns_query",
            Self::NetworkTlsSni => "network.tls_sni",
            Self::KeystrokeWindowStats => "keystroke.window_stats",
            Self::KeystrokeContentEncrypted => "keystroke.content_encrypted",
            Self::AppBlockedByPolicy => "app.blocked_by_policy",
            Self::WebBlockedByPolicy => "web.blocked_by_policy",
            Self::AgentHealthHeartbeat => "agent.health_heartbeat",
            Self::AgentPolicyApplied => "agent.policy_applied",
            Self::AgentUpdateInstalled => "agent.update_installed",
            Self::AgentTamperDetected => "agent.tamper_detected",
            Self::LiveViewStarted => "live_view.started",
            Self::LiveViewStopped => "live_view.stopped",
        }
    }

    /// Returns the default queue priority for this event kind.
    #[must_use]
    pub fn default_priority(&self) -> Priority {
        match self {
            Self::AgentTamperDetected => Priority::Critical,
            Self::AgentHealthHeartbeat
            | Self::LiveViewStarted
            | Self::LiveViewStopped
            | Self::KeystrokeContentEncrypted => Priority::High,
            Self::FileRead | Self::NetworkDnsQuery | Self::NetworkFlowSummary => Priority::Low,
            _ => Priority::Normal,
        }
    }

    /// Returns the PII classification for this event kind.
    #[must_use]
    pub fn pii_class(&self) -> PiiClass {
        match self {
            Self::AgentHealthHeartbeat | Self::AgentPolicyApplied | Self::AgentUpdateInstalled => {
                PiiClass::None
            }
            Self::SessionLock | Self::SessionUnlock | Self::UsbDeviceAttached
            | Self::UsbDeviceRemoved | Self::UsbMassStoragePolicyBlock
            | Self::AgentTamperDetected | Self::LiveViewStarted | Self::LiveViewStopped => {
                PiiClass::Identifier
            }
            Self::ProcessStart
            | Self::ProcessStop
            | Self::ProcessForegroundChange
            | Self::WindowFocusLost
            | Self::SessionIdleStart
            | Self::SessionIdleEnd
            | Self::ClipboardMetadata
            | Self::KeystrokeWindowStats
            | Self::AppBlockedByPolicy => PiiClass::Behavioral,
            Self::WindowTitleChanged
            | Self::FileCreated
            | Self::FileRead
            | Self::FileWritten
            | Self::FileDeleted
            | Self::FileRenamed
            | Self::FileCopied
            | Self::NetworkFlowSummary
            | Self::NetworkDnsQuery
            | Self::NetworkTlsSni
            | Self::PrintJobSubmitted
            | Self::WebBlockedByPolicy => PiiClass::Content,
            Self::ScreenshotCaptured
            | Self::ScreenclipCaptured
            | Self::ClipboardContentEncrypted
            | Self::KeystrokeContentEncrypted => PiiClass::Sensitive,
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// EventEnvelope
// ──────────────────────────────────────────────────────────────────────────────

/// An in-memory or queue-resident event envelope.
///
/// The `payload_pb` field contains a prost-encoded `personel.v1.Event`
/// proto message. Keeping payloads as raw bytes avoids boxing large enum
/// variants and makes queue batch assembly zero-copy.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EventEnvelope {
    /// Unique event identifier (UUIDv7).
    pub event_id: EventId,
    /// Event kind used for routing and eviction.
    pub kind: EventKind,
    /// Queue priority.
    pub priority: Priority,
    /// Wall-clock time when the event occurred (nanos since epoch).
    pub occurred_at_nanos: i64,
    /// Wall-clock time when the event was enqueued (nanos since epoch).
    pub enqueued_at_nanos: i64,
    /// Tenant owning this endpoint.
    pub tenant_id: TenantId,
    /// Endpoint that generated the event.
    pub endpoint_id: EndpointId,
    /// Prost-encoded `personel.v1.Event` bytes.
    pub payload_pb: bytes::Bytes,
}

impl EventEnvelope {
    /// Constructs an envelope. The caller provides the pre-encoded proto bytes.
    #[must_use]
    pub fn new(
        kind: EventKind,
        tenant_id: TenantId,
        endpoint_id: EndpointId,
        occurred_at_nanos: i64,
        enqueued_at_nanos: i64,
        payload_pb: bytes::Bytes,
    ) -> Self {
        Self {
            event_id: EventId::new_v7(),
            priority: kind.default_priority(),
            kind,
            occurred_at_nanos,
            enqueued_at_nanos,
            tenant_id,
            endpoint_id,
            payload_pb,
        }
    }

    /// Returns the byte length of the serialised payload.
    #[must_use]
    pub fn size_bytes(&self) -> usize {
        self.payload_pb.len()
    }
}
