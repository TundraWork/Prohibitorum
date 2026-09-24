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

/* ------------------------------------------------------------------ reads -- */

function publicConfig(config: MockConfig): PublicConfig {
  return {
    instanceName: "Prohibitorum (mock)",
    hasCustomIcon: false,
    iconUrl: "",
    iconEtag: "",
    maintenanceMode: config.instance.maintenance,
    maintenanceMessage: config.instance.maintenance
      ? "Scheduled maintenance is in progress."
      : "",
    hasCustomBackground: false,
    backgroundUrl: "",
    backgroundEtag: "",
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
