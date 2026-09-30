import { describe, expect, it } from "vitest";
import { buildTokenRequest } from "@/pages/security/TokensPanel";

describe("token request body", () => {
  it("sends the chosen applications only for a selected_apps token", () => {
    expect(
      buildTokenRequest({
        name: "ci",
        access: "selected_apps",
        appClientIds: ["wiki", "grafana"],
        expiresInDays: "0",
      }),
    ).toEqual({
      name: "ci",
      access: "selected_apps",
      appClientIds: ["wiki", "grafana"],
    });
  });

  it("drops the applications on every other level, even an empty list", () => {
    // The server refuses appClientIds on these levels, so a pick made before
    // the level changed must not travel.
    for (const access of ["all_apps", "full", "sudo"] as const) {
      const body = buildTokenRequest({
        name: "ci",
        access,
        appClientIds: ["wiki"],
        expiresInDays: "30",
      });
      expect(body).toEqual({ name: "ci", access, expiresInDays: 30 });
      expect("appClientIds" in body).toBe(false);
    }
  });

  it("omits a zero, non-integer or negative day count instead of sending it", () => {
    for (const expiresInDays of ["0", "-5", "1.5", "", "abc"]) {
      const body = buildTokenRequest({
        name: "ci",
        access: "full",
        appClientIds: [],
        expiresInDays,
      });
      expect("expiresInDays" in body).toBe(false);
    }
  });
});
