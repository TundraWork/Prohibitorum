import { describe, expect, it } from "vitest";
import { maintenanceRedirect } from "@/app/maintenance-guard";

const member = { role: "user" };
const admin = { role: "admin" };

function redirects(
  pathname: string,
  search: Record<string, unknown> = {},
  session: { role: string } | null = member,
  maintenanceMode = true,
) {
  return maintenanceRedirect({ maintenanceMode, session, pathname, search });
}

describe("the maintenance guard", () => {
  it("sends members and anonymous visitors to the maintenance page", () => {
    for (const path of ["/", "/apps", "/login", "/consent", "/error"]) {
      expect(redirects(path)).toBe(true);
      expect(redirects(path, {}, null)).toBe(true);
    }
  });

  it("leaves an administrator alone", () => {
    expect(redirects("/", {}, admin)).toBe(false);
    expect(redirects("/admin/settings", {}, admin)).toBe(false);
  });

  it("never redirects the maintenance page itself", () => {
    expect(redirects("/maintenance", {}, null)).toBe(false);
  });

  it("lets the administrators' sign-in through whatever `admin` holds", () => {
    for (const [path, search] of [
      ["/login", { admin: "" }],
      ["/login", { admin: 1 }],
      ["/login", { admin: 0 }],
      ["/login/totp", { admin: true }],
      ["/login/recovery", { admin: 1, return_to: "/" }],
    ] as const) {
      expect(redirects(path, search, null)).toBe(false);
    }
  });

  it("does not let a sign-in without `admin`, or `admin` on another page, through", () => {
    expect(redirects("/login", { return_to: "/" }, null)).toBe(true);
    expect(redirects("/apps", { admin: true })).toBe(true);
  });

  it("does nothing while maintenance is off", () => {
    expect(redirects("/", {}, null, false)).toBe(false);
  });
});
