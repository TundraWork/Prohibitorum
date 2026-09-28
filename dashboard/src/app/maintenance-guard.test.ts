import { describe, expect, it } from "vitest";
import { maintenanceRedirect } from "@/app/maintenance-guard";
import { parseSearch } from "@/app/search-params";

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
    for (const [path, searchStr] of [
      ["/login", "?admin"],
      ["/login", "?admin="],
      ["/login", "?admin=1"],
      ["/login", "?admin=0"],
      ["/login", "?admin=1&admin=2"],
      ["/login/totp", "?admin=true"],
      ["/login/recovery", "?admin=1&return_to=%2F"],
    ] as const) {
      expect(redirects(path, parseSearch(searchStr), null)).toBe(false);
    }
  });

  it("does not let a sign-in without `admin`, or `admin` on another page, through", () => {
    expect(redirects("/login", parseSearch("?return_to=%2F"), null)).toBe(true);
    expect(redirects("/apps", parseSearch("?admin=1"))).toBe(true);
  });

  it("does nothing while maintenance is off", () => {
    expect(redirects("/", {}, null, false)).toBe(false);
  });
});
