"use client";

/**
 * useAuditStream — live audit-log feed via WebSocket (backend item #66).
 *
 * ## Auth strategy (important)
 *
 * Browsers forbid setting custom headers (including `Authorization`) on the
 * native `WebSocket` constructor. Two workarounds are possible:
 *
 *   1. **Subprotocol trick** — pass `["bearer", token]` as the second
 *      argument to `new WebSocket(url, protocols)`. The browser sends it in
 *      the `Sec-WebSocket-Protocol` handshake header, and the server parses
 *      the token out of that header. This requires explicit backend support
 *      (upstream must echo one of the offered protocols in its 101 response,
 *      otherwise Chrome closes the socket).
 *
 *   2. **Query-string token** — append `?access_token=...` to the URL. Works
 *      everywhere but leaks into proxy logs and browser history if not
 *      carefully handled.
 *
 * We try the subprotocol route first (#1). If the server doesn't support it
 * and the socket closes immediately, we fall back to polling /v1/audit
 * every 3 seconds so the UI degrades gracefully. The reconnect backoff also
 * eventually stops retrying if the server keeps rejecting us (max 30s
 * between tries, infinite retries but with jitter).
 *
 * TODO(gateway): teach the /v1/audit/stream handler to read the bearer token
 * from the `Sec-WebSocket-Protocol` header (`bearer, <token>` tuple) and
 * echo `bearer` back in the 101 response. Until then this hook runs in
 * polling-fallback mode in production.
 */

import { useEffect, useRef, useState } from "react";
import { listAuditRecords } from "@/lib/api/audit";
import type { AuditRecord } from "@/lib/api/types";

export interface StreamFilter {
  /** Prefix match — e.g. ["live_view", "dsr"] shows everything starting with those strings. */
  actions?: string[];
  actor_id?: string;
}

export interface UseAuditStreamOptions {
  /** OAuth access token for the WebSocket subprotocol handshake. */
  token?: string;
  /** Maximum entries to retain in the rolling buffer. Default 200. */
  bufferSize?: number;
  /** Disable the stream entirely (e.g. insufficient RBAC). */
  disabled?: boolean;
}

export type StreamConnectionState =
  | "connecting"
  | "connected"
  | "reconnecting"
  | "disconnected"
  | "polling-fallback";

export interface AuditStreamResult {
  entries: AuditRecord[];
  state: StreamConnectionState;
  connected: boolean;
  error: string | null;
}

// Sensitive keys we strip defensively — the server should already have
// removed these but we double-check on the client per ADR 0013 + KVKK m.6.
const SENSITIVE_PAYLOAD_KEYS = new Set([
  "content",
  "keystroke_content",
  "body",
  "raw_keystrokes",
  "password",
  "secret",
  "token",
  "api_key",
]);

function stripSensitive(payload: unknown): unknown {
  if (payload === null || typeof payload !== "object") return payload;
  if (Array.isArray(payload)) return payload.map(stripSensitive);
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(payload as Record<string, unknown>)) {
    if (SENSITIVE_PAYLOAD_KEYS.has(k.toLowerCase())) {
      out[k] = "[redacted]";
      continue;
    }
    out[k] = stripSensitive(v);
  }
  return out;
}

function matchesFilter(record: AuditRecord, filter: StreamFilter): boolean {
  if (filter.actions && filter.actions.length > 0) {
    const hit = filter.actions.some((prefix) => {
      // support trailing "*" prefix match and plain prefix match
      const clean = prefix.replace(/\*$/, "");
      return record.type.startsWith(clean);
    });
    if (!hit) return false;
  }
  if (filter.actor_id && record.actor_id !== filter.actor_id) return false;
  return true;
}

function buildWsUrl(filter: StreamFilter): string {
  const httpBase =
    process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080";
  const wsBase = httpBase.replace(/^http/, "ws");
  const qs = new URLSearchParams();
  if (filter.actions?.length) {
    qs.set("actions", filter.actions.join(","));
  }
  if (filter.actor_id) qs.set("actor_id", filter.actor_id);
  const suffix = qs.toString() ? `?${qs.toString()}` : "";
  return `${wsBase}/v1/audit/stream${suffix}`;
}

export function useAuditStream(
  filter: StreamFilter,
  options: UseAuditStreamOptions = {},
): AuditStreamResult {
  const { token, bufferSize = 200, disabled = false } = options;
  const [entries, setEntries] = useState<AuditRecord[]>([]);
  const [state, setState] = useState<StreamConnectionState>("disconnected");
  const [error, setError] = useState<string | null>(null);

  // Refs that must survive across reconnect attempts without re-running effect
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pollingTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const attemptRef = useRef(0);
  const cancelledRef = useRef(false);
  // Serialize filter so effect only re-runs on meaningful change
  const filterKey = JSON.stringify(filter);

  useEffect(() => {
    if (disabled) {
      setState("disconnected");
      return;
    }

    cancelledRef.current = false;
    attemptRef.current = 0;

    function append(record: AuditRecord): void {
      if (!matchesFilter(record, filter)) return;
      const sanitized: AuditRecord = {
        ...record,
        payload_json: (stripSensitive(record.payload_json) as Record<
          string,
          unknown
        >) ?? {},
      };
      setEntries((prev) => {
        // dedup by id
        if (prev.some((p) => p.id === sanitized.id)) return prev;
        const next = [sanitized, ...prev];
        return next.length > bufferSize ? next.slice(0, bufferSize) : next;
      });
    }

    function schedulePollingFallback(): void {
      if (pollingTimerRef.current) return;
      setState("polling-fallback");
      const poll = async (): Promise<void> => {
        try {
          const res = await listAuditRecords(
            { page: 1, page_size: 50, action: filter.actions?.[0] },
            token ? { token } : {},
          );
          res.items.forEach((r) => append(r));
          setError(null);
        } catch (e) {
          setError(e instanceof Error ? e.message : "polling failed");
        }
      };
      void poll();
      pollingTimerRef.current = setInterval(() => {
        void poll();
      }, 3000);
    }

    function clearPolling(): void {
      if (pollingTimerRef.current) {
        clearInterval(pollingTimerRef.current);
        pollingTimerRef.current = null;
      }
    }

    function scheduleReconnect(): void {
      if (cancelledRef.current) return;
      const attempt = attemptRef.current++;
      // exponential backoff 1s → 30s, jittered
      const base = Math.min(30_000, 1000 * Math.pow(2, Math.min(attempt, 5)));
      const jitter = Math.floor(Math.random() * 500);
      const delay = base + jitter;
      setState("reconnecting");
      reconnectTimerRef.current = setTimeout(() => {
        if (!cancelledRef.current) connect();
      }, delay);

      // After 3 failed attempts, start polling-fallback in parallel so the
      // UI isn't frozen while we keep retrying the WebSocket in the
      // background.
      if (attempt >= 3 && !pollingTimerRef.current) {
        schedulePollingFallback();
      }
    }

    function connect(): void {
      if (cancelledRef.current) return;
      setState("connecting");
      setError(null);

      let ws: WebSocket;
      try {
        const url = buildWsUrl(filter);
        // Subprotocol auth: browser's WebSocket API disallows headers, so we
        // encode the bearer token into the Sec-WebSocket-Protocol handshake
        // header as a two-token tuple ["bearer", "<token>"]. Requires the
        // gateway to echo back one of the offered protocols (e.g. "bearer").
        ws = token
          ? new WebSocket(url, ["bearer", token])
          : new WebSocket(url);
      } catch (e) {
        setError(e instanceof Error ? e.message : "ws-open-failed");
        scheduleReconnect();
        return;
      }

      wsRef.current = ws;

      ws.onopen = () => {
        attemptRef.current = 0;
        clearPolling();
        setState("connected");
        setError(null);
      };

      ws.onmessage = (ev: MessageEvent<string>) => {
        try {
          const payload = JSON.parse(ev.data) as AuditRecord | { type: string };
          if ("id" in payload && "created_at" in payload) {
            append(payload as AuditRecord);
          }
          // Ignore keepalive frames (e.g. {"type":"ping"})
        } catch {
          // ignore non-JSON frames
        }
      };

      ws.onerror = () => {
        setError("websocket-error");
      };

      ws.onclose = () => {
        wsRef.current = null;
        if (cancelledRef.current) return;
        scheduleReconnect();
      };
    }

    connect();

    return () => {
      cancelledRef.current = true;
      if (reconnectTimerRef.current) {
        clearTimeout(reconnectTimerRef.current);
        reconnectTimerRef.current = null;
      }
      clearPolling();
      if (wsRef.current) {
        try {
          wsRef.current.close();
        } catch {
          // ignore
        }
        wsRef.current = null;
      }
      setState("disconnected");
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filterKey, token, disabled, bufferSize]);

  return {
    entries,
    state,
    connected: state === "connected",
    error,
  };
}
