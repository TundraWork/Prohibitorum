import { describe, expect, it } from "vitest";
import {
  hostProblem,
  scopeListProblem,
  scopeNameProblem,
} from "@/pages/admin/forward-auth-apps/forward-auth-validation";
import { scopeSummary } from "@/pages/admin/forward-auth-apps/scope-summary";

/**
 * These rules are the only check any of these values gets.
 *
 * The server stores the hostname as given and trims a scope name before
 * validating it, so a mistake here is a deployment that protects nothing or a
 * vocabulary that reads back differently from what was typed. The cases below
 * are the ones where "nearly right" is the failure mode: a scheme left on a
 * hostname, a scope name pasted with a space, two rows that differ only by case.
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

describe("scope vocabulary", () => {
  it("accepts the names the server accepts", () => {
    expect(scopeNameProblem("read")).toBeUndefined();
    expect(scopeNameProblem("admin:users")).toBeUndefined();
    expect(scopeNameProblem("a.b_c-d")).toBeUndefined();
  });

  it("refuses a name with whitespace at either end rather than trimming it", () => {
    // The server would trim and store "read", so accepting this would save
    // something other than what the reader typed.
    expect(scopeNameProblem(" read")).toBeDefined();
    expect(scopeNameProblem("read ")).toBeDefined();
    expect(scopeNameProblem(" ")).toBeDefined();
  });

  it("refuses a name that is too long or wrongly shaped", () => {
    expect(scopeNameProblem("a".repeat(64))).toBeUndefined();
    expect(scopeNameProblem("a".repeat(65))).toBeDefined();
    expect(scopeNameProblem(":read")).toBeDefined();
    expect(scopeNameProblem("read:")).toBeDefined();
    expect(scopeNameProblem("")).toBeDefined();
  });

  it("refuses a duplicate, which the server would too", () => {
    expect(
      scopeListProblem([{ name: "read" }, { name: "write" }, { name: "read" }]),
    ).toBeDefined();
    // Distinct names that merely look alike are fine.
    expect(
      scopeListProblem([{ name: "read" }, { name: "Read" }]),
    ).toBeUndefined();
  });

  it("refuses a description past the limit", () => {
    expect(
      scopeListProblem([{ name: "read", description: "a".repeat(256) }]),
    ).toBeUndefined();
    expect(
      scopeListProblem([{ name: "read", description: "a".repeat(257) }]),
    ).toBeDefined();
  });

  it("accepts an empty vocabulary, which the server also allows", () => {
    expect(scopeListProblem([])).toBeUndefined();
  });
});

describe("scope list cell", () => {
  it("names the scopes while there are few, and counts the rest", () => {
    expect(scopeSummary([])).toBe("—");
    expect(scopeSummary(null)).toBe("—");
    expect(scopeSummary([{ name: "read" }])).toBe("read");
    expect(scopeSummary([{ name: "read" }, { name: "write" }])).toBe(
      "read, write",
    );
    expect(
      scopeSummary([{ name: "read" }, { name: "write" }, { name: "admin" }]),
    ).toBe("read, write, admin");
    expect(
      scopeSummary([
        { name: "read" },
        { name: "write" },
        { name: "admin" },
        { name: "billing" },
        { name: "export" },
      ]),
    ).toBe("read, write, admin +2");
  });
});
