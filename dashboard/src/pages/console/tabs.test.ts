import { describe, expect, it } from "vitest";
import {
  pickTab,
  profileTab,
  profileTabs,
  securityTab,
  securityTabs,
  settingsTab,
  settingsTabs,
} from "@/pages/console/tabs";

describe("console tab search", () => {
  it("keeps a value the page can render", () => {
    expect(profileTab("avatar")).toBe("avatar");
    expect(securityTab("tokens")).toBe("tokens");
    expect(settingsTab("keys")).toBe("keys");
  });

  it("falls back to the first tab for a missing value", () => {
    expect(profileTab(undefined)).toBe(profileTabs[0]);
    expect(securityTab(undefined)).toBe(securityTabs[0]);
    expect(settingsTab(undefined)).toBe(settingsTabs[0]);
    expect(settingsTab("Keys")).toBe(settingsTabs[0]);
  });

  it("falls back rather than guessing at an unknown tab", () => {
    // A tab this build dropped, and a neighbouring page's tab, are both just
    // absent as far as this page is concerned.
    expect(profileTab("sessions")).toBe(profileTabs[0]);
    expect(securityTab("avatar")).toBe(securityTabs[0]);
  });

  it("does not correct case or trim", () => {
    expect(securityTab("Tokens")).toBe(securityTabs[0]);
    expect(securityTab(" tokens ")).toBe(securityTabs[0]);
  });

  it("falls back when the parameter arrived as a list or a non-string", () => {
    // ?tab=a&tab=b parses to an array; ?tab[]=1 parses to an object.
    expect(securityTab(["tokens", "sessions"])).toBe(securityTabs[0]);
    expect(securityTab({})).toBe(securityTabs[0]);
    expect(securityTab(7)).toBe(securityTabs[0]);
    expect(securityTab(null)).toBe(securityTabs[0]);
  });

  it("never returns a value outside the page's set", () => {
    for (const input of [undefined, "", "nope", 1, [], {}]) {
      expect(securityTabs).toContain(pickTab(input, securityTabs));
      expect(profileTabs).toContain(pickTab(input, profileTabs));
    }
  });
});
