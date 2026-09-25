import type { AuditEventFilters } from "@/api/queries";
import {
  type AuditEvent,
  type AuditFactor,
  isAuditEvent,
  isAuditFactor,
} from "@/pages/admin/audit-vocabulary";

/**
 * The audit log's filters, which live in the URL and nowhere else, validated
 * the way the account list's are (`user-filters.ts`): a value outside what the
 * page offers falls back to the default rather than being guessed at — no
 * trimming, no case folding — and a cursor never enters the URL.
 */

export const auditRanges = ["1h", "24h", "7d", "30d", "all", "custom"] as const;
export type AuditRange = (typeof auditRanges)[number];

/** How far back each preset reaches from the moment it was chosen. */
const presetMs: Record<Exclude<AuditRange, "all" | "custom">, number> = {
  "1h": 60 * 60 * 1000,
  "24h": 24 * 60 * 60 * 1000,
  "7d": 7 * 24 * 60 * 60 * 1000,
  "30d": 30 * 24 * 60 * 60 * 1000,
};

export interface AuditSearch {
  range: AuditRange;
  /** `YYYY-MM-DD`, read only for `range=custom`. */
  from: string;
  to: string;
  factor: AuditFactor | "";
  event: AuditEvent | "";
  /** An account id, or `undefined` for every account. */
  account?: number;
}

const datePattern = /^\d{4}-\d{2}-\d{2}$/;

function text(value: unknown): string {
  return typeof value === "string" ? value : "";
}

/**
 * The router parses `?account=5` into a number and `?account=abc` into a
 * string, so both are accepted — as long as they name a positive integer.
 */
function accountId(value: unknown): number | undefined {
  if (typeof value === "number") {
    return Number.isSafeInteger(value) && value > 0 ? value : undefined;
  }
  if (typeof value === "string" && /^[1-9]\d*$/.test(value)) {
    const parsed = Number(value);
    return Number.isSafeInteger(parsed) ? parsed : undefined;
  }
  return undefined;
}

export function auditSearch(search: Record<string, unknown>): AuditSearch {
  const range = text(search.range);
  const factor = text(search.factor);
  const event = text(search.event);
  const from = text(search.from);
  const to = text(search.to);
  const account = accountId(search.account);
  return {
    range: (auditRanges as readonly string[]).includes(range)
      ? (range as AuditRange)
      : "24h",
    from: datePattern.test(from) ? from : "",
    to: datePattern.test(to) ? to : "",
    factor: isAuditFactor(factor) ? factor : "",
    event: isAuditEvent(event) ? event : "",
    ...(account === undefined ? {} : { account }),
  };
}

/**
 * A `YYYY-MM-DD` as a local calendar day, or `null` for a day that does not
 * exist (`2026-02-30`), which `Date` would otherwise roll into March.
 */
export function parseDay(value: string): Date | null {
  if (!datePattern.test(value)) return null;
  const [year, month, day] = value.split("-").map(Number) as [
    number,
    number,
    number,
  ];
  const date = new Date(year, month - 1, day);
  return date.getFullYear() === year &&
    date.getMonth() === month - 1 &&
    date.getDate() === day
    ? date
    : null;
}

export type AuditQuery =
  | { status: "ready"; filters: AuditEventFilters }
  | { status: "incomplete"; reason: "missing-dates" | "reversed-dates" };

/**
 * The filters as the server takes them, or why they cannot be sent yet.
 *
 * A preset reaches back from `anchor`, the moment the reader chose the filters
 * or asked for a refresh, rather than from whenever a page is fetched: the
 * server binds its cursor to the filters, so every page of one answer has to
 * carry the same `since`. A custom range runs from the start of its first day
 * to the last millisecond of its last, in the reader's time zone.
 */
export function auditQuery(search: AuditSearch, anchor: number): AuditQuery {
  const filters: AuditEventFilters = {
    ...(search.factor === "" ? {} : { factor: search.factor }),
    ...(search.event === "" ? {} : { event: search.event }),
    ...(search.account === undefined ? {} : { accountId: search.account }),
  };
  if (search.range === "all") return { status: "ready", filters };
  if (search.range === "custom") {
    const from = parseDay(search.from);
    const to = parseDay(search.to);
    if (from === null || to === null) {
      return { status: "incomplete", reason: "missing-dates" };
    }
    if (from > to) return { status: "incomplete", reason: "reversed-dates" };
    const end = new Date(to);
    end.setHours(23, 59, 59, 999);
    return {
      status: "ready",
      filters: {
        ...filters,
        since: from.toISOString(),
        until: end.toISOString(),
      },
    };
  }
  return {
    status: "ready",
    filters: {
      ...filters,
      since: new Date(anchor - presetMs[search.range]).toISOString(),
    },
  };
}

/** How many of the popover's filters are set, for the count on its button. */
export function activeFilterCount(search: AuditSearch): number {
  return [
    search.factor !== "",
    search.event !== "",
    search.account !== undefined,
  ].filter(Boolean).length;
}
