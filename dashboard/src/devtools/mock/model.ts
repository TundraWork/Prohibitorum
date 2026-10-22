import type { LoginAppearance } from "@/api/raw-paths";
import { defaultLoginAppearance } from "@/components/custom/login-appearance/appearance";

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
    /**
     * The role the sign-in carries, which is also the management area's gate:
     * the sidebar hides it and `_protected.admin` redirects away from it unless
     * the account is an admin.
     */
    role: "admin" | "member";
    /**
     * How many applications of each kind the account is assigned to manage.
     *
     * A delegated manager reaches the application sections without being an
     * admin, so this is what a walkthrough varies to see the member's view of
     * them: the sidebar entries appear one kind at a time, and each list answers
     * with only the assigned rows.
     */
    managedApps: { oidc: number; saml: number; forwardAuth: number };
  };
  /** The pictures `GET /me/avatar` lists and the one the session shows. */
  avatar: {
    /** Whether the account has uploaded a picture of its own. */
    upload: boolean;
    /** How many upstream pictures are stored, 0–3 (`mockAvatarUpstreamsMax`). */
    upstreams: number;
    /** The source in use: `user`, `none` or `upstream:provider-N`. */
    active: string;
    /** Whether the account has picked an avatar itself. */
    selected: boolean;
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
  /** The home page's launcher: the applications this account may open. */
  launchpad: {
    /** How many applications `GET /me/apps` lists, up to 50. */
    apps: number;
    /** Whether the list answers with a server error. */
    fail: boolean;
  };
  /**
   * The management area's directory. Kept apart from `lists` because these are
   * the instance's records rather than the signed-in account's, and because
   * their ceiling is higher: the account directory pages by cursor, so a
   * walkthrough needs enough rows to reach the second page.
   */
  admin: {
    accounts: number;
    /**
     * Whether the directory's non-admin accounts carry `member`, a role the
     * server does not issue, so the account page's handling of a stored role
     * outside user and admin can be walked through.
     */
    legacyRoleAccounts: boolean;
    /** Whether the account directory answers with a server error, so its failure can be walked through. */
    accountsFail: boolean;
    /** Whether the account directory answers its first page and then fails every later one, so the rest of a paged list can be seen failing. */
    accountsFailLater: boolean;
    /** Whether the identity provider directory answers with a server error. */
    identityProvidersFail: boolean;
    /**
     * Active accounts a rule is evaluated against. Apart from `accounts`
     * because a rule's total is not bounded by the directory page and a
     * walkthrough needs one larger than a page of matches.
     */
    ruleAccounts: number;
    /** Every Nth rule account matches (1 matches all), so matches can be sparse. */
    ruleMatchEvery: number;
    groups: number;
    /**
     * Allow and deny decisions a manual group holds, paged like the directory;
     * every third one is a deny. Apart from `groups` because a group's
     * decisions are not bounded by the directory page and a walkthrough needs
     * more than one page of them, or none.
     */
    groupDecisions: number;
    /** Decisions per page, so a short list still has a second page (1-50). */
    groupDecisionsPageSize: number;
    /** Whether every page after the first is refused, so a walk can fail. */
    groupDecisionsLaterPagesFail: boolean;
    invitations: number;
    /** Whether creating an invitation is refused with a server error, so the form's failure can be walked through. */
    invitationCreateFails: boolean;
    /** Audit log entries, paged like the directory and capped the same. */
    auditEvents: number;
    /** Whether reading the audit log is refused with a server error, so its failure can be walked through. */
    auditFails: boolean;
    /** Signing keys, newest first; a short list, capped like `lists`. */
    signingKeys: number;
    /** Whether every page of signing keys after the first is refused with a server error. */
    signingKeysLaterPageFails: boolean;
    /**
     * Identity providers. The fixture cycles protocols and states in order, so a
     * walkthrough always sees an OIDC, a Steam and a VRChat provider, an enabled
     * one, a disabled one and one that is not ready.
     */
    identityProviders: number;
    /**
     * Which fixture providers have sign-in off, one letter per row: `.` keeps
     * the protocol default (off for VRChat, on otherwise), `x` turns it off,
     * `o` turns it on. A string rather than a list, so it survives the
     * stored-config overlay. Rows past its length take `.`.
     */
    providerSignInMask: string;
    /**
     * Which fixture providers are not ready, one letter per row: `.` keeps the
     * fixture's own credential, `x` removes it (no Steam key, an expired
     * VRChat session, no OIDC secret) so the row is not ready, `o` supplies
     * it. A public OIDC client needs no credential, so `x` breaks its connection
     * instead and `o` leaves it ready. A string,
     * like the sign-in mask, so it survives the stored-config overlay.
     */
    providerNotReadyMask: string;
    /** OIDC applications, paged; covers confidential and public, restricted and open. */
    oidcApps: number;
    /** SAML applications, paged. */
    samlApps: number;
    /**
     * How many attributes each SAML application that has an attribute map
     * sends; the cycle of fixtures keeps every third one at none.
     */
    samlAttributes: number;
    /** Forward-auth applications, paged. */
    forwardAuthApps: number;
    /**
     * What a diagnostic run reports once it finishes: `succeeded` shows the
     * mapped fields and both documents, `fallback` a provider that returns no
     * ID token (everything from UserInfo), `failed` a code exchange that fails
     * before there is anything to show, and `running` never finishes, so the
     * result dialog's wait for a slow provider can be reached.
     */
    diagnosticOutcome: "succeeded" | "fallback" | "failed" | "running";
    /**
     * Every VRChat provider's operator session: `fixture` keeps the cycle of
     * fixtures (a checked session on even rows, none on odd ones), the rest
     * set them all to a valid, an expired or an absent session.
     */
    operatorSession: "fixture" | "valid" | "expired" | "none";
    /**
     * What an account's access lists hold: `fixture` cycles by account (one to
     * two identities, up to two passkeys, one to three sessions, tokens on odd
     * ids), `empty` leaves all four lists empty, and `long` fills them with
     * values that do not fit a line (an unnameable User-Agent, a subject with
     * no break point).
     */
    accountAccess: "fixture" | "empty" | "long";
    /** Whether checking a VRChat operator session is refused by the rate limit. */
    operatorCheckFails: boolean;
    /**
     * The latest finished connection test every provider reports: `none` has
     * not run one yet, `passed` and `failed` finished that way, `stale`
     * passed before the configuration changed, and `failed-stale` failed before it.
     */
    latestConnectionTest:
      | "none"
      | "passed"
      | "failed"
      | "stale"
      | "failed-stale";
    /**
     * Identity fields the latest test did not find, space-separated by name
     * (`email picture`). A string rather than a list, so it survives the
     * stored-config overlay. Inert unless the test passed or went stale.
     */
    latestConnectionTestAbsent: string;
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
  /** The device a pairing lookup finds, whatever code was entered. */
  pairing: {
    /** How long the pairing has left when it is looked up, in seconds. */
    expiresInSeconds: number;
    /** Whether this account has already approved it. */
    alreadyBound: boolean;
    /** Whether it asked from this device's own address. */
    sameNetwork: boolean;
  };
  /** The public pages an application sends the browser to. */
  publicFlows: {
    consent: {
      /**
       * `first` asks about every scope; `incremental` reports the first three
       * as allowed before, so the page lists only the rest.
       */
      grant: "first" | "incremental";
      /** The scopes the application asks for, space-separated as in OIDC. */
      scopes: string;
      /** Whether the application has a logo, a privacy policy and terms. */
      logo: boolean;
      policy: boolean;
      terms: boolean;
      /** Off answers every ticket as expired or used. */
      ticketValid: boolean;
      /** How many attributes a SAML service receives; 0 draws the fallback line. */
      samlAttributes: number;
    };
    /** What an enrollment link previews, whatever its token. */
    enrollment: {
      intent: "bootstrap" | "invite" | "reset";
      /** Whether the link fixes the username, so the page shows it read-only. */
      fixedUsername: boolean;
      /** Whether a password and an authenticator are allowed besides a passkey. */
      passwordTotp: boolean;
      /** How many upstream providers the page offers, up to three. */
      providers: number;
      /** Whether the invitation must be accepted through the first provider. */
      bound: boolean;
      /** Whether the invite must set a local sign-in method before the account exists. */
      requireLocal: boolean;
      /** Whether the invite already carries a verified provider identity. */
      verified: boolean;
      /** Off answers every token as expired. */
      valid: boolean;
    };
    /** The account a first federated sign-in prepared, on `/welcome`. */
    welcome: {
      /** Whether the prepared account has a picture from the provider. */
      avatar: boolean;
      /** Whether confirming goes on to offer a local sign-in. */
      offerLocalSignin: boolean;
      /** Off answers as an expired sign-in. */
      valid: boolean;
    };
    /** An application's require-identity request on `/require-identity/$ticket`. */
    requireIdentity: {
      /**
       * `pending` lists the providers; `mismatch` is signed in as another
       * account than the application's; `satisfied` returns at once;
       * `expired` answers every ticket as gone.
       */
      state: "pending" | "mismatch" | "satisfied" | "expired";
      /** How many providers the application lists (1–3). */
      providers: number;
    };
    /** A VRChat profile verification on `/federation/flow/$flow`. */
    flow: {
      intent: "login" | "link" | "invite";
      step: "identify" | "proof";
      /** Whether verifying asks for the new account's username. */
      requiresLocalUsername: boolean;
      /** Whether verifying answers that the link is not in the bio yet. */
      proofMissing: boolean;
    };
    /** The pairing `/pair` starts, as the new device sees it. */
    pairing: {
      /**
       * How many status reads answer "pending" before one answers "approved".
       * 0 never approves, so the code is left to run out.
       */
      approveAfterPolls: number;
      /** How long a new code lasts, in seconds. */
      expiresInSeconds: number;
    };
  };
  instance: {
    maintenance: boolean;
    maintenanceMessage: string;
    bootstrapped: boolean;
    /** The saved name override; empty means the configured name. */
    name: string;
    customIcon: boolean;
    /** The saved sign-in page look, as `/config` and the settings report it. */
    loginAppearance: LoginAppearance;
    /** How many sign-in background images are uploaded, 0–10. */
    loginImageCount: number;
    hasUnsplashKey: boolean;
    /** Bumped by every image write, so `/config` reports a new ETag. */
    imageRevision: number;
    clientIpStrategy: "direct" | "forwarded" | "header";
    clientIpHeader: string;
    /** The trusted proxies, one per line, like the settings field. */
    trustedProxies: string;
    /**
     * Whether SMTP is configured. One switch answers three reads the way the
     * server's one config file does: the settings panel's rows, whether a
     * test mail or a verification code can be sent at all, and the warnings
     * the profile's email list draws.
     */
    smtpConfigured: boolean;
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

/** Upper bound for the accounts a rule is evaluated against. */
export const mockRuleAccountsMax = 500;

/** Upper bound for a manual group's decisions: a few hundred, several pages. */
export const mockDecisionsMax = 500;

export function clampDecisionCount(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, Math.min(mockDecisionsMax, Math.floor(value)));
}

/** The server's largest page of a group's decisions. */
export const mockDecisionsPageMax = 50;

export function clampDecisionsPageSize(value: number): number {
  if (!Number.isFinite(value)) return mockDecisionsPageMax;
  return Math.max(1, Math.min(mockDecisionsPageMax, Math.floor(value)));
}

export function clampRuleMatchEvery(value: number): number {
  if (!Number.isFinite(value)) return 1;
  return Math.max(1, Math.min(mockRuleAccountsMax, Math.floor(value)));
}

export function clampRuleAccounts(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, Math.min(mockRuleAccountsMax, Math.floor(value)));
}

/** How many rows one mocked cursor page carries. */
export const mockPageSize = 5;

/** Upper bound for the response delay, so one control cannot stall a page for minutes. */
export const mockDelayMax = 5000;

/**
 * Bounds for how long a mocked pairing has left. Long enough to be read, short
 * enough for a walkthrough to wait out; the server's own limit is 300.
 */
export const mockPairingExpiryMin = 5;
export const mockPairingExpiryMax = 600;

export const defaultMockConfig: MockConfig = {
  enabled: false,
  writes: false,
  delayMs: 700,
  session: {
    signedIn: true,
    displayName: "Mock Member",
    username: "mock",
    role: "admin",
    managedApps: { oidc: 0, saml: 0, forwardAuth: 0 },
  },
  avatar: { upload: true, upstreams: 2, active: "user", selected: false },
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
  launchpad: { apps: 9, fail: false },
  admin: {
    accounts: 12,
    legacyRoleAccounts: false,
    accountsFail: false,
    accountsFailLater: false,
    identityProvidersFail: false,
    ruleAccounts: 12,
    ruleMatchEvery: 2,
    groups: 3,
    groupDecisions: 4,
    groupDecisionsPageSize: 50,
    groupDecisionsLaterPagesFail: false,
    invitations: 4,
    invitationCreateFails: false,
    auditEvents: 24,
    auditFails: false,
    signingKeys: 4,
    signingKeysLaterPageFails: false,
    signingKeyStates: "",
    identityProviders: 4,
    providerSignInMask: "",
    providerNotReadyMask: "",
    oidcApps: 12,
    samlApps: 6,
    samlAttributes: 3,
    forwardAuthApps: 6,
    diagnosticOutcome: "succeeded",
    operatorSession: "fixture",
    accountAccess: "fixture",
    operatorCheckFails: false,
    latestConnectionTest: "passed",
    latestConnectionTestAbsent: "picture",
  },
  sudo: { fresh: true, webauthn: true, passwordTotp: true },
  pairing: { expiresInSeconds: 300, alreadyBound: false, sameNetwork: false },
  publicFlows: {
    consent: {
      grant: "first",
      scopes:
        "openid profile email groups offline_access identity:provider-1 wiki:write",
      logo: true,
      policy: true,
      terms: true,
      ticketValid: true,
      samlAttributes: 3,
    },
    enrollment: {
      intent: "invite",
      fixedUsername: false,
      passwordTotp: true,
      providers: 1,
      bound: false,
      requireLocal: false,
      verified: false,
      valid: true,
    },
    welcome: {
      avatar: true,
      offerLocalSignin: true,
      valid: true,
    },
    flow: {
      intent: "login",
      step: "identify",
      requiresLocalUsername: false,
      proofMissing: false,
    },
    pairing: { approveAfterPolls: 3, expiresInSeconds: 300 },
    requireIdentity: { state: "pending", providers: 2 },
  },
  instance: {
    maintenance: false,
    maintenanceMessage: "Scheduled maintenance is in progress.",
    bootstrapped: true,
    name: "",
    customIcon: false,
    loginAppearance: structuredClone(defaultLoginAppearance),
    loginImageCount: 0,
    hasUnsplashKey: false,
    imageRevision: 0,
    clientIpStrategy: "direct",
    clientIpHeader: "",
    trustedProxies: "",
    smtpConfigured: true,
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

/** The server keeps at most ten sign-in background images. */
export const mockLoginImagesMax = 10;

export function clampAdminCount(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, Math.min(mockAdminListMax, Math.floor(value)));
}

export function clampDelay(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, Math.min(mockDelayMax, Math.round(value)));
}

/** The most upstream pictures the avatar gallery lists. */
export const mockAvatarUpstreamsMax = 3;

export function clampAvatarUpstreams(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, Math.min(mockAvatarUpstreamsMax, Math.floor(value)));
}

/** The most providers an enrollment preview offers. */
export const mockEnrollmentProvidersMax = 3;

export function clampEnrollmentProviders(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, Math.min(mockEnrollmentProvidersMax, Math.floor(value)));
}

export function clampPairingExpiry(value: number): number {
  if (!Number.isFinite(value)) return mockPairingExpiryMin;
  return Math.max(
    mockPairingExpiryMin,
    Math.min(mockPairingExpiryMax, Math.round(value)),
  );
}
