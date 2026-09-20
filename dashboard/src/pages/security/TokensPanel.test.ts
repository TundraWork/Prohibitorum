import { describe, expect, it } from "vitest";
import type { components } from "@/api/generated/schema";
import {
  buildTokenRequest,
  isOfferedScope,
} from "@/pages/security/TokensPanel";

type ForwardAuthApp = components["schemas"]["MyForwardAuthApp"];

const apps: ForwardAuthApp[] = [
  {
    clientId: "wiki",
    displayName: "Wiki",
    scopes: [{ name: "read", description: "Read pages" }, { name: "write" }],
  },
  { clientId: "bare", displayName: "Bare", scopes: [] },
];

describe("token request body", () => {
  it("drops the grants when every application is allowed", () => {
    // The server rejects a body that carries both, so the grants must not be
    // sent even though the user picked some before flipping the switch.
    const body = buildTokenRequest({
      name: "ci",
      allApps: true,
      grants: { wiki: ["read"] },
      expiresInDays: "30",
    });
    expect(body).toEqual({
      name: "ci",
      allApps: true,
      appGrants: {},
      expiresInDays: 30,
    });
  });

  it("sends the grants when the token is limited to chosen applications", () => {
    const body = buildTokenRequest({
      name: "ci",
      allApps: false,
      grants: { wiki: ["read", "write"] },
      expiresInDays: "0",
    });
    expect(body).toEqual({
      name: "ci",
      allApps: false,
      appGrants: { wiki: ["read", "write"] },
    });
    // Zero days means no expiry, which the server reads as the field's absence.
    expect("expiresInDays" in body).toBe(false);
  });

  it("omits a non-integer or negative day count instead of sending it", () => {
    for (const expiresInDays of ["0", "-5", "1.5", "", "abc"]) {
      const body = buildTokenRequest({
        name: "ci",
        allApps: true,
        grants: {},
        expiresInDays,
      });
      expect("expiresInDays" in body).toBe(false);
    }
  });
});

describe("scope vocabulary", () => {
  it("accepts only scopes the chosen application publishes", () => {
    expect(isOfferedScope(apps, "wiki", "read")).toBe(true);
    expect(isOfferedScope(apps, "wiki", "admin")).toBe(false);
    expect(isOfferedScope(apps, "bare", "read")).toBe(false);
    expect(isOfferedScope(apps, "unknown", "read")).toBe(false);
  });
});
