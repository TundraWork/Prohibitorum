import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  clampCount,
  clampDelay,
  clampEnrollmentProviders,
  clampPairingExpiry,
  defaultMockConfig,
  getMockConfig,
  mockDelayMax,
  mockEnrollmentProvidersMax,
  mockListMax,
  mockPairingExpiryMax,
  mockPairingExpiryMin,
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

  it("clamps the response delay into the range the panel offers", () => {
    expect(clampDelay(-20)).toBe(0);
    expect(clampDelay(699.6)).toBe(700);
    expect(clampDelay(mockDelayMax + 1)).toBe(mockDelayMax);
    expect(clampDelay(Number.NaN)).toBe(0);
  });

  it("keeps a pairing's expiry within its bounds, defaulting to the server's five minutes", () => {
    expect(defaultMockConfig.pairing).toEqual({
      expiresInSeconds: 300,
      alreadyBound: false,
      sameNetwork: false,
    });
    expect(clampPairingExpiry(0)).toBe(mockPairingExpiryMin);
    expect(clampPairingExpiry(9.6)).toBe(10);
    expect(clampPairingExpiry(mockPairingExpiryMax + 1)).toBe(
      mockPairingExpiryMax,
    );
    expect(clampPairingExpiry(Number.NaN)).toBe(mockPairingExpiryMin);
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

  it("starts the public flows on a first-time consent with an app-defined scope", () => {
    expect(defaultMockConfig.publicFlows.consent).toEqual({
      grant: "first",
      scopes: "openid profile email groups offline_access wiki:write",
      logo: true,
      policy: true,
      terms: true,
      ticketValid: true,
      samlAttributes: 3,
    });
  });

  it("loads stored public-flow edits and keeps a default the stored value cannot replace", async () => {
    window.localStorage.setItem(
      "prohibitorum.devtools.mock",
      JSON.stringify({
        publicFlows: { consent: { grant: "incremental", logo: "no" } },
      }),
    );
    vi.resetModules();
    const reloaded = await import("@/devtools/mock/model");
    const consent = reloaded.getMockConfig().publicFlows.consent;
    expect(consent.grant).toBe("incremental");
    expect(consent.logo).toBe(true);
    expect(consent.ticketValid).toBe(true);
  });

  it("gives a panel saved before the enrollment and VRChat settings their defaults", async () => {
    window.localStorage.setItem(
      "prohibitorum.devtools.mock",
      JSON.stringify({ publicFlows: { consent: { grant: "incremental" } } }),
    );
    vi.resetModules();
    const reloaded = await import("@/devtools/mock/model");
    const flows = reloaded.getMockConfig().publicFlows;
    expect(flows.consent.grant).toBe("incremental");
    expect(flows.enrollment).toEqual(defaultMockConfig.publicFlows.enrollment);
    expect(flows.welcome).toEqual(defaultMockConfig.publicFlows.welcome);
    expect(flows.flow).toEqual(defaultMockConfig.publicFlows.flow);
  });

  it("caps an enrollment's providers at three", () => {
    expect(clampEnrollmentProviders(-1)).toBe(0);
    expect(clampEnrollmentProviders(2.5)).toBe(2);
    expect(clampEnrollmentProviders(9)).toBe(mockEnrollmentProvidersMax);
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
