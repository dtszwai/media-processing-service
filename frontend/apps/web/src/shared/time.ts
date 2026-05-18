import type { Timestamp } from "@bufbuild/protobuf/wkt";

// Convert a protobuf Timestamp to JS Date or epoch ms. Both fields on a
// Timestamp are bigint; preserving precision below ms isn't useful for the
// UI, so we collapse to Number.
export function tsToDate(ts: Timestamp | undefined): Date | undefined {
  if (!ts) return undefined;
  const seconds = Number(ts.seconds);
  const nanos = ts.nanos;
  return new Date(seconds * 1000 + Math.floor(nanos / 1_000_000));
}

export function tsToMs(ts: Timestamp | undefined): number | undefined {
  const d = tsToDate(ts);
  return d ? d.getTime() : undefined;
}

export function fmtTimeMs(ts: Timestamp | undefined): string {
  const d = tsToDate(ts);
  if (!d) return "—";
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  const ss = String(d.getSeconds()).padStart(2, "0");
  const ms = String(d.getMilliseconds()).padStart(3, "0");
  return `${hh}:${mm}:${ss}.${ms}`;
}

export function fmtDateTime(ts: Timestamp | undefined): string {
  const d = tsToDate(ts);
  if (!d) return "—";
  const yyyy = d.getFullYear();
  const mo = String(d.getMonth() + 1).padStart(2, "0");
  const dd = String(d.getDate()).padStart(2, "0");
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  const ss = String(d.getSeconds()).padStart(2, "0");
  return `${yyyy}-${mo}-${dd}\u00a0${hh}:${mm}:${ss}`;
}

export function fmtDuration(ms: number | bigint | undefined): string {
  if (ms === undefined || ms === null) return "—";
  const n = typeof ms === "bigint" ? Number(ms) : ms;
  if (!Number.isFinite(n) || n < 0) return "—";
  if (n < 1) return "0ms";
  if (n < 1000) return `${Math.round(n)}ms`;
  if (n < 60_000) return `${(n / 1000).toFixed(2)}s`;
  const mins = Math.floor(n / 60_000);
  const secs = Math.floor((n % 60_000) / 1000);
  return `${mins}m${secs.toString().padStart(2, "0")}s`;
}

// Absolute clock display for "last refreshed" stamps on auto-polling
// panels. fmtAge re-renders every second when paired with a 1s tick driver,
// which causes the header to alternate between "just now" and "1s ago" on a
// 2s refresh cycle. fmtClock only changes when the underlying timestamp
// updates, so the operator sees a stable wall-clock value instead.
export function fmtClock(ms: number): string {
  const d = new Date(ms);
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  const ss = String(d.getSeconds()).padStart(2, "0");
  return `${hh}:${mm}:${ss}`;
}

export function fmtAge(ms: number, now = Date.now()): string {
  const delta = now - ms;
  if (delta < 0) return "in the future";
  if (delta < 1000) return "just now";
  if (delta < 60_000) return `${Math.floor(delta / 1000)}s ago`;
  if (delta < 3_600_000) return `${Math.floor(delta / 60_000)}m ago`;
  if (delta < 86_400_000) return `${Math.floor(delta / 3_600_000)}h ago`;
  return `${Math.floor(delta / 86_400_000)}d ago`;
}

export function fmtBytes(v: number | bigint | string): string {
  const n = typeof v === "number" ? v : typeof v === "bigint" ? Number(v) : parseInt(v, 10);
  if (!Number.isFinite(n) || n < 0) return typeof v === "string" ? v : "—";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

export function idempotencyKey(prefix = "op"): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return `${prefix}-${crypto.randomUUID()}`;
  }
  return `${prefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}
