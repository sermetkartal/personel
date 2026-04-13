# Event Schema Registry

**Roadmap item #79 — Faz 7**
**Scope**: authoritative inventory of every wire-format event the Personel agent emits, including required fields, proto message name, and schema version history.
**Source of truth**: `proto/personel/v1/events.proto` + `proto/personel/v1/common.proto`
**Compile-time guard**: `apps/gateway/cmd/schema-check/main.go` (reads `.proto` files and fails build if a required field is removed without bumping `schema_version`).

---

## 1. Versioning contract

Each event carries an `EventMeta.schema_version` (uint8) on the wire. The contract:

1. **Additive changes** (new optional field, new payload message, new enum value) do NOT bump the version. Consumers must tolerate unknown tags.
2. **Removing or renaming a field**, or changing its semantic meaning, **requires** bumping the table's schema_version and updating the entry below.
3. **Required-field removal is forbidden at the same schema_version**. The schema-check tool enforces this.
4. Gateway/enricher MUST respect the field even at old versions — we never retroactively break past pilot customers.
5. Long-term deprecation path: mark the field `deprecated` in `events.proto`, wait one major release, then remove.

---

## 2. Core event catalogue (Faz 1 taxonomy, schema_version = 1)

| # | EventKind | Proto message | Required fields | Sensitivity | Notes |
|---|---|---|---|---|---|
| 1 | process_start | `ProcessStart` | pid, image_path | low | command_line_hash, image_sha256 optional |
| 2 | process_stop | `ProcessStop` | pid, exit_code | low | |
| 3 | process_foreground_change | `ProcessForegroundChange` | pid_new, exe_new | low | |
| 4 | window_title_changed | `WindowTitleChanged` | pid, hwnd, title, exe_name | **redacted** | title is run through SensitivityGuard regex |
| 5 | session_idle_start | `SessionIdleStart` | idle_threshold_sec | none | |
| 6 | session_idle_end | `SessionIdleEnd` | idle_duration_ms | none | |
| 7 | screenshot_captured | `ScreenshotCaptured` | blob_ref, width, height | **high** | PII requires blur; see ADR 0008 |
| 8 | file_created | `FileCreated` | path, pid | low | |
| 9 | file_written | `FileWritten` | path, pid, bytes_delta | low | sha256 optional |
| 10 | file_deleted | `FileDeleted` | path, pid | low | |
| 11 | file_renamed | `FileRenamed` | path_from, path_to, pid | low | |
| 12 | file_read | `FileRead` | path, process_pid | low | Phase 1 revision |
| 13 | file_copied | `FileCopied` | source_path, destination_path, pid | low | Phase 1 revision |
| 14 | clipboard_metadata | `ClipboardMetadata` | source_pid, source_exe, content_kind | medium | metadata only, no content unless DLP enabled (ADR 0013) |
| 15 | usb_device_attached | `UsbDeviceAttached` | vid, pid, device_class | low | serial hashed |
| 16 | usb_device_removed | `UsbDeviceRemoved` | vid, pid | low | |
| 17 | network_flow_summary | `NetworkFlowSummary` | pid, dest_ip, dest_port, protocol | medium | dest hostname only |
| 18 | keystroke_window_stats | `KeystrokeWindowStats` | hwnd, exe_name, keystroke_count | low | counts, never content |
| 19 | keystroke_content_encrypted | `KeystrokeContentEncrypted` | ciphertext_ref, nonce, aad, key_version | **restricted** | DLP-gated. ADR 0013 default OFF. Enricher MUST NOT log ciphertext_ref beyond audit. |
| 20 | app_blocked_by_policy | `AppBlockedByPolicy` | exe_name, rule_id | low | |
| 21 | web_blocked_by_policy | `WebBlockedByPolicy` | host, rule_id | low | |
| 22 | agent_health_heartbeat | `AgentHealthHeartbeat` | cpu_percent, rss_bytes, queue_depth | none | |
| 23 | agent_tamper_detected | `AgentTamperDetected` | check_name, severity | **critical** | emits even if sensitive policies active |
| 24 | live_view_started | `LiveViewStarted` | session_id, requested_by, approved_by, reason_code | high | HR dual-control invariant |
| 25 | live_view_stopped | `LiveViewStopped` | session_id, end_reason | medium | |
| 26 | print_job_submitted | `PrintJobSubmitted` | printer_name, document_name, page_count | medium | |
| 27 | session_lock | `SessionLock` | user_sid, lock_reason | low | |
| 28 | session_unlock | `SessionUnlock` | user_sid, locked_duration_ms | low | |
| 29 | window_focus_lost | `WindowFocusLost` | hwnd_prev, pid_prev, exe_prev, focused_duration_ms | low | back-reference to prior WindowTitleChanged |

## 3. Phase 2 event catalogue (extends schema_version = 1 additively)

The Faz 2 collectors (roadmap items #7–#20) emit these new kinds via the same `Event.payload` oneof, but they are tagged with additional proto numbers > 100 per ADR 0007's "reserved numeric ranges". As of 2026-04-13 the proto messages have NOT yet been added to `events.proto` — Wave 5 will add them. The registry pre-reserves the kinds so downstream consumers (ClickHouse, OpenSearch, ML classifier) can allocate columns / indices.

| # | EventKind | Agent collector | Required fields | Notes |
|---|---|---|---|---|
| 30 | browser_history_visited | `browser_history.rs` | url, title, visit_ts, browser_family | Chromium-family profiles. Sensitivity = title. |
| 31 | browser_firefox_history_visited | `firefox_history.rs` | url, title, visit_ts | Places.sqlite reader. |
| 32 | browser_url_extracted | `window_url_extraction.rs` | url, title_src | heuristic from foreground window title |
| 33 | cloud_storage_sync_event | `cloud_storage.rs` | provider, local_path, op | ReadDirectoryChangesW fan-in |
| 34 | email_metadata_observed | `email_metadata.rs` | provider, mbox_path, size_delta | PST/OST delta. Phase 2 MAPI COM TODO |
| 35 | office_recent_file_opened | `office_activity.rs` | app, path, accessed_at | HKCU MRU |
| 36 | system_power_state_changed | `system_events.rs` | new_state, old_state | suspend/resume/low-battery |
| 37 | system_login | `system_events.rs` | user_sid, session_id, source | WTS session notification |
| 38 | system_logout | `system_events.rs` | user_sid, session_id | |
| 39 | system_av_deactivated | `system_events.rs` | product_name, timestamp | WMI AntiVirusProduct |
| 40 | bluetooth_device_paired | `bluetooth_devices.rs` | address, name, cod_class | |
| 41 | bluetooth_device_unpaired | `bluetooth_devices.rs` | address | |
| 42 | mtp_device_attached | `mtp_devices.rs` | vendor, product, serial_hash | WPD COM Phase 2 TODO |
| 43 | mtp_device_removed | `mtp_devices.rs` | vendor, product, serial_hash | |
| 44 | device_status_snapshot | `device_status.rs` | cpu, ram, disk, battery, uptime, locked | 60s snapshot |
| 45 | network_geo_ip_resolved | `geo_ip.rs` | dest_ip, country, asn | maxminddb mmdb file not shipped per license |

---

## 4. Schema version history

| Version | Date | Change | Compatibility |
|---|---|---|---|
| 1 | 2026-03-20 | Initial Faz 1 taxonomy (events 1–29) | — |
| 1 (additive) | 2026-04-13 | Faz 2 events 30–45 reserved (protos pending Wave 5) | additive |

No breaking changes have occurred. The schema-check tool baseline
(`proto/personel/v1/SCHEMA_BASELINE.json`) is pinned to version 1.

---

## 5. Schema-check CI enforcement

The script `apps/gateway/cmd/schema-check/main.go`:

1. Parses `proto/personel/v1/events.proto` and `common.proto` with a minimal hand-rolled tokenizer (no new deps).
2. Extracts every `message X { … }` with its field list, marking fields with comments/annotations as required.
3. Compares the extracted snapshot against `proto/personel/v1/SCHEMA_BASELINE.json`.
4. Exits 1 if any required field disappeared and `schema_version` was not bumped.

Run locally:

```bash
go run ./apps/gateway/cmd/schema-check -baseline ./proto/personel/v1/SCHEMA_BASELINE.json
```

Typical wiring (Faz 16 item #168 — wire to CI):

```yaml
# .github/workflows/schema-check.yml
# TODO Faz 16: wire to CI
- name: Schema registry check
  run: go run ./apps/gateway/cmd/schema-check
```

---

## 6. Deprecation policy

- A field marked `deprecated` in `events.proto` must stay on the wire for at least **two** customer releases after the deprecation flag is added.
- A field may be removed only after the following are true simultaneously:
  1. `schema_version` has been bumped.
  2. ClickHouse column has been `ALTER TABLE DROP COLUMN`-ed in a migration.
  3. Admin API no longer exposes the field in any endpoint response.
  4. The schema-check baseline has been regenerated.

---

## 7. Out-of-scope

- **Audit log schema** — governed by `apps/api/internal/audit/` hash chain, not this registry.
- **Policy bundle schema** — `proto/personel/v1/policy.proto`, separate versioning via signed bundle epoch.
- **LiveView control channel** — `proto/personel/v1/live_view.proto`, separate stateful protocol.
