/**
 * What the console should believe about the account, independent of the server.
 *
 * The store is a module-level external store rather than a jotai atom: the
 * devtools panel renders through a portal, outside the app's `Provider`.
 */
export interface MockConfig {
  /** Whether reads answer from the panel instead of the server. */
  enabled: boolean;
  /** Whether writes answer from the panel as well. Inert while `enabled` is off. */
  writes: boolean;
  /** Milliseconds every mocked response waits before it is answered. */
  delayMs: number;
  session: {
    signedIn: boolean;
    displayName: string;
    username: string;
    avatarPending: boolean;
    /**
     * The role the sign-in carries, which is also the management area's gate:
     * the sidebar hides it and `_protected.admin` redirects away from it unless
     * the account is an admin.
     */
    role: "admin" | "member";
  };
  factors: {
    passwordSet: boolean;
    totpEnrolled: boolean;
    passkeys: number;
    recoveryCodes: number;
  };
  lists: {
    sessions: number;
    identities: number;
    tokens: number;
    consentedApps: number;
    federationProviders: number;
    forwardAuthApps: number;
  };
  /**
   * The management area's directory. Kept apart from `lists` because these are
   * the instance's records rather than the signed-in account's, and because
   * their ceiling is higher: the account directory pages by cursor, so a
   * walkthrough needs enough rows to reach the second page.
   */
  admin: {
    accounts: number;
    groups: number;
    invitations: number;
    /** Audit log entries, paged like the directory and capped the same. */
    auditEvents: number;
    /** Signing keys, newest first; a short list, capped like `lists`. */
    signingKeys: number;
    /**
     * Each signing key's state as one letter, newest first — `P`ending,
     * `A`ctive, `D`ecommissioning, `R`etired, or `X` for decommissioning
     * straight from pending — once a write has moved one.
     * A key past the end of the string takes its state from its position.
     * A string rather than a list, so it survives the stored-config overlay.
     */
    signingKeyStates: string;
  };
  sudo: {
    fresh: boolean;
    webauthn: boolean;
    passwordTotp: boolean;
  };
  instance: {
    maintenance: boolean;
    maintenanceMessage: string;
    bootstrapped: boolean;
    /** The saved name override; empty means the configured name. */
    name: string;
    customIcon: boolean;
    customBackground: boolean;
    /** Bumped by every image write, so `/config` reports a new ETag. */
    imageRevision: number;
    clientIpStrategy: "direct" | "forwarded" | "header";
    clientIpHeader: string;
    /** The trusted proxies, one per line, like the settings field. */
    trustedProxies: string;
  };
}

/** Upper bound for every list length, so one control cannot render a huge table. */
export const mockListMax = 10;

/**
 * Upper bound for the management directory. Higher than `mockListMax` because
 * the account list pages by cursor: reaching the second page is part of what a
 * walkthrough has to be able to see.
 */
export const mockAdminListMax = 50;

/** How many rows one mocked cursor page carries. */
export const mockPageSize = 5;

/** Upper bound for the response delay, so one control cannot stall a page for minutes. */
export const mockDelayMax = 5000;

export const defaultMockConfig: MockConfig = {
  enabled: false,
  writes: false,
  delayMs: 700,
  session: {
    signedIn: true,
    displayName: "Mock Member",
    username: "mock",
    avatarPending: false,
    role: "admin",
  },
  factors: {
    passwordSet: true,
    totpEnrolled: true,
    passkeys: 2,
    recoveryCodes: 8,
  },
  lists: {
    sessions: 2,
    identities: 1,
    tokens: 1,
    consentedApps: 2,
    federationProviders: 2,
    forwardAuthApps: 1,
  },
  admin: {
    accounts: 12,
    groups: 3,
    invitations: 4,
    auditEvents: 24,
    signingKeys: 4,
    signingKeyStates: "",
  },
  sudo: { fresh: true, webauthn: true, passwordTotp: true },
  instance: {
    maintenance: false,
    maintenanceMessage: "Scheduled maintenance is in progress.",
    bootstrapped: true,
    name: "",
    customIcon: false,
    customBackground: false,
    imageRevision: 0,
    clientIpStrategy: "direct",
    clientIpHeader: "",
    trustedProxies: "",
  },
} satisfies MockConfig;

const storageKey = "prohibitorum.devtools.mock";

function clone(config: MockConfig): MockConfig {
  return structuredClone(config);
}

/**
 * Copies stored values onto the defaults, one key at a time, keeping a default
 * whose stored value has the wrong type. New fields therefore arrive with their
 * default and a config written by an older panel still loads.
 */
function overlay(
  base: Record<string, unknown>,
  stored: unknown,
): Record<string, unknown> {
  if (typeof stored !== "object" || stored === null) return { ...base };
  const source = stored as Record<string, unknown>;
  const result: Record<string, unknown> = { ...base };
  for (const key of Object.keys(base)) {
    const fallback = base[key];
    const value = source[key];
    if (value === undefined) continue;
    if (typeof fallback === "object" && fallback !== null) {
      result[key] = overlay(fallback as Record<string, unknown>, value);
    } else if (typeof value === typeof fallback) {
      result[key] = value;
    }
  }
  return result;
}

function load(): MockConfig {
  // Mocking never survives a reload: a console left serving fabricated data
  // would hide the server it is being developed against.
  const base = clone(defaultMockConfig);
  let stored: unknown;
  try {
    stored = JSON.parse(window.localStorage.getItem(storageKey) ?? "null");
  } catch {
    stored = null;
  }
  const merged = overlay(
    base as unknown as Record<string, unknown>,
    stored,
  ) as unknown as MockConfig;
  merged.enabled = false;
  return merged;
}

let current = load();
const listeners = new Set<() => void>();

export function getMockConfig(): MockConfig {
  return current;
}

export function subscribeMockConfig(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

function commit(next: MockConfig): void {
  current = next;
  try {
    window.localStorage.setItem(
      storageKey,
      JSON.stringify({ ...next, enabled: false }),
    );
  } catch {
    // Private-mode storage or a quota error: the panel still works in memory.
  }
  for (const listener of listeners) listener();
}

/** Applies a change to a copy of the config and publishes it. */
export function updateMockConfig(apply: (draft: MockConfig) => void): void {
  const draft = clone(current);
  apply(draft);
  commit(draft);
}

export function resetMockConfig(): void {
  commit(clone(defaultMockConfig));
}

export function clampCount(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, Math.min(mockListMax, Math.floor(value)));
}

export function clampAdminCount(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, Math.min(mockAdminListMax, Math.floor(value)));
}

export function clampDelay(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, Math.min(mockDelayMax, Math.round(value)));
}
