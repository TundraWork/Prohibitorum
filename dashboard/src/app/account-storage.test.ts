import { describe, expect, it } from "vitest";
import {
  clearKnownAccounts,
  forgetKnownAccount,
  knownUsername,
  readKnownAccounts,
  readLastUsed,
  rememberKnownAccount,
  writeLastUsed,
} from "@/app/account-storage";

const knownKey = "prohibitorum.accounts.known";

describe("the known accounts", () => {
  it("keeps each account's username under its u", () => {
    rememberKnownAccount("3fa9", "lin");
    rememberKnownAccount("b071", "ops-oncall");
    expect(JSON.parse(window.localStorage.getItem(knownKey) ?? "")).toEqual({
      "3fa9": "lin",
      b071: "ops-oncall",
    });
    expect(knownUsername("b071")).toBe("ops-oncall");
  });

  it("replaces the record of a u that grew longer", () => {
    rememberKnownAccount("3fa9", "lin");
    rememberKnownAccount("3fa9c2d1", "lin");
    expect(readKnownAccounts()).toEqual({ "3fa9c2d1": "lin" });
    expect(knownUsername("3fa9")).toBe("lin");
  });

  it("names no one when a shortened u matches two usernames", () => {
    rememberKnownAccount("ab12cd34", "one");
    rememberKnownAccount("ab12ef56", "two");
    expect(knownUsername("ab12")).toBeUndefined();
    expect(knownUsername("ab12ef56")).toBe("two");
  });

  it("forgets one account, then all of them", () => {
    rememberKnownAccount("3fa9", "lin");
    rememberKnownAccount("b071", "ops-oncall");
    forgetKnownAccount("3fa9");
    expect(readKnownAccounts()).toEqual({ b071: "ops-oncall" });
    clearKnownAccounts();
    expect(window.localStorage.getItem(knownKey)).toBeNull();
    expect(knownUsername("b071")).toBeUndefined();
  });

  it("reads a damaged record as empty", () => {
    window.localStorage.setItem(knownKey, "{not json");
    expect(readKnownAccounts()).toEqual({});
    window.localStorage.setItem(knownKey, JSON.stringify({ a: 1, b: "bob" }));
    expect(readKnownAccounts()).toEqual({ b: "bob" });
  });
});

describe("the last-used account per application", () => {
  it("keeps one u per application key", () => {
    writeLastUsed("oidc:wiki", "3fa9");
    writeLastUsed("saml:https://expenses.example", "b071");
    expect(
      window.localStorage.getItem("prohibitorum.accounts.last-used.oidc:wiki"),
    ).toBe("3fa9");
    expect(readLastUsed("oidc:wiki")).toBe("3fa9");
    expect(readLastUsed("saml:https://expenses.example")).toBe("b071");
    expect(readLastUsed("oidc:other")).toBeUndefined();
  });
});
