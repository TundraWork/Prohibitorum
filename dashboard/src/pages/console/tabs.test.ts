import { describe, expect, it } from "vitest";
import { parseSearch } from "@/app/search-params";
import { accountTabs, settingsTabs, tabSearch } from "@/pages/console/tabs";

/** The tab a page with `tabs` shows at `?searchStr`. */
function tabAt<const T extends readonly [string, ...string[]]>(
  tabs: T,
  searchStr: string,
) {
  return tabSearch(tabs).parse(parseSearch(searchStr)).tab;
}

describe("console tab search", () => {
  it("keeps a value the page can render", () => {
    expect(tabAt(accountTabs, "?tab=access")).toBe("access");
    expect(tabAt(settingsTabs, "?tab=keys")).toBe("keys");
  });

  it("falls back to the first tab for a missing value", () => {
    expect(tabAt(accountTabs, "")).toBe(accountTabs[0]);
    expect(tabAt(settingsTabs, "?tab=")).toBe(settingsTabs[0]);
  });

  it("falls back rather than guessing at an unknown tab", () => {
    // A tab this build dropped, and a neighbouring page's tab, are both just
    // absent as far as this page is concerned.
    expect(tabAt(settingsTabs, "?tab=sessions")).toBe(settingsTabs[0]);
    expect(tabAt(accountTabs, "?tab=keys")).toBe(accountTabs[0]);
  });

  it("does not correct case or trim", () => {
    expect(tabAt(settingsTabs, "?tab=Keys")).toBe(settingsTabs[0]);
    expect(tabAt(settingsTabs, "?tab=+keys+")).toBe(settingsTabs[0]);
  });

  it("falls back when the parameter is repeated", () => {
    expect(tabAt(accountTabs, "?tab=access&tab=danger")).toBe(accountTabs[0]);
    expect(tabAt(accountTabs, "?tab=access&tab=access")).toBe(accountTabs[0]);
  });
});
