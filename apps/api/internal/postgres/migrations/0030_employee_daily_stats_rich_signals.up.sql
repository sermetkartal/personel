-- Migration 0030: employee_daily_stats.rich_signals — schemaless pass-through for all non-core collector signals.
--
-- The core columns (active_minutes, idle_minutes, top_apps, ...) capture the
-- foreground/idle/app-focus slice. `rich_signals` holds everything else the
-- 23-collector agent produces (browser, email, filesystem, network, usb,
-- bluetooth, mtp, system events, device status, print, clipboard, keystroke
-- metadata, live view, policy enforcement, tamper findings). Consumed as
-- json.RawMessage by the Go API and typed as `RichSignals` on the console.

ALTER TABLE employee_daily_stats
    ADD COLUMN IF NOT EXISTS rich_signals JSONB DEFAULT '{}'::jsonb;

COMMENT ON COLUMN employee_daily_stats.rich_signals IS
    'Schemaless blob of non-core collector signals (browser/email/filesystem/network/usb/bluetooth/mtp/system/device/print/clipboard/keystroke/liveview/policy/tamper). Shape defined on the console as RichSignals.';
