import { describe, expect, it } from "vitest";
import {
  activeFilterCount,
  auditQuery,
  auditSearch,
  parseDay,
} from "@/pages/admin/audit-filters";

const anchor = Date.parse("2026-09-25T12:00:00Z");

describe("audit log search", () => {
  it("defaults to the last day with no filters", () => {
    expect(auditSearch({})).toEqual({
      range: "24h",
      from: "",
      to: "",
      factor: "",
      event: "",
    });
  });

  it("keeps values the page offers and drops the rest without repairing them", () => {
    expect(
      auditSearch({
        range: "7d",
        factor: "password",
        event: "fail",
        account: 42,
      }),
    ).toEqual({
      range: "7d",
      from: "",
      to: "",
      factor: "password",
      event: "fail",
      account: 42,
    });
    const odd = auditSearch({
      range: "7D",
      factor: " password",
      event: "FAIL",
      account: "0",
      from: "2026-9-1",
      to: ["2026-09-02"],
    });
    expect(odd).toEqual({
      range: "24h",
      from: "",
      to: "",
      factor: "",
      event: "",
    });
  });

  it("reads an account id whether the router parsed it as a number or a string", () => {
    expect(auditSearch({ account: "17" }).account).toBe(17);
    expect(auditSearch({ account: 17 }).account).toBe(17);
    for (const account of [-1, 1.5, "017", "abc", Number.MAX_VALUE]) {
      expect(auditSearch({ account }).account).toBeUndefined();
    }
  });

  it("counts the popover's filters for the badge on its button", () => {
    expect(activeFilterCount(auditSearch({}))).toBe(0);
    expect(
      activeFilterCount(
        auditSearch({ factor: "totp", event: "use", account: 3, range: "1h" }),
      ),
    ).toBe(3);
  });
});

describe("audit log query", () => {
  it("reaches back from the anchor for a preset and leaves the end open", () => {
    expect(auditQuery(auditSearch({ range: "1h" }), anchor)).toEqual({
      status: "ready",
      filters: { since: "2026-09-25T11:00:00.000Z" },
    });
    expect(auditQuery(auditSearch({ range: "30d" }), anchor)).toEqual({
      status: "ready",
      filters: { since: "2026-08-26T12:00:00.000Z" },
    });
  });

  it("asks the same question for the same anchor, so the cursor stays bound", () => {
    const search = auditSearch({ range: "24h", factor: "session" });
    expect(auditQuery(search, anchor)).toEqual(auditQuery(search, anchor));
  });

  it("sends no time bound for all time, and passes the other filters through", () => {
    expect(
      auditQuery(
        auditSearch({
          range: "all",
          factor: "webauthn",
          event: "use",
          account: 9,
        }),
        anchor,
      ),
    ).toEqual({
      status: "ready",
      filters: { factor: "webauthn", event: "use", accountId: 9 },
    });
  });

  it("covers whole local days for a custom range", () => {
    const query = auditQuery(
      auditSearch({ range: "custom", from: "2026-09-01", to: "2026-09-03" }),
      anchor,
    );
    expect(query).toEqual({
      status: "ready",
      filters: {
        since: new Date(2026, 8, 1, 0, 0, 0, 0).toISOString(),
        until: new Date(2026, 8, 3, 23, 59, 59, 999).toISOString(),
      },
    });
  });

  it("holds back a custom range that is missing a day, names a day that does not exist, or runs backwards", () => {
    expect(
      auditQuery(auditSearch({ range: "custom", from: "2026-09-01" }), anchor),
    ).toEqual({ status: "incomplete", reason: "missing-dates" });
    expect(
      auditQuery(
        auditSearch({ range: "custom", from: "2026-02-30", to: "2026-03-02" }),
        anchor,
      ),
    ).toEqual({ status: "incomplete", reason: "missing-dates" });
    expect(
      auditQuery(
        auditSearch({ range: "custom", from: "2026-09-03", to: "2026-09-01" }),
        anchor,
      ),
    ).toEqual({ status: "incomplete", reason: "reversed-dates" });
  });

  it("ignores custom days while a preset is chosen", () => {
    expect(
      auditQuery(
        auditSearch({ range: "all", from: "2026-09-03", to: "2026-09-01" }),
        anchor,
      ),
    ).toEqual({ status: "ready", filters: {} });
  });
});

describe("calendar days", () => {
  it("parses a real day and refuses one Date would roll over", () => {
    expect(parseDay("2024-02-29")?.getDate()).toBe(29);
    expect(parseDay("2026-02-29")).toBeNull();
    expect(parseDay("2026-13-01")).toBeNull();
    expect(parseDay("20260901")).toBeNull();
  });
});
