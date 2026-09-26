import { describe, expect, it } from "vitest";
import {
  pickTab,
  profileTab,
  profileTabs,
  settingsTab,
  settingsTabs,
} from "@/pages/console/tabs";

describe("console tab search", () => {
  it("keeps a value the page can render", () => {
    expect(profileTab("avatar")).toBe("avatar");
    expect(settingsTab("keys")).toBe("keys");
  });

  it("falls back to the first tab for a missing value", () => {
    expect(profileTab(undefined)).toBe(profileTabs[0]);
    expect(settingsTab(undefined)).toBe(settingsTabs[0]);
    expect(settingsTab("Keys")).toBe(settingsTabs[0]);
  });

  it("falls back rather than guessing at an unknown tab", () => {
    // A tab this build dropped, and a neighbouring page's tab, are both just
    // absent as far as this page is concerned.
    expect(settingsTab("sessions")).toBe(settingsTabs[0]);
    expect(profileTab("keys")).toBe(profileTabs[0]);
  });

  it("does not correct case or trim", () => {
    expect(settingsTab("Keys")).toBe(settingsTabs[0]);
    expect(settingsTab(" keys ")).toBe(settingsTabs[0]);
  });

  it("falls back when the parameter arrived as a list or a non-string", () => {
    // ?tab=a&tab=b parses to an array; ?tab[]=1 parses to an object.
    expect(settingsTab(["keys", "network"])).toBe(settingsTabs[0]);
    expect(settingsTab({})).toBe(settingsTabs[0]);
    expect(settingsTab(7)).toBe(settingsTabs[0]);
    expect(settingsTab(null)).toBe(settingsTabs[0]);
  });

  it("never returns a value outside the page's set", () => {
    for (const input of [undefined, "", "nope", 1, [], {}]) {
      expect(settingsTabs).toContain(pickTab(input, settingsTabs));
      expect(profileTabs).toContain(pickTab(input, profileTabs));
    }
  });
});
