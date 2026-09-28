import { describe, expect, it } from "vitest";
import { parseSearch, type SearchValue } from "@/app/search-params";
import {
  activeFilterCount,
  auditQuery,
  auditSearch,
  parseDay,
} from "@/pages/admin/audit-filters";

/** The filters a page reads from these search values. */
function read(search: Record<string, SearchValue>) {
  return auditSearch.parse(search);
}

const anchor = Date.parse("2026-09-25T12:00:00Z");

describe("audit log search", () => {
  it("defaults to the last day with no filters", () => {
    expect(read({})).toEqual({
      range: "24h",
      from: "",
      to: "",
      factor: "",
      event: "",
    });
  });

  it("keeps values the page offers and drops the rest without repairing them", () => {
    expect(
      read({
        range: "7d",
        factor: "password",
        event: "fail",
        account: "42",
      }),
    ).toEqual({
      range: "7d",
      from: "",
      to: "",
      factor: "password",
      event: "fail",
      account: "42",
    });
    const odd = read({
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

  it("keeps an account id as the text the address carried", () => {
    expect(read(parseSearch("?account=17")).account).toBe("17");
    for (const searchStr of [
      "?account=0",
      "?account=-1",
      "?account=1.5",
      "?account=017",
      "?account=abc",
      "?account=1e3",
      "?account=9007199254740993",
      "?account=3&account=4",
    ]) {
      expect(read(parseSearch(searchStr)).account).toBeUndefined();
    }
  });

  it("keeps a day in the address and drops one that is not a day", () => {
    const search = read(parseSearch("?range=custom&from=2026-01-01&to=01-02"));
    expect(search.from).toBe("2026-01-01");
    expect(search.to).toBe("");
  });

  it("counts the popover's filters for the badge on its button", () => {
    expect(activeFilterCount(read({}))).toBe(0);
    expect(
      activeFilterCount(
        read({ factor: "totp", event: "use", account: "3", range: "1h" }),
      ),
    ).toBe(3);
  });
});

describe("audit log query", () => {
  it("reaches back from the anchor for a preset and leaves the end open", () => {
    expect(auditQuery(read({ range: "1h" }), anchor)).toEqual({
      status: "ready",
      filters: { since: "2026-09-25T11:00:00.000Z" },
    });
    expect(auditQuery(read({ range: "30d" }), anchor)).toEqual({
      status: "ready",
      filters: { since: "2026-08-26T12:00:00.000Z" },
    });
  });

  it("asks the same question for the same anchor, so the cursor stays bound", () => {
    const search = read({ range: "24h", factor: "session" });
    expect(auditQuery(search, anchor)).toEqual(auditQuery(search, anchor));
  });

  it("sends no time bound for all time, and passes the other filters through", () => {
    expect(
      auditQuery(
        read({
          range: "all",
          factor: "webauthn",
          event: "use",
          account: "9",
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
      read({ range: "custom", from: "2026-09-01", to: "2026-09-03" }),
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
      auditQuery(read({ range: "custom", from: "2026-09-01" }), anchor),
    ).toEqual({ status: "incomplete", reason: "missing-dates" });
    expect(
      auditQuery(
        read({ range: "custom", from: "2026-02-30", to: "2026-03-02" }),
        anchor,
      ),
    ).toEqual({ status: "incomplete", reason: "missing-dates" });
    expect(
      auditQuery(
        read({ range: "custom", from: "2026-09-03", to: "2026-09-01" }),
        anchor,
      ),
    ).toEqual({ status: "incomplete", reason: "reversed-dates" });
  });

  it("ignores custom days while a preset is chosen", () => {
    expect(
      auditQuery(
        read({ range: "all", from: "2026-09-03", to: "2026-09-01" }),
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
