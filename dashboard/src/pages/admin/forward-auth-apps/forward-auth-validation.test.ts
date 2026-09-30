import { describe, expect, it } from "vitest";
import { hostProblem } from "@/pages/admin/forward-auth-apps/forward-auth-validation";

/**
 * These rules are the only check the hostname gets.
 *
 * The server stores it as given, so a mistake here is a deployment that
 * protects nothing. The cases below are the ones where "nearly right" is the
 * failure mode: a scheme or a port left on the hostname.
 */
describe("hostname", () => {
  it("accepts a hostname Traefik can match", () => {
    expect(hostProblem("app.example.com")).toBeUndefined();
    expect(hostProblem("app.acme.io")).toBeUndefined();
    expect(hostProblem("a-b.c-d.example.com")).toBeUndefined();
  });

  it("refuses a scheme or a port, which would match nothing", () => {
    expect(hostProblem("https://app.example.com")).toBeDefined();
    expect(hostProblem("app.example.com:443")).toBeDefined();
    // A bare label is not a host either: the cookie is scoped to a domain.
    expect(hostProblem("app")).toBeDefined();
  });

  it("refuses an uppercase or hyphen-edged label", () => {
    expect(hostProblem("App.example.com")).toBeDefined();
    expect(hostProblem("-app.example.com")).toBeDefined();
    expect(hostProblem("app-.example.com")).toBeDefined();
  });

  it("refuses a name past the DNS limit", () => {
    const long = `${"a".repeat(60)}.`.repeat(5);
    expect(hostProblem(long)).toBeDefined();
    expect(hostProblem("")).toBeDefined();
  });
});
