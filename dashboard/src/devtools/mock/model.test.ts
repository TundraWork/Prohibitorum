import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  clampCount,
  defaultMockConfig,
  getMockConfig,
  mockListMax,
  resetMockConfig,
  subscribeMockConfig,
  updateMockConfig,
} from "@/devtools/mock/model";

beforeEach(() => {
  window.localStorage.clear();
  resetMockConfig();
});

afterEach(() => {
  vi.resetModules();
});

describe("mock config", () => {
  it("clamps a list length into the range the panel offers", () => {
    expect(clampCount(-3)).toBe(0);
    expect(clampCount(2.7)).toBe(2);
    expect(clampCount(mockListMax + 1)).toBe(mockListMax);
    expect(clampCount(Number.NaN)).toBe(0);
  });

  it("publishes a changed config to its subscribers and restores the defaults on reset", () => {
    const seen: string[] = [];
    const unsubscribe = subscribeMockConfig(() => {
      seen.push(getMockConfig().session.displayName);
    });
    updateMockConfig((draft) => {
      draft.session.displayName = "Ada";
      draft.factors.passkeys = 5;
    });
    expect(getMockConfig().session.displayName).toBe("Ada");
    resetMockConfig();
    unsubscribe();

    expect(seen).toEqual(["Ada", defaultMockConfig.session.displayName]);
    expect(getMockConfig().factors).toEqual(defaultMockConfig.factors);
  });

  it("keeps the data but not the master switch across a reload", async () => {
    updateMockConfig((draft) => {
      draft.enabled = true;
      draft.writes = true;
      draft.factors.passkeys = 5;
    });
    expect(getMockConfig().enabled).toBe(true);

    vi.resetModules();
    // The module reads storage once, when it first evaluates; only a fresh
    // module instance can stand in for a reload.
    const reloaded = await import("@/devtools/mock/model");

    // A console left serving fabricated data would hide the server it is being
    // developed against.
    expect(reloaded.getMockConfig().enabled).toBe(false);
    expect(reloaded.getMockConfig().writes).toBe(true);
    expect(reloaded.getMockConfig().factors.passkeys).toBe(5);
  });
});
