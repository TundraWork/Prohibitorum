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
  session: {
    signedIn: boolean;
    displayName: string;
    username: string;
    avatarPending: boolean;
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
  sudo: {
    fresh: boolean;
    webauthn: boolean;
    passwordTotp: boolean;
  };
  instance: {
    maintenance: boolean;
    bootstrapped: boolean;
  };
}

/** Upper bound for every list length, so one control cannot render a huge table. */
export const mockListMax = 10;

export const defaultMockConfig: MockConfig = {
  enabled: false,
  writes: false,
  session: {
    signedIn: true,
    displayName: "Mock Member",
    username: "mock",
    avatarPending: false,
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
  sudo: { fresh: true, webauthn: true, passwordTotp: true },
  instance: { maintenance: false, bootstrapped: true },
};

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
