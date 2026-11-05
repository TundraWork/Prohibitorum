import type { MockConfig } from "@/devtools/mock/model";

/** The request and response header that names the account a request acts as. */
export const accountHeader = "X-Prohibitorum-Account";

/** The server's own ceiling for `PROHIBITORUM_SESSION_MAX_ACCOUNTS`. */
export const mockAccountLimitMax = 20;

export function clampAccountLimit(value: number): number {
  if (!Number.isFinite(value)) return 1;
  return Math.max(1, Math.min(mockAccountLimitMax, Math.floor(value)));
}

/** One account signed in on the mocked browser. */
export interface MockAccount {
  id: number;
  username: string;
  displayName: string;
  role: "admin" | "member";
  /** Whether the account may use the application the chooser is asked for. */
  allowed: boolean;
}

/**
 * The accounts after the first, as the mask spells them. A position past the
 * ceiling is ignored: the server never holds more than the ceiling.
 */
function otherLetters(config: MockConfig): string[] {
  return [...config.session.others.slice(0, mockAccountLimitMax - 1)];
}

function allowedFor(config: MockConfig, id: number): boolean {
  return config.session.chooserDenied[id - 1] !== "x";
}

/**
 * The browser's accounts in their fixed order. Account 1 is the one the rest
 * of the panel describes (its name, role, factors and pictures) and the
 * directory's first row; account N after it is the directory's row N, so the
 * admin list and the avatar menu name the same people.
 */
export function browserAccounts(config: MockConfig): MockAccount[] {
  const accounts: MockAccount[] = [];
  if (config.session.signedIn) {
    accounts.push({
      id: 1,
      username: config.session.username,
      displayName: config.session.displayName,
      role: config.session.role,
      allowed: allowedFor(config, 1),
    });
  }
  otherLetters(config).forEach((letter, index) => {
    if (letter !== "a" && letter !== "m") return;
    const id = index + 2;
    accounts.push({
      id,
      username: `mock-user-${id}`,
      displayName: `Mock User ${id}`,
      role: letter === "a" ? "admin" : "member",
      allowed: allowedFor(config, id),
    });
  });
  return accounts;
}

export function browserAccount(
  config: MockConfig,
  id: number,
): MockAccount | undefined {
  return browserAccounts(config).find((account) => account.id === id);
}

/** Account `id` as it is or as it would be once it signs in, as a member. */
export function accountFor(config: MockConfig, id: number): MockAccount {
  return (
    browserAccount(config, id) ?? {
      id,
      username: id === 1 ? config.session.username : `mock-user-${id}`,
      displayName: id === 1 ? config.session.displayName : `Mock User ${id}`,
      role: id === 1 ? config.session.role : "member",
      allowed: allowedFor(config, id),
    }
  );
}

/** Whether a sign-in of an account not already here would be refused. */
export function browserAtLimit(config: MockConfig): boolean {
  return browserAccounts(config).length >= config.session.limit;
}

/**
 * What a request's account header resolves to: `none` when the browser holds
 * no account and none was named, `absent` when the named one is not here,
 * `invalid` when the header is not a positive integer.
 */
export type AccountSelection =
  | { kind: "account"; account: MockAccount }
  | { kind: "none" }
  | { kind: "absent"; id: number }
  | { kind: "invalid" };

export function selectBrowserAccount(
  config: MockConfig,
  header: string | null | undefined,
): AccountSelection {
  if (header === null || header === undefined || header === "") {
    const first = browserAccounts(config)[0];
    return first ? { kind: "account", account: first } : { kind: "none" };
  }
  if (!/^[1-9][0-9]*$/.test(header)) return { kind: "invalid" };
  const id = Number(header);
  const account = browserAccount(config, id);
  return account ? { kind: "account", account } : { kind: "absent", id };
}

/**
 * A stand-in for the server's keyed reference: FNV-1a over the same input, so
 * an account keeps its `u` across reloads without the mock holding a key.
 */
function fullRef(id: number): string {
  let hash = 0x811c9dc5;
  for (const char of `account-ref:${id}`) {
    hash ^= char.charCodeAt(0);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return hash.toString(16).padStart(8, "0");
}

/**
 * Each account's `u`: four hex digits, or eight for the accounts whose four
 * digits collide inside this set, as the server shortens them.
 */
export function accountShorts(ids: readonly number[]): Map<number, string> {
  const counts = new Map<string, number>();
  for (const id of new Set(ids)) {
    const short = fullRef(id).slice(0, 4);
    counts.set(short, (counts.get(short) ?? 0) + 1);
  }
  const shorts = new Map<number, string>();
  for (const id of ids) {
    const full = fullRef(id);
    const short = full.slice(0, 4);
    shorts.set(id, (counts.get(short) ?? 0) > 1 ? full : short);
  }
  return shorts;
}

export function browserShorts(config: MockConfig): Map<number, string> {
  return accountShorts(browserAccounts(config).map((account) => account.id));
}

/** The account a new sign-in takes: the first free place in the order. */
export function nextAccountId(config: MockConfig): number {
  if (!config.session.signedIn) return 1;
  const gap = otherLetters(config).findIndex(
    (letter) => letter !== "a" && letter !== "m",
  );
  return gap === -1 ? otherLetters(config).length + 2 : gap + 2;
}

/** Signs account `id` in, keeping a role it already has. */
export function addBrowserAccount(draft: MockConfig, id: number): void {
  if (id === 1) {
    draft.session.signedIn = true;
    return;
  }
  const letters = otherLetters(draft);
  while (letters.length < id - 1) letters.push(".");
  const index = id - 2;
  if (letters[index] !== "a") letters[index] = "m";
  draft.session.others = letters.join("");
}

/**
 * Signs one account out. The others keep their ids, so a tab pinned to a later
 * account still finds it.
 */
export function removeBrowserAccount(draft: MockConfig, id: number): void {
  if (id === 1) {
    draft.session.signedIn = false;
    return;
  }
  const letters = otherLetters(draft);
  if (id - 2 < letters.length) letters[id - 2] = ".";
  draft.session.others = letters.join("").replace(/\.+$/, "");
}

export function removeAllBrowserAccounts(draft: MockConfig): void {
  draft.session.signedIn = false;
  draft.session.others = "";
}

/**
 * Sets how many accounts the browser holds, renumbering them 1…n: account 1
 * and the roles already given keep their place, new ones are members.
 */
export function setBrowserAccountCount(draft: MockConfig, count: number): void {
  const total = Math.max(0, Math.min(mockAccountLimitMax, Math.floor(count)));
  const kept = otherLetters(draft).filter(
    (letter) => letter === "a" || letter === "m",
  );
  draft.session.signedIn = total > 0;
  const others = Math.max(0, total - 1);
  while (kept.length < others) kept.push("m");
  draft.session.others = kept.slice(0, others).join("");
}

/** Fills the browser up to its limit, the state the limit alert is drawn in. */
export function fillBrowserToLimit(draft: MockConfig): void {
  setBrowserAccountCount(draft, draft.session.limit);
}

export function setBrowserAccountRole(
  draft: MockConfig,
  id: number,
  role: "admin" | "member",
): void {
  if (id === 1) {
    draft.session.role = role;
    return;
  }
  const letters = otherLetters(draft);
  if (letters[id - 2] !== "a" && letters[id - 2] !== "m") return;
  letters[id - 2] = role === "admin" ? "a" : "m";
  draft.session.others = letters.join("");
}

export function setChooserAllowed(
  draft: MockConfig,
  id: number,
  allowed: boolean,
): void {
  const letters = [
    ...draft.session.chooserDenied.slice(0, mockAccountLimitMax),
  ];
  while (letters.length < id) letters.push(".");
  letters[id - 1] = allowed ? "." : "x";
  draft.session.chooserDenied = letters.join("").replace(/\.+$/, "");
}
