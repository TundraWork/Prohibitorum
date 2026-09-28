import { z } from "zod";
import type { AuditEventFilters } from "@/api/queries";
import {
  optionalSearchText,
  searchChoice,
  searchText,
} from "@/app/search-params";
import {
  auditEventKeys,
  auditFactorKeys,
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

const datePattern = /^\d{4}-\d{2}-\d{2}$/;

export const auditSearch = z.object({
  range: searchChoice(auditRanges, "24h"),
  /** `YYYY-MM-DD`, read only for `range=custom`. */
  from: searchText(z.string().regex(datePattern)),
  to: searchText(z.string().regex(datePattern)),
  factor: searchChoice(["", ...auditFactorKeys], ""),
  event: searchChoice(["", ...auditEventKeys], ""),
  /**
   * An account id, or `undefined` for every account. It stays the text the
   * address carried; the request converts it.
   */
  account: optionalSearchText(
    z
      .string()
      .regex(/^[1-9]\d*$/)
      .refine((value) => Number.isSafeInteger(Number(value))),
  ),
});

export type AuditSearch = z.output<typeof auditSearch>;

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
    ...(search.account === undefined
      ? {}
      : { accountId: Number(search.account) }),
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
