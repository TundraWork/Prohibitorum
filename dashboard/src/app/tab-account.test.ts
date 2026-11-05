import { describe, expect, it } from "vitest";
import {
  type BrowserAccount,
  browserAccountCountAtom,
  headerAccountId,
  isUnpinnedPath,
  pinTabAccount,
  resolveTabAccount,
  tabAccountAtom,
  withoutU,
  withU,
} from "@/app/tab-account";
import { store } from "@/i18n";

function account(accountId: number, u: string): BrowserAccount {
  return {
    accountId,
    u,
    username: `user${accountId}`,
    displayName: `User ${accountId}`,
  };
}

const lin = account(12, "3fa9");
const ops = account(40, "b071");
const qa = account(41, "c2d4");

describe("resolveTabAccount", () => {
  it("pins nothing and keeps the address while nothing is signed in", () => {
    expect(resolveTabAccount([], undefined)).toEqual({
      kind: "pin",
      account: null,
      u: undefined,
    });
  });

  it("pins the only account without putting u in the address", () => {
    expect(resolveTabAccount([lin], undefined)).toEqual({
      kind: "pin",
      account: lin,
      u: undefined,
    });
  });

  it("strips a u that names the only account", () => {
    expect(resolveTabAccount([lin], "3fa9")).toEqual({
      kind: "strip",
      account: lin,
    });
  });

  it("pins the default account and adds its u when there are several", () => {
    expect(resolveTabAccount([lin, ops, qa], undefined)).toEqual({
      kind: "pin",
      account: lin,
      u: "3fa9",
    });
  });

  it("keeps the account the tab is pinned to when the address lost its u", () => {
    expect(resolveTabAccount([lin, ops, qa], undefined, 41)).toEqual({
      kind: "pin",
      account: qa,
      u: "c2d4",
    });
  });

  it("falls back to the default when the pinned account has gone", () => {
    expect(resolveTabAccount([lin, ops], undefined, 41)).toEqual({
      kind: "pin",
      account: lin,
      u: "3fa9",
    });
  });

  it("pins the one account u matches", () => {
    expect(resolveTabAccount([lin, ops, qa], "b071")).toEqual({
      kind: "pin",
      account: ops,
      u: "b071",
    });
  });

  it("matches u by prefix either way round, as it grows from 4 to 8", () => {
    const long = account(50, "77aa01bc");
    expect(resolveTabAccount([lin, long], "77aa")).toMatchObject({
      kind: "pin",
      account: long,
    });
    expect(resolveTabAccount([lin, ops], "b071ffee")).toMatchObject({
      kind: "pin",
      account: ops,
      u: "b071ffee",
    });
  });

  it("asks to choose when a shortened u matches several accounts", () => {
    const one = account(60, "ab12cd34");
    const two = account(61, "ab12ef56");
    expect(resolveTabAccount([lin, one, two], "ab12")).toEqual({
      kind: "choose",
    });
  });

  it("asks to sign in when no account matches u", () => {
    expect(resolveTabAccount([lin, ops], "ffff")).toEqual({ kind: "login" });
    expect(resolveTabAccount([], "ffff")).toEqual({ kind: "login" });
  });
});

describe("the tab's account", () => {
  it("names itself on reads only while the browser has several", () => {
    pinTabAccount(lin, 1);
    expect(headerAccountId()).toBeUndefined();
    expect(headerAccountId("HEAD")).toBeUndefined();
    pinTabAccount(lin, 2);
    expect(headerAccountId()).toBe(12);
    pinTabAccount(null, 2);
    expect(headerAccountId()).toBeUndefined();
    expect(store.get(tabAccountAtom)).toBeNull();
    expect(store.get(browserAccountCountAtom)).toBe(2);
  });

  it("names itself on every write, whatever count it last saw", () => {
    // The tab saw one account; another tab may since have signed it out
    // and signed someone else in. A write must not act as that account.
    pinTabAccount(lin, 1);
    for (const method of ["POST", "PUT", "PATCH", "DELETE"]) {
      expect(headerAccountId(method)).toBe(12);
    }
    pinTabAccount(null, 1);
    expect(headerAccountId("POST")).toBeUndefined();
  });

  it("keeps the pages that are no one account's apart", () => {
    for (const path of [
      "/login",
      "/login/totp",
      "/login/recovery",
      "/choose-account",
      "/sign-out",
      "/error",
      "/pair",
      "/enroll/abc",
    ]) {
      expect(isUnpinnedPath(path)).toBe(true);
    }
    for (const path of ["/", "/profile", "/consent", "/admin/users/3"]) {
      expect(isUnpinnedPath(path)).toBe(false);
    }
  });

  it("adds and removes u without touching the rest of the address", () => {
    expect(withU("/admin/users?q=a#top", "3fa9")).toBe(
      "/admin/users?q=a&u=3fa9#top",
    );
    expect(withoutU("/admin/users?u=3fa9&q=a#top")).toBe(
      "/admin/users?q=a#top",
    );
  });
});
