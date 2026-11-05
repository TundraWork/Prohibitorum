import { atom } from "jotai";
import type { components } from "@/api/generated/schema";
import { sameRef } from "@/app/account-storage";
import { store } from "@/i18n";

export type BrowserAccount = components["schemas"]["BrowserAccount"];

/** The request and response header that names the account a request acts as. */
export const accountHeader = "X-Prohibitorum-Account";

/** The account this tab acts as. */
export interface TabAccount {
  accountId: number;
  u: string;
  username: string;
}

/**
 * The account this tab is pinned to, for the tab's whole life. A tab never
 * moves to another account by itself: another account opens in another tab.
 * It is pinned even while it is the browser's only account, so that once it
 * signs out and someone else signs in, this tab does not quietly act as them.
 */
export const tabAccountAtom = atom<TabAccount | null>(null);

/** How many accounts the browser had at the last read of the list. */
export const browserAccountCountAtom = atom(0);

export function tabAccountOf(account: BrowserAccount): TabAccount {
  return {
    accountId: account.accountId,
    u: account.u,
    username: account.username,
  };
}

export function pinTabAccount(
  account: BrowserAccount | TabAccount | null,
  count: number,
): void {
  store.set(browserAccountCountAtom, count);
  const current = store.get(tabAccountAtom);
  if (account === null) {
    if (current !== null) store.set(tabAccountAtom, null);
    return;
  }
  if (
    current?.accountId === account.accountId &&
    current.u === account.u &&
    current.username === account.username
  ) {
    return;
  }
  store.set(tabAccountAtom, {
    accountId: account.accountId,
    u: account.u,
    username: account.username,
  });
}

/**
 * The account the API header names, or undefined to let the server use its
 * default. A browser with one account sends nothing, so a request reads the
 * same as it did before several accounts were possible.
 */
export function headerAccountId(): number | undefined {
  const pinned = store.get(tabAccountAtom);
  if (pinned === null || store.get(browserAccountCountAtom) <= 1) {
    return undefined;
  }
  return pinned.accountId;
}

/**
 * What a tab does with the `u` its address carries (design 6.1):
 * - `pin`: act as `account` (none while nothing is signed in), with `u` as
 *   the address's value, which is replaced when it differs;
 * - `strip`: act as the browser's one account and drop `u` from the address;
 * - `choose`: several accounts match a shortened `u`, so the reader picks;
 * - `login`: no account here matches `u`.
 */
export type TabResolution =
  | { kind: "pin"; account: BrowserAccount | null; u: string | undefined }
  | { kind: "strip"; account: BrowserAccount }
  | { kind: "choose" }
  | { kind: "login" };

/**
 * Decides the tab's account from the browser's accounts (in sign-in order)
 * and the address's `u`. `u` matches an account by prefix, either way round,
 * because it grows from 4 to 8 characters while two accounts share the first
 * four. Without `u`, a tab with several accounts keeps the one it is pinned
 * to (`currentId`) while it is still here, and otherwise takes the first.
 */
export function resolveTabAccount(
  accounts: readonly BrowserAccount[],
  urlU: string | undefined,
  currentId?: number,
): TabResolution {
  if (urlU === undefined || urlU === "") {
    if (accounts.length === 0)
      return { kind: "pin", account: null, u: undefined };
    if (accounts.length === 1) {
      return { kind: "pin", account: accounts[0] ?? null, u: undefined };
    }
    const account =
      accounts.find((candidate) => candidate.accountId === currentId) ??
      accounts[0];
    return { kind: "pin", account: account ?? null, u: account?.u };
  }
  const matches = accounts.filter((account) => sameRef(account.u, urlU));
  const [only] = matches;
  if (only === undefined) return { kind: "login" };
  if (matches.length > 1) return { kind: "choose" };
  if (accounts.length === 1) return { kind: "strip", account: only };
  return { kind: "pin", account: only, u: urlU };
}

/**
 * Pages that are not one account's: the sign-in steps and the pages that add
 * an account (enrollment, pairing), the account chooser, the sign-out
 * confirmation and the error page. They never add `u`, never send the reader
 * elsewhere over it, and are left alone when an account signs out.
 */
export function isUnpinnedPath(pathname: string): boolean {
  return unpinnedPaths.has(pathname) || pathname.startsWith("/enroll/");
}

const unpinnedPaths = new Set([
  "/login",
  "/login/totp",
  "/login/recovery",
  "/choose-account",
  "/sign-out",
  "/error",
  "/pair",
]);

/** An address on this site without its `u`, for a page to come back to. */
export function withoutU(href: string): string {
  const url = new URL(href, window.location.origin);
  url.searchParams.delete("u");
  return url.pathname + url.search + url.hash;
}

/** An address on this site with `u` set. */
export function withU(href: string, u: string): string {
  const url = new URL(href, window.location.origin);
  url.searchParams.set("u", u);
  return url.pathname + url.search + url.hash;
}

/**
 * What the API client asks of the app when a response says the tab's account
 * may be gone: the response came back as another account, or the server said
 * the account the header named is not signed in here. The app checks the
 * list again and, if the account really is gone, signs the tab out. Set by
 * `useAccountSync`, which has the router and the query client.
 */
let tabAccountCheck: (() => void) | undefined;

export function setTabAccountCheck(check: (() => void) | undefined): void {
  tabAccountCheck = check;
}

export function requestTabAccountCheck(): void {
  tabAccountCheck?.();
}
