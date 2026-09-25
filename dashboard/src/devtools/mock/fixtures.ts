import type { components } from "@/api/generated/schema";
import type {
  AppAccessRule,
  AppGroupView,
  ManualDecisionView,
  ProviderDescriptorView,
  RulePreviewPageView,
} from "@/api/raw-admin-paths";
import type { DevicePairing, PublicConfig, SudoMethod } from "@/api/raw-paths";
import {
  clampAdminCount,
  clampCount,
  type MockConfig,
  mockListMax,
  mockPageSize,
} from "@/devtools/mock/model";

type Credential = components["schemas"]["CredentialView"];
type Session = components["schemas"]["SessionView"];
type SessionListItem = components["schemas"]["SessionListItem"];
type Identity = components["schemas"]["AccountIdentityView"];
type Token = components["schemas"]["PersonalAccessTokenView"];
type ForwardAuthApp = components["schemas"]["MyForwardAuthApp"];
type ConsentedApp = components["schemas"]["ConsentedApp"];
type Provider = components["schemas"]["FederationProvider"];
type Factors = components["schemas"]["MeFactorsView"];
type Account = components["schemas"]["AccountView"];
type Invitation = components["schemas"]["InvitationView"];
type IdentityProvider = components["schemas"]["IdentityProviderView"];
type AuditEvent = components["schemas"]["AuditEventView"];
type SigningKey = components["schemas"]["SigningKeyView"];

/** Applies what a write did to the config, so the reads that follow agree with it. */
export type MockEffect = (draft: MockConfig) => void;

/** What a mocked request answers with: a body, an empty success, or a public error. */
export type MockReply =
  | { kind: "json"; status: number; body: unknown; effect?: MockEffect }
  | { kind: "empty"; status: number; effect?: MockEffect }
  | {
      kind: "error";
      status: number;
      code: string;
      details?: Record<string, unknown>;
      effect?: MockEffect;
    };

export interface MockRequest {
  method: string;
  schemaPath: string;
  url: string;
  /** Parsed JSON body, present for a write that carried one. */
  body?: unknown;
}

const day = 86_400_000;

/** How many recovery codes an enrollment or a regeneration hands out. */
const issuedRecoveryCodes = 10;

const recoveryAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";

function iso(offsetMs: number): string {
  return new Date(Date.now() + offsetMs).toISOString();
}

function range(count: number): number[] {
  return Array.from({ length: clampCount(count) }, (_, index) => index);
}

function field(source: unknown, key: string): unknown {
  if (typeof source !== "object" || source === null) return undefined;
  return (source as Record<string, unknown>)[key];
}

function stringField(source: unknown, key: string): string | undefined {
  const value = field(source, key);
  return typeof value === "string" ? value : undefined;
}

function booleanField(source: unknown, key: string): boolean | undefined {
  const value = field(source, key);
  return typeof value === "boolean" ? value : undefined;
}

function grantsField(source: unknown): Record<string, string[] | null> {
  const value = field(source, "appGrants");
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? (value as Record<string, string[] | null>)
    : {};
}

/** Codes in the `XXXX-XXXX-XXXX-XXXX` shape the console validates against. */
function freshRecoveryCodes(): string[] {
  const alphabetLength = recoveryAlphabet.length;
  return Array.from({ length: issuedRecoveryCodes }, (_, index) => {
    const groups = Array.from({ length: 4 }, (_, group) =>
      Array.from(
        { length: 4 },
        (_, position) =>
          recoveryAlphabet[
            (index * 7 + group * 5 + position * 3) % alphabetLength
          ] ?? "A",
      ).join(""),
    );
    return groups.join("-");
  });
}

/** Solid-colour placeholder, so a mocked avatar needs no network fetch. */
const mockAvatarUrl = `data:image/svg+xml;utf8,${encodeURIComponent(
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="32" fill="#2f6f8f"/><circle cx="32" cy="25" r="11" fill="#e8f2f7"/><path d="M10 64a22 22 0 0 1 44 0z" fill="#e8f2f7"/></svg>',
)}`;

function json(body: unknown, effect?: MockEffect): MockReply {
  return { kind: "json", status: 200, body, ...(effect ? { effect } : {}) };
}

function empty(status = 204, effect?: MockEffect): MockReply {
  return { kind: "empty", status, ...(effect ? { effect } : {}) };
}

function noSession(): MockReply {
  return { kind: "error", status: 401, code: "no_session" };
}

/**
 * A request the mock has taken on but has no fixture for.
 *
 * It fails rather than reaching the server: a page that mixes fabricated and
 * real data is harder to trust than one that reports the gap, and the details
 * name the request that needs a fixture.
 */
function unmocked(request: MockRequest): MockReply {
  return {
    kind: "error",
    status: 501,
    code: "mock_unmocked",
    details: { method: request.method, path: request.schemaPath },
  };
}

/** Reads behind `/me` answer `no_session` while the panel is signed out. */
function guarded(config: MockConfig, reply: () => MockReply): MockReply {
  return config.session.signedIn ? reply() : noSession();
}

/**
 * The last path segment as a number. Every admin detail route ends in an id —
 * the account id, the group id, the account id inside an explain — so one
 * reading serves them all, and a route that ever ends otherwise gets its own.
 *
 * The segment comes from the URL because that is where a real request carries
 * it: openapi-fetch substitutes the path parameter before the request leaves,
 * so the fixture never sees a `{id}` placeholder.
 */
function pathTail(request: MockRequest): number {
  const segment = new URL(request.url).pathname.split("/").pop() ?? "";
  const value = Number(segment);
  return Number.isFinite(value) ? value : 0;
}

/** A placeholder image as a `data:` URL, so a mocked image needs no fetch. */
function svgUrl(svg: string): string {
  return `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`;
}

/**
 * The instance icon: the built-in mark, or an "uploaded" one whose colour moves
 * with every image write, so replacing it visibly changes the sidebar.
 */
function mockIconUrl(config: MockConfig): string {
  if (!config.instance.customIcon) {
    return svgUrl(
      '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="14" fill="#1f5f7a"/><path d="M20 46V18h14a9 9 0 0 1 0 18H28v10z" fill="#e8f2f7"/></svg>',
    );
  }
  const hue = (config.instance.imageRevision * 67) % 360;
  return svgUrl(
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="14" fill="hsl(${hue} 55% 42%)"/><circle cx="32" cy="32" r="14" fill="#fff" fill-opacity=".85"/></svg>`,
  );
}

/** A soft gradient standing in for an uploaded sign-in background. */
function mockBackgroundUrl(config: MockConfig): string {
  const hue = (config.instance.imageRevision * 67 + 180) % 360;
  return svgUrl(
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1600 900" preserveAspectRatio="xMidYMid slice"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="hsl(${hue} 45% 70%)"/><stop offset="1" stop-color="hsl(${(hue + 60) % 360} 45% 35%)"/></linearGradient></defs><rect width="1600" height="900" fill="url(#g)"/></svg>`,
  );
}

/* ------------------------------------------------------------------ reads -- */

function publicConfig(config: MockConfig): PublicConfig {
  const revision = String(config.instance.imageRevision);
  return {
    instanceName: config.instance.name || "Prohibitorum (mock)",
    hasCustomIcon: config.instance.customIcon,
    iconUrl: mockIconUrl(config),
    iconEtag: `icon-${revision}`,
    maintenanceMode: config.instance.maintenance,
    maintenanceMessage: config.instance.maintenanceMessage,
    hasCustomBackground: config.instance.customBackground,
    backgroundUrl: config.instance.customBackground
      ? mockBackgroundUrl(config)
      : "",
    backgroundEtag: config.instance.customBackground
      ? `background-${revision}`
      : "",
    totp: {
      issuer: "Prohibitorum (mock)",
      algorithm: "SHA1",
      digits: 6,
      period: 30,
    },
  };
}

function sessionView(config: MockConfig): Session {
  return {
    id: 1,
    username: config.session.username,
    displayName: config.session.displayName,
    role: config.session.role,
    avatarUrl: mockAvatarUrl,
    avatarPending: config.session.avatarPending,
    avatarSource: "user",
    avatarSourceLabels: { user: "My uploaded picture" },
    avatarSourceUrls: { user: mockAvatarUrl },
  };
}

function credentialViews(count: number): Credential[] {
  const transportSets: (string[] | null)[] = [["internal", "hybrid"], null];
  return range(count).map((index) => ({
    id: index + 1,
    nickname: `Passkey ${index + 1}`,
    createdAt: iso(-day * (index + 3)),
    ...(index === 0 ? { lastUsedAt: iso(-day) } : {}),
    backupState: index % 2 === 0,
    attestationType: "none",
    credentialIdSuffix: `mock-${String(index + 1).padStart(2, "0")}`,
    transports: transportSets[index % transportSets.length] ?? null,
  }));
}

function factorsView(config: MockConfig): Factors {
  return {
    passkeyCount: clampCount(config.factors.passkeys),
    passwordSet: config.factors.passwordSet,
    totpEnrolled: config.factors.totpEnrolled,
    recoveryCodesRemaining: clampCount(config.factors.recoveryCodes),
  };
}

function sessionList(count: number): SessionListItem[] {
  const agents = [
    "Mozilla/5.0 (X11; Linux x86_64) MockBrowser/1.0",
    "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) MockBrowser/1.0",
  ];
  return range(count).map((index) => ({
    id: `mock-session-${index + 1}`,
    isCurrent: index === 0,
    issuedAt: iso(-day * (index + 1)),
    expiresAt: iso(day * (30 - index)),
    lastSeenIp: `192.0.2.${index + 1}`,
    userAgent: agents[index % agents.length],
  }));
}

function identityList(count: number): Identity[] {
  return range(count).map((index) => ({
    id: index + 1,
    providerSlug: `provider-${index + 1}`,
    providerDisplayName: `Example IdP ${index + 1}`,
    protocol: index % 2 === 0 ? "oidc" : "saml",
    subject: `mock-subject-${index + 1}`,
    email: `member${index + 1}@example.com`,
    linkedAt: iso(-day * (index + 7)),
    data: {},
  }));
}

function tokenList(count: number): Token[] {
  return range(count).map((index) => ({
    id: index + 1,
    name: `Mock token ${index + 1}`,
    tokenHint: `phb_mock${index + 1}`,
    allApps: index % 2 === 0,
    appGrants: {},
    createdAt: iso(-day * (index + 2)),
    expiresAt: iso(day * 90),
    ...(index === 0 ? { lastUsedAt: iso(-day) } : {}),
  }));
}

function forwardAuthAppList(config: MockConfig): ForwardAuthApp[] {
  return range(config.lists.forwardAuthApps).map((index) => ({
    clientId: `forward-auth-${index + 1}`,
    displayName: `Protected service ${index + 1}`,
    scopes: [{ name: "profile", description: "Read your profile" }],
  }));
}

function consentedAppList(config: MockConfig): ConsentedApp[] {
  return range(config.lists.consentedApps).map((index) => ({
    clientId: `mock-client-${index + 1}`,
    kind: index % 2 === 0 ? "oidc" : "saml",
    name: `Sample application ${index + 1}`,
    grantedAt: iso(-day * (index + 4)),
    scopes: ["openid", "profile", "email"],
  }));
}

function providerList(config: MockConfig): Provider[] {
  return range(config.lists.federationProviders).map((index) => ({
    slug: `provider-${index + 1}`,
    displayName: `Example IdP ${index + 1}`,
    protocol: "oidc",
  }));
}

function sudoMethods(config: MockConfig): {
  methods: SudoMethod[];
  fresh: boolean;
} {
  const methods: SudoMethod[] = [];
  if (config.sudo.webauthn) methods.push("webauthn");
  if (config.sudo.passwordTotp) methods.push("password_totp");
  return { methods, fresh: config.sudo.fresh };
}

function pairing(code: string): DevicePairing {
  return {
    pairingId: `mock-pairing-${code}`,
    displayCode: code,
    initiatorUa: "Mozilla/5.0 (X11; Linux x86_64) MockBrowser/1.0",
    initiatorIp: "198.51.100.7",
    createdAt: iso(-60_000),
    expiresAt: iso(9 * 60_000),
    alreadyBound: false,
  };
}

/* ------------------------------------------------------- admin directory -- */

/**
 * The account directory the management area walks.
 *
 * The rows are generated from one index so every page of the directory agrees
 * with the next: account 3 is the same account whether it arrives in the first
 * cursor page, in its detail page, or in a preview list. Where a row's shape
 * varies — an admin here, a disabled account there — it varies by a rule on the
 * index rather than at random, so a walkthrough can name the row it means.
 */
function accountAt(index: number, config: MockConfig): Account {
  const id = index + 1;
  const disabled = index % 4 === 3;
  return {
    id,
    username: `mock-user-${id}`,
    displayName: `Mock User ${id}`,
    role: index % 5 === 0 ? "admin" : "member",
    // The account the panel is signed in as, so a mocked profile write and the
    // signed-in session stay one account rather than two. It is written after
    // the role above on purpose: the panel's own role wins for this row.
    ...(id === 1
      ? {
          username: config.session.username,
          displayName: config.session.displayName,
          role: config.session.role,
        }
      : {}),
    disabled,
    email: `mock-user-${id}@example.com`,
    emailVerified: index % 3 !== 2,
    oidcSubject: `mock-oidc-${id}`,
    avatarUrl: mockAvatarUrl,
    createdAt: iso(-day * (index + 30)),
    updatedAt: iso(-day * (index + 1)),
    ...(disabled ? {} : { lastSignInAt: iso(-day * (index + 1)) }),
    attributes: {},
    matchingIdentities: [],
  };
}

function accounts(config: MockConfig): Account[] {
  return Array.from(
    { length: clampAdminCount(config.admin.accounts) },
    (_, index) => accountAt(index, config),
  );
}

function accountIdentities(index: number): Identity[] {
  const id = index + 1;
  // Every third account is reachable by a single provider rather than two, so a
  // walkthrough sees both a linked pair and a lone identity.
  const count = index % 3 === 2 ? 1 : 2;
  return Array.from({ length: count }, (_, position) => ({
    id: index * 2 + position + 1,
    providerSlug: `provider-${position + 1}`,
    providerDisplayName: `Example IdP ${position + 1}`,
    protocol: position % 2 === 0 ? "oidc" : "saml",
    subject: `mock-subject-${id}-${position + 1}`,
    email: `mock-user-${id}@example.com`,
    linkedAt: iso(-day * (index + 7)),
    data: {},
  }));
}

function accountCredentials(index: number): Credential[] {
  return credentialViews(index % 3);
}

function accountSessions(index: number): SessionListItem[] {
  return sessionList((index % 3) + 1).map((session, position) => ({
    ...session,
    id: `mock-account-${index + 1}-session-${position + 1}`,
  }));
}

function accountTokens(index: number): Token[] {
  return tokenList(index % 2).map((token, position) => ({
    ...token,
    id: index * 10 + position + 1,
    name: `Mock token ${position + 1}`,
  }));
}

function invitationAt(index: number, config: MockConfig): Invitation {
  const groups =
    groupsFrom(config)
      .slice(0, index % 3)
      .map((group) => ({
        id: group.id,
        slug: group.slug,
        displayName: group.displayName,
      })) ?? [];
  return {
    token: `mock-invitation-${index + 1}`,
    url: `http://localhost:8080/enroll/mock-invitation-${index + 1}`,
    role: index % 4 === 0 ? "admin" : "member",
    createdAt: iso(-day * (index + 2)),
    expiresAt: iso(day * (7 - index)),
    username: index % 2 === 0 ? `mock-invitee-${index + 1}` : "",
    // Every other invitation insists on an upstream, which is what the list's
    // provider column reads as absent rather than empty.
    ...(index % 2 === 0 ? { expectedUpstreamIdpSlug: "provider-1" } : {}),
    groupIds: groups.map((group) => group.id),
    groups,
    attributes: {},
  };
}

/**
 * The user-group directory. One manual group and one rule group are always
 * present, because the two edit shapes are the point of the page: a walkthrough
 * that only ever sees manual groups never reaches the rule editor.
 */
function groupsFrom(config: MockConfig): AppGroupView[] {
  const generated = Array.from(
    { length: clampAdminCount(config.admin.groups) },
    (_, index): AppGroupView => {
      const id = index + 1;
      const kind = index % 2 === 0 ? "manual" : "rule";
      return {
        id,
        kind,
        slug: `${kind === "rule" ? "rule" : "manual"}-group-${id}`,
        displayName: `${kind === "rule" ? "Rule" : "Manual"} group ${id}`,
        description: index % 3 === 2 ? "" : `A mock ${kind} group.`,
        exposedToDownstream: index % 2 === 0,
        ...(kind === "rule" ? { rule: sampleRule(id) } : {}),
        applicationCount: index % 4,
      };
    },
  );
  return generated.length > 0
    ? generated
    : [
        {
          id: 1,
          kind: "manual",
          slug: "manual-group-1",
          displayName: "Manual group 1",
          description: "A mock manual group.",
          exposedToDownstream: true,
          applicationCount: 1,
        },
      ];
}

/** Two conditions and a `not`, so the rule editor has something to render. */
function sampleRule(id: number): AppAccessRule {
  return {
    version: 1,
    condition: {
      op: "all",
      children: [
        { fact: "login_method", method: id % 2 === 0 ? "passkey" : "password" },
        { fact: "connection.provider", provider: "provider-1" },
      ],
    },
  };
}

/**
 * The per-account allow/deny rows a manual group carries. Not a panel control:
 * the decisions belong to one group rather than to the instance, and a handful
 * is enough to show the list, its effects and its clear action.
 */
const mockDecisions = 4;

function groupDecisions(): ManualDecisionView[] {
  return Array.from(
    { length: mockDecisions },
    (_, index): ManualDecisionView => ({
      account: {
        id: index + 1,
        username: `mock-user-${index + 1}`,
        displayName: `Mock User ${index + 1}`,
      },
      effect: index % 3 === 2 ? "deny" : "allow",
      updatedAt: iso(-day * (index + 1)),
    }),
  );
}

function groupPreview(config: MockConfig): RulePreviewPageView {
  const items = Array.from(
    { length: clampAdminCount(config.admin.accounts) },
    (_, index) => ({
      account: {
        id: index + 1,
        username: `mock-user-${index + 1}`,
        displayName: `Mock User ${index + 1}`,
      },
      matched: index % 2 === 0,
    }),
  );
  return {
    items,
    matchedCount: items.filter((item) => item.matched).length,
    nextCursor: "",
  };
}

function identityProviders(): IdentityProvider[] {
  return [
    {
      slug: "provider-1",
      displayName: "Example IdP 1",
      protocol: "oidc",
      mode: "manual",
      disabled: false,
      ready: true,
      secretConfigured: true,
      secretStatus: "valid",
      secretValidatedAt: iso(-day),
      createdAt: iso(-day * 60),
      iconUrl: mockAvatarUrl,
      config: {},
      supportsOperator: true,
      searchFields: [
        { key: "email", operators: ["eq", "contains"] },
        { key: "subject", operators: ["eq"] },
      ],
    },
    {
      slug: "provider-2",
      displayName: "Example IdP 2",
      protocol: "saml",
      mode: "manual",
      disabled: true,
      ready: false,
      secretConfigured: false,
      secretStatus: "missing",
      secretValidatedAt: null,
      createdAt: iso(-day * 60),
      config: {},
      supportsOperator: true,
      searchFields: [{ key: "email", operators: ["eq"] }],
    },
  ];
}

function groupProviders(): ProviderDescriptorView[] {
  return [
    { slug: "provider-1", displayName: "Example IdP 1" },
    { slug: "provider-2", displayName: "Example IdP 2" },
  ];
}

/* ------------------------------------------------------------ audit log -- */

/**
 * What each generated event records, cycling so that every page mixes
 * factors, failures, events with and without an account, and details with and
 * without a browser or a detail map.
 */
const auditShapes: readonly {
  factor: string;
  event: string;
  detail?: Record<string, unknown>;
}[] = [
  { factor: "webauthn", event: "use" },
  { factor: "password", event: "fail", detail: { reason: "bad_password" } },
  { factor: "session", event: "session_start" },
  {
    factor: "settings",
    event: "update",
    detail: { reason: "maintenance_enabled" },
  },
  {
    factor: "signing_key",
    event: "register",
    detail: { kid: "mock-key", action: "generate" },
  },
  { factor: "totp", event: "factor_locked" },
  { factor: "account", event: "update", detail: { field: "role" } },
  {
    factor: "oidc_client",
    event: "access_denied",
    detail: { clientId: "mock-client-1" },
  },
  { factor: "invitation", event: "enrollment_issued" },
  { factor: "session", event: "sudo_failed" },
  { factor: "personal_access_token", event: "revoke" },
  // A value the dashboard's vocabulary does not know, shown as it arrives.
  { factor: "mock_unknown", event: "mock_event" },
];

/**
 * The generated log, newest first, one event every two hours: the default
 * count spans two days, so the day-long preset shows part of it and the
 * week-long one all of it.
 */
function auditEventsFrom(config: MockConfig): AuditEvent[] {
  const accountCount = clampAdminCount(config.admin.accounts);
  return Array.from(
    { length: clampAdminCount(config.admin.auditEvents) },
    (_, index): AuditEvent => {
      const shape = auditShapes[index % auditShapes.length] ?? {
        factor: "webauthn",
        event: "use",
      };
      // Every fourth event is the system's own, with no account; the rest
      // belong to the directory's accounts.
      const account =
        index % 4 === 3 || accountCount === 0
          ? undefined
          : accountAt(index % accountCount, config);
      return {
        id: 10_000 - index,
        at: iso(-(index * 2 + 0.25) * 3_600_000),
        ...(account === undefined
          ? {}
          : { accountId: account.id, accountUsername: account.username }),
        factor: shape.factor,
        event: shape.event,
        ...(index % 5 === 4 ? {} : { ip: `198.51.100.${(index % 250) + 1}` }),
        ...(index % 2 === 0
          ? {
              userAgent:
                "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) MockBrowser/1.0 Safari/537.36",
            }
          : {}),
        ...(shape.detail ? { detail: shape.detail } : {}),
      };
    },
  );
}

/** The log with `GET /audit-events`' filters applied, as the server would. */
function filteredAuditEvents(
  request: MockRequest,
  config: MockConfig,
): AuditEvent[] {
  const params = new URL(request.url).searchParams;
  const factor = params.get("factor");
  const event = params.get("event");
  const accountId = params.get("accountId");
  const since = params.get("since");
  const until = params.get("until");
  return auditEventsFrom(config).filter(
    (item) =>
      (factor === null || item.factor === factor) &&
      (event === null || item.event === event) &&
      (accountId === null || item.accountId === Number(accountId)) &&
      (since === null || Date.parse(item.at) >= Date.parse(since)) &&
      (until === null || Date.parse(item.at) <= Date.parse(until)),
  );
}

/* --------------------------------------------------------- signing keys -- */

const keyStateLetters = {
  P: "pending",
  A: "active",
  D: "decommissioning",
  R: "retired",
  // Retiring, but retired straight from pending: it never signed.
  X: "decommissioning",
} as const;
type KeyStateLetter = keyof typeof keyStateLetters;

/**
 * Each key's state letter, newest first. A key the stored string does not
 * reach takes its state from its position: the newest is pending, the next
 * signs, the one before that is retiring, and the rest are retired.
 */
function signingKeyStates(config: MockConfig): KeyStateLetter[] {
  return Array.from(
    { length: clampCount(config.admin.signingKeys) },
    (_, index): KeyStateLetter => {
      const stored = config.admin.signingKeyStates[index];
      if (stored !== undefined && stored in keyStateLetters) {
        return stored as KeyStateLetter;
      }
      return index === 0 ? "P" : index === 1 ? "A" : index === 2 ? "D" : "R";
    },
  );
}

/**
 * The key list. A key is numbered from the oldest, so generating one adds a
 * number at the top and leaves every existing key's id where it was.
 */
function signingKeysFrom(config: MockConfig): SigningKey[] {
  const states = signingKeyStates(config);
  return states.map((letter, index): SigningKey => {
    const serial = states.length - index;
    const status = keyStateLetters[letter];
    return {
      kid: `mock${String(serial).padStart(3, "0")}-${"x".repeat(26)}-k${serial}`,
      algorithm: "RS256",
      use: "sig",
      status,
      publicJwk: {
        kty: "RSA",
        kid: `mock-key-${serial}`,
        use: "sig",
        alg: "RS256",
        n: `mock-modulus-${serial}`,
        e: "AQAB",
      },
      ...(letter === "P" || letter === "X"
        ? {}
        : { activatedAt: iso(-day * (index * 90 + 30)) }),
      ...(status === "decommissioning"
        ? {
            decommissionedAt: iso(-day * (index * 90 - 60)),
            retireAfter: iso(day * 2),
          }
        : status === "retired"
          ? {
              decommissionedAt: iso(-day * (index * 90 - 60)),
              retireAfter: iso(-day * (index * 90 - 88)),
            }
          : {}),
    };
  });
}

/** The index of the key a path names, or -1 when there is none. */
function signingKeyIndex(request: MockRequest, config: MockConfig): number {
  const segments = new URL(request.url).pathname.split("/");
  const kid = decodeURIComponent(segments[segments.length - 2] ?? "");
  return signingKeysFrom(config).findIndex((key) => key.kid === kid);
}

function withKeyStates(
  config: MockConfig,
  change: (states: KeyStateLetter[]) => void,
): MockEffect {
  const states = signingKeyStates(config);
  change(states);
  return (draft) => {
    draft.admin.signingKeyStates = states.join("");
  };
}

function readReply(
  request: MockRequest,
  config: MockConfig,
): MockReply | undefined {
  switch (request.schemaPath) {
    case "/api/prohibitorum/config":
      return json(publicConfig(config));
    case "/api/prohibitorum/auth/status":
      return json({ bootstrapped: config.instance.bootstrapped });
    case "/api/prohibitorum/me":
      return guarded(config, () => json(sessionView(config)));
    case "/api/prohibitorum/me/credentials":
      return guarded(config, () =>
        json(credentialViews(config.factors.passkeys)),
      );
    case "/api/prohibitorum/me/factors":
      return guarded(config, () => json(factorsView(config)));
    case "/api/prohibitorum/me/sessions":
      return guarded(config, () => json(sessionList(config.lists.sessions)));
    case "/api/prohibitorum/me/identities":
      return guarded(config, () => json(identityList(config.lists.identities)));
    case "/api/prohibitorum/me/tokens":
      return guarded(config, () => json(tokenList(config.lists.tokens)));
    case "/api/prohibitorum/me/forward-auth-apps":
      return guarded(config, () => json(forwardAuthAppList(config)));
    case "/api/prohibitorum/me/consent":
      return guarded(config, () => json(consentedAppList(config)));
    case "/api/prohibitorum/me/avatar/status":
      return guarded(config, () =>
        json({ pending: config.session.avatarPending }),
      );
    case "/api/prohibitorum/auth/federation":
      return json(providerList(config));
    case "/api/prohibitorum/me/sudo/methods":
      return guarded(config, () => json(sudoMethods(config)));
    case "/api/prohibitorum/me/devices/pair/lookup": {
      const code = new URL(request.url).searchParams.get("code") ?? "";
      return guarded(config, () => json(pairing(code)));
    }

    /* -------------------------------------------------- admin directory -- */

    // The management reads answer with the same account the panel signed in as,
    // so a walkthrough that edits its own profile sees one account rather than
    // two. A member is answered with the whole directory here too: the real
    // server would refuse it, but the guard that keeps a member out of these
    // pages is the role on `/me`, which this fixture also drives.
    case "/api/prohibitorum/accounts": {
      const all = accounts(config);
      const cursor = new URL(request.url).searchParams.get("cursor");
      const start = cursor === null ? 0 : Number(cursor);
      const page = all.slice(start, start + mockPageSize);
      const next = start + page.length;
      return guarded(config, () =>
        json({
          items: page,
          nextCursor: next < all.length ? String(next) : "",
        }),
      );
    }
    case "/api/prohibitorum/accounts/{id}": {
      const id = Number(new URL(request.url).pathname.split("/").pop());
      const found = accounts(config).find((account) => account.id === id);
      return guarded(config, () =>
        found ? json(found) : { kind: "error", status: 404, code: "not_found" },
      );
    }
    case "/api/prohibitorum/accounts/{id}/identities": {
      const id = pathTail(request);
      return guarded(config, () => json(accountIdentities(id - 1)));
    }
    case "/api/prohibitorum/accounts/{id}/credentials": {
      const id = pathTail(request);
      return guarded(config, () => json(accountCredentials(id - 1)));
    }
    case "/api/prohibitorum/accounts/{id}/sessions": {
      const id = pathTail(request);
      return guarded(config, () => json(accountSessions(id - 1)));
    }
    case "/api/prohibitorum/accounts/{id}/tokens": {
      const id = pathTail(request);
      return guarded(config, () => json(accountTokens(id - 1)));
    }
    case "/api/prohibitorum/invitations": {
      const all = Array.from(
        { length: clampAdminCount(config.admin.invitations) },
        (_, index) => invitationAt(index, config),
      );
      const cursor = new URL(request.url).searchParams.get("cursor");
      const start = cursor === null ? 0 : Number(cursor);
      const page = all.slice(start, start + mockPageSize);
      const next = start + page.length;
      return guarded(config, () =>
        json({
          items: page,
          nextCursor: next < all.length ? String(next) : "",
        }),
      );
    }
    case "/api/prohibitorum/identity-providers":
      // A cursor page, like the handler's `contract.Page[...]` envelope — not
      // the bare array `GET /groups` answers with.
      return guarded(config, () =>
        json({ items: identityProviders(), nextCursor: "" }),
      );
    case "/api/prohibitorum/groups":
      return guarded(config, () => json(groupsFrom(config)));
    case "/api/prohibitorum/groups/providers":
      return guarded(config, () => json(groupProviders()));
    case "/api/prohibitorum/groups/{groupId}": {
      const id = pathTail(request);
      const found = groupsFrom(config).find((group) => group.id === id);
      return guarded(config, () =>
        found ? json(found) : { kind: "error", status: 404, code: "not_found" },
      );
    }
    case "/api/prohibitorum/groups/{groupId}/decisions":
      return guarded(config, () =>
        json({ items: groupDecisions(), nextCursor: "" }),
      );
    case "/api/prohibitorum/groups/{groupId}/preview":
      return guarded(config, () => json(groupPreview(config)));
    case "/api/prohibitorum/groups/{groupId}/applications":
      return guarded(config, () =>
        json({
          items: range(clampCount(config.admin.groups)).map((index) => ({
            appId: `mock-client-${index + 1}`,
            kind: index % 2 === 0 ? "oidc" : "saml",
            displayName: `Sample application ${index + 1}`,
            iconUrl: mockAvatarUrl,
          })),
          nextCursor: "",
        }),
      );
    case "/api/prohibitorum/audit-events": {
      const all = filteredAuditEvents(request, config);
      const cursor = new URL(request.url).searchParams.get("cursor");
      const start = cursor === null ? 0 : Number(cursor);
      const page = all.slice(start, start + mockPageSize);
      const next = start + page.length;
      return guarded(config, () =>
        json({
          items: page,
          nextCursor: next < all.length ? String(next) : "",
        }),
      );
    }
    case "/api/prohibitorum/signing-keys": {
      const all = signingKeysFrom(config);
      const cursor = new URL(request.url).searchParams.get("cursor");
      const start = cursor === null ? 0 : Number(cursor);
      const page = all.slice(start, start + mockPageSize);
      const next = start + page.length;
      return guarded(config, () =>
        json({
          items: page,
          nextCursor: next < all.length ? String(next) : "",
        }),
      );
    }
    case "/api/prohibitorum/admin/settings/client-ip":
      return guarded(config, () =>
        json({
          strategy: config.instance.clientIpStrategy,
          header: config.instance.clientIpHeader,
          trustedProxies: config.instance.trustedProxies
            .split("\n")
            .filter((line) => line !== ""),
        }),
      );

    case "/api/prohibitorum/groups/{groupId}/explain/{accountId}": {
      const accountId = pathTail(request);
      return guarded(config, () =>
        json({
          account: {
            id: accountId,
            username: `mock-user-${accountId}`,
            displayName: `Mock User ${accountId}`,
          },
          explanation: {
            path: "$",
            label: "All of",
            result: accountId % 2 === 0,
            children: [
              { path: "$.0", label: "Signed in with a passkey", result: true },
              {
                path: "$.1",
                label: "Provider is Example IdP 1",
                result: false,
              },
            ],
          },
        }),
      );
    }

    default:
      return undefined;
  }
}

/* ----------------------------------------------------------------- writes -- */

function decrement(value: number): number {
  return clampCount(value - 1);
}

function recoveryCodesReply(effect: MockEffect): MockReply {
  return json({ recovery_codes: freshRecoveryCodes() }, effect);
}

function writeReply(
  request: MockRequest,
  config: MockConfig,
): MockReply | undefined {
  const { schemaPath, method, body } = request;
  switch (schemaPath) {
    case "/api/prohibitorum/auth/logout":
      return empty(204, (draft) => {
        draft.session.signedIn = false;
      });

    case "/api/prohibitorum/auth/password/begin":
      return json({ partial_session_token: "mock-partial-session" });

    case "/api/prohibitorum/auth/totp/verify":
      return json({ redirect: "/" }, (draft) => {
        draft.session.signedIn = true;
      });

    case "/api/prohibitorum/auth/recovery-code/verify": {
      const resets = booleanField(body, "reset_authenticator") === true;
      return json(
        resets
          ? { redirect: "/", recovery_codes: freshRecoveryCodes() }
          : { redirect: "/" },
        (draft) => {
          draft.session.signedIn = true;
          if (resets) {
            draft.factors.totpEnrolled = true;
            draft.factors.recoveryCodes = issuedRecoveryCodes;
          }
        },
      );
    }

    case "/api/prohibitorum/me": {
      if (method !== "PUT") return undefined;
      const displayName =
        stringField(body, "displayName") ?? config.session.displayName;
      return json({ ...sessionView(config), displayName }, (draft) => {
        draft.session.displayName = displayName;
      });
    }

    case "/api/prohibitorum/me/credentials/rename":
      return empty();

    case "/api/prohibitorum/me/credentials/delete":
      return empty(204, (draft) => {
        draft.factors.passkeys = decrement(draft.factors.passkeys);
      });

    case "/api/prohibitorum/me/password/set":
      return empty(204, (draft) => {
        draft.factors.passwordSet = true;
      });

    case "/api/prohibitorum/me/totp/verify":
      return recoveryCodesReply((draft) => {
        draft.factors.totpEnrolled = true;
        draft.factors.recoveryCodes = issuedRecoveryCodes;
      });

    case "/api/prohibitorum/me/password-totp/verify":
      return recoveryCodesReply((draft) => {
        draft.factors.passwordSet = true;
        draft.factors.totpEnrolled = true;
        draft.factors.recoveryCodes = issuedRecoveryCodes;
      });

    case "/api/prohibitorum/me/recovery-codes/regenerate":
      return recoveryCodesReply((draft) => {
        draft.factors.recoveryCodes = issuedRecoveryCodes;
      });

    case "/api/prohibitorum/me/auth/revoke-password-totp":
      return empty(204, (draft) => {
        draft.factors.passwordSet = false;
        draft.factors.totpEnrolled = false;
      });

    case "/api/prohibitorum/me/sessions/revoke":
      return empty(204, (draft) => {
        draft.lists.sessions = decrement(draft.lists.sessions);
      });

    case "/api/prohibitorum/me/identities/{id}/unlink":
      return empty(204, (draft) => {
        draft.lists.identities = decrement(draft.lists.identities);
      });

    case "/api/prohibitorum/me/consent/revoke":
      return empty(204, (draft) => {
        draft.lists.consentedApps = decrement(draft.lists.consentedApps);
      });

    case "/api/prohibitorum/me/tokens": {
      if (method !== "POST") return undefined;
      const id = clampCount(config.lists.tokens) + 1;
      const name = stringField(body, "name") ?? `Mock token ${id}`;
      return json(
        {
          token: `phb_mock_${id}_${name}`,
          pat: {
            id,
            name,
            tokenHint: `phb_mock${id}`,
            allApps: booleanField(body, "allApps") ?? true,
            appGrants: grantsField(body),
            createdAt: iso(0),
          },
        },
        (draft) => {
          draft.lists.tokens = clampCount(draft.lists.tokens + 1);
        },
      );
    }

    case "/api/prohibitorum/me/tokens/revoke":
      return empty(204, (draft) => {
        draft.lists.tokens = decrement(draft.lists.tokens);
      });

    case "/api/prohibitorum/me/devices/pair/approve":
    case "/api/prohibitorum/me/devices/pair/cancel":
      return empty();

    case "/api/prohibitorum/me/avatar/selection":
      return empty(204, (draft) => {
        draft.session.avatarPending = false;
      });

    case "/api/prohibitorum/me/avatar":
      return method === "PUT" || method === "DELETE" ? empty() : undefined;

    case "/api/prohibitorum/me/sudo/begin":
      // A passkey assertion cannot come from a fabricated challenge, so only
      // the password-and-code method is answerable here; the passkey method
      // falls through to the unmocked failure.
      return stringField(body, "method") === "password_totp"
        ? empty()
        : undefined;

    case "/api/prohibitorum/me/sudo/complete":
      return empty(204, (draft) => {
        draft.sudo.fresh = true;
      });

    /* ------------------------------------------------- admin directory -- */

    // The directory's writes follow the same rule as the rest of the mock: the
    // reply carries the new state and an effect moves the config, so the read
    // that follows the write agrees with it rather than undoing it.
    case "/api/prohibitorum/accounts/{id}": {
      if (method !== "PUT") return undefined;
      const id = pathTail(request);
      const index = id - 1;
      const current = accountAt(index, config);
      const updated: Account = {
        ...current,
        displayName: stringField(body, "displayName") ?? current.displayName,
        role: stringField(body, "role") ?? current.role,
        updatedAt: iso(0),
      };
      if (index === 0) {
        return json(updated, (draft) => {
          draft.session.displayName = updated.displayName;
          draft.session.role = updated.role === "admin" ? "admin" : "member";
        });
      }
      return json(updated);
    }

    case "/api/prohibitorum/accounts/set-disabled": {
      const id = Number(field(body, "id"));
      const disabled = booleanField(body, "disabled") ?? false;
      return json({ ...accountAt(id - 1, config), disabled });
    }

    case "/api/prohibitorum/accounts/delete":
      return json({}, (draft) => {
        draft.admin.accounts = Math.max(0, draft.admin.accounts - 1);
      });

    case "/api/prohibitorum/accounts/reissue-enrollment": {
      const id = Number(field(body, "id"));
      return json({
        url: `http://localhost:8080/enroll/mock-reissue-${id}`,
        expiresAt: iso(day),
      });
    }

    case "/api/prohibitorum/accounts/credentials/delete":
      return empty();

    case "/api/prohibitorum/accounts/tokens/revoke":
      return empty();

    case "/api/prohibitorum/accounts/{id}/sessions/revoke":
      return empty();

    case "/api/prohibitorum/accounts/revoke-sessions":
      return json({ revoked: 1 });

    case "/api/prohibitorum/invitations": {
      if (method !== "POST") return undefined;
      const id = clampAdminCount(config.admin.invitations) + 1;
      return json(
        {
          url: `http://localhost:8080/enroll/mock-invitation-${id}`,
          expiresAt: iso(day * 7),
          groupIds: (field(body, "groupIds") as number[] | undefined) ?? null,
          username: stringField(body, "username") ?? "",
        },
        (draft) => {
          draft.admin.invitations = clampAdminCount(
            draft.admin.invitations + 1,
          );
        },
      );
    }

    case "/api/prohibitorum/invitations/revoke":
      return json({}, (draft) => {
        draft.admin.invitations = Math.max(0, draft.admin.invitations - 1);
      });

    case "/api/prohibitorum/groups": {
      if (method !== "POST") return undefined;
      return json(createdGroup(body, config), (draft) => {
        draft.admin.groups = clampAdminCount(draft.admin.groups + 1);
      });
    }

    case "/api/prohibitorum/groups/{groupId}": {
      if (method !== "PUT") return undefined;
      const id = pathTail(request);
      const current = groupsFrom(config).find((group) => group.id === id);
      return json({
        ...(current ?? createdGroup(body, config)),
        id,
        slug: stringField(body, "slug") ?? current?.slug ?? `group-${id}`,
        displayName:
          stringField(body, "displayName") ?? current?.displayName ?? "",
        description: stringField(body, "description") ?? "",
      });
    }

    case "/api/prohibitorum/groups/{groupId}/delete":
      return empty(204, (draft) => {
        draft.admin.groups = Math.max(0, draft.admin.groups - 1);
      });

    case "/api/prohibitorum/groups/{groupId}/decisions": {
      const accountId = Number(field(body, "accountId"));
      return json({
        account: {
          id: accountId,
          username: `mock-user-${accountId}`,
          displayName: `Mock User ${accountId}`,
        },
        effect: stringField(body, "effect") ?? "allow",
        updatedAt: iso(0),
      });
    }

    case "/api/prohibitorum/groups/{groupId}/decisions/clear":
      return empty();

    /* ------------------------------------------------ instance settings -- */

    case "/api/prohibitorum/admin/settings": {
      if (method !== "PUT") return undefined;
      const name = stringField(body, "instanceName") ?? "";
      return empty(204, (draft) => {
        draft.instance.name = name;
      });
    }

    case "/api/prohibitorum/admin/settings/maintenance": {
      const on = booleanField(body, "maintenanceMode") ?? false;
      const message = stringField(body, "maintenanceMessage") ?? "";
      return empty(204, (draft) => {
        draft.instance.maintenance = on;
        draft.instance.maintenanceMessage = message;
      });
    }

    case "/api/prohibitorum/admin/settings/icon":
      if (method !== "PUT" && method !== "DELETE") return undefined;
      return empty(204, (draft) => {
        draft.instance.customIcon = method === "PUT";
        draft.instance.imageRevision += 1;
      });

    case "/api/prohibitorum/admin/settings/background":
      if (method !== "PUT" && method !== "DELETE") return undefined;
      return empty(204, (draft) => {
        draft.instance.customBackground = method === "PUT";
        draft.instance.imageRevision += 1;
      });

    case "/api/prohibitorum/admin/settings/client-ip": {
      if (method !== "PUT") return undefined;
      const strategy = stringField(body, "strategy");
      const proxies = field(body, "trustedProxies");
      return empty(204, (draft) => {
        if (
          strategy === "direct" ||
          strategy === "forwarded" ||
          strategy === "header"
        ) {
          draft.instance.clientIpStrategy = strategy;
        }
        draft.instance.clientIpHeader = stringField(body, "header") ?? "";
        draft.instance.trustedProxies = Array.isArray(proxies)
          ? proxies.filter((line) => typeof line === "string").join("\n")
          : "";
      });
    }

    /* ----------------------------------------------------- signing keys -- */

    case "/api/prohibitorum/signing-keys/generate": {
      const serial = clampCount(config.admin.signingKeys) + 1;
      if (serial > mockListMax) {
        return { kind: "error", status: 500, code: "server_error" };
      }
      const states = signingKeyStates(config);
      const generated = signingKeysFrom({
        ...config,
        admin: {
          ...config.admin,
          signingKeys: serial,
          signingKeyStates: `P${states.join("")}`,
        },
      })[0];
      return {
        kind: "json",
        status: 201,
        body: generated,
        effect: (draft) => {
          draft.admin.signingKeys = serial;
          draft.admin.signingKeyStates = `P${states.join("")}`;
        },
      };
    }

    case "/api/prohibitorum/signing-keys/{kid}/activate": {
      // Like the handler: only a pending key, and anything else is "not found".
      const index = signingKeyIndex(request, config);
      if (signingKeyStates(config)[index] !== "P") {
        return { kind: "error", status: 404, code: "credential_not_found" };
      }
      const effect = withKeyStates(config, (states) => {
        states.forEach((state, position) => {
          if (state === "A") states[position] = "D";
        });
        states[index] = "A";
      });
      const next = structuredClone(config);
      effect(next);
      return json(signingKeysFrom(next)[index], effect);
    }

    case "/api/prohibitorum/signing-keys/{kid}/retire": {
      const index = signingKeyIndex(request, config);
      const state = signingKeyStates(config)[index];
      if (state === "A") {
        return {
          kind: "error",
          status: 409,
          code: "active_key_no_replacement",
        };
      }
      if (state !== "P" && state !== "D" && state !== "X") {
        return { kind: "error", status: 404, code: "credential_not_found" };
      }
      const effect = withKeyStates(config, (states) => {
        if (state === "P") states[index] = "X";
      });
      const next = structuredClone(config);
      effect(next);
      return json(signingKeysFrom(next)[index], effect);
    }

    case "/api/prohibitorum/groups/rule-preview": {
      // The draft is what is being previewed, so the answer is derived from it
      // rather than from the saved groups: a rule with no conditions matches
      // nobody, which is what the editor should show for an empty draft.
      const condition = field(body, "condition");
      const matches = hasLeaf(condition)
        ? groupPreview(config)
        : emptyPreview();
      return json(matches);
    }

    default:
      return undefined;
  }
}

/** The group a create write should answer with, taken from what it carried. */
function createdGroup(body: unknown, config: MockConfig): AppGroupView {
  const id = clampAdminCount(config.admin.groups) + 1;
  const kind = stringField(body, "kind") ?? "manual";
  const rule = field(body, "rule") as AppAccessRule | undefined;
  return {
    id,
    kind,
    slug: stringField(body, "slug") ?? `group-${id}`,
    displayName: stringField(body, "displayName") ?? `Group ${id}`,
    description: stringField(body, "description") ?? "",
    exposedToDownstream: booleanField(body, "exposedToDownstream") ?? false,
    ...(kind === "rule" && rule ? { rule } : {}),
    applicationCount: 0,
  };
}

/** Whether a rule draft carries at least one leaf condition. */
function hasLeaf(condition: unknown): boolean {
  if (typeof condition !== "object" || condition === null) return false;
  const node = condition as { fact?: unknown; children?: unknown[] };
  if (typeof node.fact === "string") return true;
  return Array.isArray(node.children) && node.children.some(hasLeaf);
}

function emptyPreview(): RulePreviewPageView {
  return { items: [], matchedCount: 0, nextCursor: "" };
}

/**
 * The reply for one request, or `undefined` when the mock is not answering it.
 *
 * The mock owns a whole verb rather than a list of paths: while reads are
 * mocked every GET is answered, and once `writes` is on so is every other
 * method. A path with no fixture fails with `mock_unmocked` instead of
 * reaching the server, so a mocked console never mixes fabricated answers
 * with real ones.
 */
export function buildMockReply(
  request: MockRequest,
  config: MockConfig,
): MockReply | undefined {
  if (request.method === "GET") {
    return readReply(request, config) ?? unmocked(request);
  }
  if (!config.writes) return undefined;
  return writeReply(request, config) ?? unmocked(request);
}
