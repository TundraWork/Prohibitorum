/**
 * What this browser remembers about its accounts between visits, in
 * localStorage. Neither record is ever trusted: the known usernames only fill
 * in the sign-in page's username field, and the last-used marks only label a
 * row on the account chooser.
 */

const knownKey = "prohibitorum.accounts.known";
const lastUsedPrefix = "prohibitorum.accounts.last-used.";

// Private browsing, a full quota or a policy can make storage throw; losing
// a prefill or a label is better than failing the page.
function read(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

function write(key: string, value: string | null): void {
  try {
    if (value === null) window.localStorage.removeItem(key);
    else window.localStorage.setItem(key, value);
  } catch {
    // See `read`.
  }
}

/**
 * Two `u` values name the same account when one is a prefix of the other: a
 * `u` grows from 4 to 8 characters while another account on the browser
 * shares its first four.
 */
export function sameRef(a: string, b: string): boolean {
  return a.startsWith(b) || b.startsWith(a);
}

/** `{ [u]: username }` for the accounts signed in here before. */
export function readKnownAccounts(): Record<string, string> {
  const raw = read(knownKey);
  if (raw === null) return {};
  try {
    const parsed: unknown = JSON.parse(raw);
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed))
      return {};
    const known: Record<string, string> = {};
    for (const [u, username] of Object.entries(parsed)) {
      if (typeof username === "string") known[u] = username;
    }
    return known;
  } catch {
    return {};
  }
}

function writeKnown(known: Record<string, string>): void {
  write(
    knownKey,
    Object.keys(known).length === 0 ? null : JSON.stringify(known),
  );
}

export function rememberKnownAccount(u: string, username: string): void {
  if (u === "" || username === "") return;
  const known = readKnownAccounts();
  if (known[u] === username) return;
  for (const key of Object.keys(known)) {
    if (sameRef(key, u)) delete known[key];
  }
  known[u] = username;
  writeKnown(known);
}

export function forgetKnownAccount(u: string): void {
  const known = readKnownAccounts();
  let changed = false;
  for (const key of Object.keys(known)) {
    if (sameRef(key, u)) {
      delete known[key];
      changed = true;
    }
  }
  if (changed) writeKnown(known);
}

export function clearKnownAccounts(): void {
  write(knownKey, null);
}

/** The username remembered for `u`, when exactly one record matches it. */
export function knownUsername(u: string | undefined): string | undefined {
  if (u === undefined || u === "") return undefined;
  const names = new Set(
    Object.entries(readKnownAccounts())
      .filter(([key]) => sameRef(key, u))
      .map(([, username]) => username),
  );
  return names.size === 1 ? [...names][0] : undefined;
}

/** The `u` last chosen for an application (`oidc:<client_id>`, `saml:<sp id>`). */
export function readLastUsed(appKey: string): string | undefined {
  if (appKey === "") return undefined;
  return read(lastUsedPrefix + appKey) ?? undefined;
}

export function writeLastUsed(appKey: string, u: string): void {
  if (appKey === "" || u === "") return;
  write(lastUsedPrefix + appKey, u);
}
