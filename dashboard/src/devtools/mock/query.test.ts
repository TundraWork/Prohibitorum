import { beforeEach, describe, expect, it } from "vitest";
import {
  defaultMockConfig,
  getMockConfig,
  mockAdminListMax,
  mockDelayMax,
  mockListMax,
  resetMockConfig,
  subscribeMockConfig,
} from "@/devtools/mock/model";
import { applyMockQuery } from "@/devtools/mock/query";

beforeEach(() => {
  window.localStorage.clear();
  resetMockConfig();
});

describe("mock URL control", () => {
  it("turns the mock on for the bare flag", () => {
    applyMockQuery("?mock");
    expect(getMockConfig().enabled).toBe(true);
  });

  it("turns the mock on for any mock.* parameter, so a walkthrough need not say both", () => {
    applyMockQuery("?mock.admin.oidcApps=7");
    const config = getMockConfig();
    expect(config.enabled).toBe(true);
    expect(config.admin.oidcApps).toBe(7);
  });

  it("ignores a URL that names nothing the mock knows", () => {
    applyMockQuery("?tab=profile&page=2");
    expect(getMockConfig().enabled).toBe(false);
  });

  it("leaves the config alone for a parameter that is not a real field", () => {
    const before = structuredClone(getMockConfig());
    const applied = applyMockQuery("?mock.admin.nonsense=3&mock.notAThing=1");
    expect(applied).toEqual([]);
    const after = getMockConfig();
    expect(after.admin.oidcApps).toBe(before.admin.oidcApps);
    expect(after.admin.accounts).toBe(before.admin.accounts);
  });

  it("reads booleans the way a URL can spell them", () => {
    applyMockQuery("?mock.session.signedIn=false&mock.writes=1");
    const config = getMockConfig();
    expect(config.session.signedIn).toBe(false);
    expect(config.writes).toBe(true);
  });

  it("refuses a boolean it cannot read rather than guessing", () => {
    const applied = applyMockQuery("?mock.writes=maybe");
    expect(applied).toEqual([]);
    expect(getMockConfig().writes).toBe(false);
  });

  it("clamps counts to the ceilings the panel uses", () => {
    applyMockQuery(
      "?mock.admin.identityProviders=9999&mock.lists.sessions=9999",
    );
    const config = getMockConfig();
    expect(config.admin.identityProviders).toBe(mockAdminListMax);
    expect(config.lists.sessions).toBe(mockListMax);
  });

  it("clamps the response delay", () => {
    applyMockQuery(`?mock.delayMs=${mockDelayMax * 10}`);
    expect(getMockConfig().delayMs).toBe(mockDelayMax);
  });

  it("accepts an enum value and refuses one outside the set", () => {
    applyMockQuery("?mock.session.role=member");
    expect(getMockConfig().session.role).toBe("member");

    applyMockQuery("?mock.session.role=superuser");
    expect(getMockConfig().session.role).toBe("member");
  });

  it("sets a nested count without disturbing its siblings", () => {
    applyMockQuery("?mock.session.managedApps.oidc=3");
    const config = getMockConfig();
    expect(config.session.managedApps.oidc).toBe(3);
    // Untouched, so the sibling keeps its default.
    expect(config.session.managedApps.saml).toBe(
      defaultMockConfig.session.managedApps.saml,
    );
  });

  it("reports the paths it applied, so a URL can be checked against what took", () => {
    const applied = applyMockQuery("?mock.admin.oidcApps=4&mock.bogus=1");
    expect(applied).toEqual(["admin.oidcApps"]);
  });

  it("publishes nothing when the same URL is applied again", () => {
    // The router calls this on every resolve, and a publish refreshes the
    // queries and invalidates the router — so a repeat that published would
    // re-trigger itself forever and leave every list on its spinner.
    applyMockQuery("?mock.admin.oidcApps=4");
    let notifications = 0;
    const unsubscribe = subscribeMockConfig(() => {
      notifications += 1;
    });
    applyMockQuery("?mock.admin.oidcApps=4");
    applyMockQuery("?mock.admin.oidcApps=4&mock.delayMs=0");
    expect(notifications).toBe(1);
    unsubscribe();
  });
});
