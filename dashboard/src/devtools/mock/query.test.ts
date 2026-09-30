import { beforeEach, describe, expect, it } from "vitest";
import {
  defaultMockConfig,
  getMockConfig,
  mockAdminListMax,
  mockDelayMax,
  mockListMax,
  mockPairingExpiryMax,
  mockPairingExpiryMin,
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

  it("sets up a public flow from the address, refusing a grant the panel does not offer", () => {
    applyMockQuery(
      "?mock.publicFlows.consent.grant=incremental&mock.publicFlows.consent.samlAttributes=0",
    );
    expect(getMockConfig().publicFlows.consent.grant).toBe("incremental");
    expect(getMockConfig().publicFlows.consent.samlAttributes).toBe(0);
    applyMockQuery("?mock.publicFlows.consent.grant=everything");
    expect(getMockConfig().publicFlows.consent.grant).toBe("incremental");
  });

  it("sets up the enrollment, welcome and VRChat pages, within the panel's choices", () => {
    applyMockQuery(
      "?mock.publicFlows.enrollment.intent=reset&mock.publicFlows.enrollment.providers=9" +
        "&mock.publicFlows.welcome.avatarPending=never&mock.publicFlows.flow.step=proof" +
        "&mock.publicFlows.flow.intent=link",
    );
    const flows = getMockConfig().publicFlows;
    expect(flows.enrollment.intent).toBe("reset");
    expect(flows.enrollment.providers).toBe(3);
    expect(flows.welcome.avatarPending).toBe("never");
    expect(flows.flow).toMatchObject({ step: "proof", intent: "link" });
    applyMockQuery(
      "?mock.publicFlows.enrollment.intent=signup&mock.publicFlows.flow.step=done",
    );
    expect(getMockConfig().publicFlows.enrollment.intent).toBe("reset");
    expect(getMockConfig().publicFlows.flow.step).toBe("proof");
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

  it("sets the pairing a lookup finds, clamping its expiry", () => {
    applyMockQuery(
      "?mock.pairing.expiresInSeconds=10&mock.pairing.alreadyBound=true&mock.pairing.sameNetwork=1",
    );
    expect(getMockConfig().pairing).toEqual({
      expiresInSeconds: 10,
      alreadyBound: true,
      sameNetwork: true,
    });

    applyMockQuery("?mock.pairing.expiresInSeconds=1");
    expect(getMockConfig().pairing.expiresInSeconds).toBe(mockPairingExpiryMin);
    applyMockQuery("?mock.pairing.expiresInSeconds=99999");
    expect(getMockConfig().pairing.expiresInSeconds).toBe(mockPairingExpiryMax);
  });

  it("sets how the new device's pairing goes, clamping its expiry and reads", () => {
    applyMockQuery(
      "?mock.publicFlows.pairing.approveAfterPolls=0&mock.publicFlows.pairing.expiresInSeconds=30",
    );
    expect(getMockConfig().publicFlows.pairing).toEqual({
      approveAfterPolls: 0,
      expiresInSeconds: 30,
    });
    applyMockQuery("?mock.publicFlows.pairing.expiresInSeconds=1");
    expect(getMockConfig().publicFlows.pairing.expiresInSeconds).toBe(
      mockPairingExpiryMin,
    );
    applyMockQuery("?mock.publicFlows.pairing.approveAfterPolls=999");
    expect(getMockConfig().publicFlows.pairing.approveAfterPolls).toBe(
      mockListMax,
    );
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

  it("lays out the sign-in page from the address, refusing a theme the settings do not offer", () => {
    applyMockQuery(
      "?mock.instance.loginAppearance.cardPosition=left&mock.instance.loginAppearance.theme=dark",
    );
    const appearance = getMockConfig().instance.loginAppearance;
    expect(appearance.cardPosition).toBe("left");
    expect(appearance.theme).toBe("dark");

    applyMockQuery("?mock.instance.loginAppearance.theme=system");
    expect(getMockConfig().instance.loginAppearance.theme).toBe("dark");
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
