import type { components } from "@/api/generated/schema";
import type {
  AppAccessRule,
  AppAccessWorkspace,
  AppGroupView,
  ManualDecisionView,
  PageManualDecisionView,
  ProviderDescriptorView,
  RulePreviewPageView,
} from "@/api/raw-admin-paths";
import type {
  ConsentAccount,
  ConsentRequest,
  DevicePairing,
  FederationConfirm,
  FederationFlow,
  LoginAppearance,
  LoginImage,
  PublicConfig,
  RequireIdentityRequest,
  SamlConsentRequest,
  SudoMethod,
  Wallpaper,
} from "@/api/raw-paths";
import {
  clampAdminCount,
  clampCount,
  clampDecisionCount,
  clampDecisionsPageSize,
  clampEnrollmentProviders,
  clampPairingExpiry,
  clampRuleAccounts,
  clampRuleMatchEvery,
  type MockConfig,
  mockAdminListMax,
  mockListMax,
  mockLoginImagesMax,
  mockPageSize,
} from "@/devtools/mock/model";

type Credential = components["schemas"]["CredentialView"];
type Session = components["schemas"]["SessionView"];
type MyAvatar = components["schemas"]["MyAvatarView"];
type MyAvatarSource = components["schemas"]["MyAvatarSourceView"];
type SessionListItem = components["schemas"]["SessionListItem"];
type Identity = components["schemas"]["AccountIdentityView"];
type Token = components["schemas"]["PersonalAccessTokenView"];
type ForwardAuthApp = components["schemas"]["MyForwardAuthApp"];
type ConsentedApp = components["schemas"]["ConsentedApp"];
type Provider = components["schemas"]["FederationProvider"];
type EnrollmentPreview = components["schemas"]["EnrollmentPreview"];
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

/**
 * Indexes for the application directory, which pages by cursor and so reaches
 * the management directory's ceiling rather than the short lists'.
 */
function adminRange(count: number): number[] {
  return Array.from({ length: clampAdminCount(count) }, (_, index) => index);
}

/**
 * What a list of one application kind answers with: every row for an admin,
 * and for a delegated manager (a member) only the first rows they are assigned
 * to, as many as the panel says.
 */
function assignedTo<T>(
  config: MockConfig,
  kind: keyof MockConfig["session"]["managedApps"],
  rows: T[],
): T[] {
  if (config.session.role === "admin") return rows;
  return rows.slice(0, clampAdminCount(config.session.managedApps[kind]));
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

function numberField(source: unknown, key: string): number | undefined {
  const value = field(source, key);
  return typeof value === "number" ? value : undefined;
}

function booleanField(source: unknown, key: string): boolean | undefined {
  const value = field(source, key);
  return typeof value === "boolean" ? value : undefined;
}

function stringListField(source: unknown, key: string): string[] {
  const value = field(source, key);
  return Array.isArray(value)
    ? value.filter((entry): entry is string => typeof entry === "string")
    : [];
}

const tokenAccessLevels: Token["access"][] = [
  "selected_apps",
  "all_apps",
  "full",
  "sudo",
];

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

/**
 * A soft gradient standing in for uploaded sign-in image `id`. Its colours move
 * with the id and with every image write, so a carousel visibly changes and a
 * new upload is a new picture.
 */
function mockLoginImageUrl(config: MockConfig, id: number): string {
  const hue = (config.instance.imageRevision * 67 + id * 53 + 180) % 360;
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
    loginAppearance: config.instance.loginAppearance,
    loginImages: mockLoginImages(config),
    totp: {
      issuer: "Prohibitorum (mock)",
      algorithm: "SHA1",
      digits: 6,
      period: 30,
    },
  };
}

/** The uploaded images are ids 1…n; removing one renumbers the rest. */
function mockLoginImages(config: MockConfig): LoginImage[] {
  const revision = config.instance.imageRevision;
  return Array.from({ length: config.instance.loginImageCount }, (_, i) => ({
    id: i + 1,
    url: mockLoginImageUrl(config, i + 1),
    etag: `login-image-${i + 1}-${revision}`,
  }));
}

/**
 * Bing's picture of the day or a batch of five Unsplash photos, as
 * `/branding/wallpaper` would describe them, with `data:` pictures so nothing
 * is fetched. The Unsplash photos differ in hue and photographer, so the
 * carousel and its credit can be followed. `caption` leaves Bing's title and
 * copyright out, as the public endpoint does when the caption is hidden.
 */
function mockWallpaper(
  source: "bing" | "unsplash",
  detail: string,
  caption: boolean,
): Wallpaper {
  if (source === "unsplash") {
    return {
      source,
      pictures: Array.from({ length: 5 }, (_, i) => ({
        imageUrl: mockWallpaperUrl(150 + i * 50),
        photographer:
          detail === ""
            ? `Mock Photographer ${i + 1}`
            : `Mock ${detail} ${i + 1}`,
        photographerUrl:
          "https://unsplash.com/?utm_source=prohibitorum&utm_medium=referral",
        photoUrl:
          "https://unsplash.com/?utm_source=prohibitorum&utm_medium=referral",
      })),
    };
  }
  return {
    source,
    pictures: [
      {
        imageUrl: mockWallpaperUrl(28),
        ...(caption
          ? {
              title: `Mock picture of the day (${detail})`,
              copyright:
                "A ridge at dusk, somewhere quiet (© Mock Photographer)",
              copyrightUrl: "https://www.bing.com/",
            }
          : {}),
      },
    ],
  };
}

function mockWallpaperUrl(hue: number): string {
  return svgUrl(
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1600 900" preserveAspectRatio="xMidYMid slice"><defs><linearGradient id="g" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="hsl(${hue + 180} 45% 62%)"/><stop offset=".62" stop-color="hsl(${hue} 70% 72%)"/><stop offset="1" stop-color="hsl(${hue - 20} 35% 28%)"/></linearGradient></defs><rect width="1600" height="900" fill="url(#g)"/><path d="M0 700 L380 470 L640 640 L980 380 L1600 720 V900 H0z" fill="hsl(${hue - 20} 30% 20%)" fill-opacity=".7"/></svg>`,
  );
}

/** A square portrait in one hue, so each stored picture looks different. */
function avatarPictureUrl(hue: number): string {
  return svgUrl(
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" fill="hsl(${hue} 42% 42%)"/><circle cx="32" cy="25" r="11" fill="hsl(${hue} 50% 92%)"/><path d="M10 64a22 22 0 0 1 44 0z" fill="hsl(${hue} 50% 92%)"/></svg>`,
  );
}

/** The stored pictures as the server lists them: the upload, then each provider's. */
function avatarSources(config: MockConfig): MyAvatarSource[] {
  const upstreams = range(config.avatar.upstreams).map((index) => ({
    source: `upstream:provider-${index + 1}`,
    label: `Example IdP ${index + 1}`,
    url: avatarPictureUrl(30 + index * 110),
  }));
  return config.avatar.upload
    ? [{ source: "user", url: avatarPictureUrl(200) }, ...upstreams]
    : upstreams;
}

/** The picture in use; a source that is no longer stored reads as none. */
function activeAvatar(config: MockConfig): MyAvatarSource | undefined {
  return avatarSources(config).find(
    (entry) => entry.source === config.avatar.active,
  );
}

export function myAvatarView(config: MockConfig): MyAvatar {
  return {
    activeSource: activeAvatar(config)?.source ?? "none",
    sources: avatarSources(config),
  };
}

function sessionView(config: MockConfig): Session {
  const active = activeAvatar(config);
  return {
    id: 1,
    username: config.session.username,
    // The directory's first row is this account, with the same subject.
    oidcSubject: "mock-oidc-1",
    displayName: config.session.displayName,
    role: config.session.role,
    avatarSource: active?.source ?? "none",
    ...(active ? { avatarUrl: active.url } : {}),
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

/**
 * Real User-Agents, so a walkthrough sees what the console reads out of them:
 * a browser and a system by name, and an icon for the kind of device.
 */
const userAgents = {
  firefoxLinux:
    "Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0",
  safariIphone:
    "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1",
  chromeWindows:
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36",
  safariIpad:
    "Mozilla/5.0 (iPad; CPU OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1",
};

/** The address the current session reports, which the pairing can share. */
const currentSessionIp = "192.0.2.1";

function sessionList(count: number): SessionListItem[] {
  const agents = [
    userAgents.firefoxLinux,
    userAgents.safariIphone,
    userAgents.chromeWindows,
    userAgents.safariIpad,
  ];
  return range(count).map((index) => ({
    id: `mock-session-${index + 1}`,
    isCurrent: index === 0,
    issuedAt: iso(-day * (index + 1)),
    expiresAt: iso(day * (30 - index)),
    lastSeenIp: index === 0 ? currentSessionIp : `192.0.2.${index + 1}`,
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

/**
 * A plaintext token shaped like the server's: its fixed prefix, then 32 random
 * bytes as unpadded base64url, 43 characters.
 */
function mockTokenPlaintext(): string {
  const alphabet =
    "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
  const bytes = crypto.getRandomValues(new Uint8Array(43));
  return `prohibitorum_pat_${Array.from(bytes, (byte) => alphabet[byte % 64]).join("")}`;
}

/** The list's hint for a token, as the server writes it: prefix, then the last four. */
function mockTokenHint(last: string): string {
  return `prohibitorum_pat_…${last.slice(-4)}`;
}

/** Tokens cycle through the four access levels, so a list shows each one. */
function tokenList(count: number): Token[] {
  return range(count).map((index) => {
    const access = tokenAccessLevels[
      index % tokenAccessLevels.length
    ] as Token["access"];
    return {
      id: index + 1,
      name: `Mock token ${index + 1}`,
      tokenHint: mockTokenHint(`m${String(index + 1).padStart(3, "0")}`),
      access,
      apps:
        access === "selected_apps"
          ? [
              {
                clientId: "forward-auth-1",
                displayName: "Protected service 1",
              },
              {
                clientId: "forward-auth-2",
                displayName: "Protected service 2",
              },
            ]
          : [],
      createdAt: iso(-day * (index + 2)),
      expiresAt: iso(day * 90),
      ...(index === 0 ? { lastUsedAt: iso(-day) } : {}),
    };
  });
}

function forwardAuthAppList(config: MockConfig): ForwardAuthApp[] {
  return range(config.lists.forwardAuthApps).map((index) => ({
    clientId: `forward-auth-${index + 1}`,
    displayName: `Protected service ${index + 1}`,
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
    // Every fourth entry has sign-in off: the login page must drop it while
    // the linked-identities page still lists it.
    allowSignIn: index % 4 !== 3,
  }));
}

/* --------------------------------------------------------- public flows -- */

/** The scopes the incremental consent reports as allowed before. */
const previouslyGranted = ["openid", "profile", "email"];

/** What a SAML service receives, in the order the attributes are configured. */
const samlAttributeLabels = [
  "显示名称",
  "邮箱地址",
  "用户组",
  "用户名",
  "部门",
  "工号",
  "职位",
  "办公地点",
  "电话",
  "语言",
];

function consentAccount(config: MockConfig): ConsentAccount {
  return { displayName: config.session.displayName, avatarUrl: mockAvatarUrl };
}

function appLogoUrl(fill: string, letter: string): string {
  return svgUrl(
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="12" fill="${fill}"/><text x="32" y="42" text-anchor="middle" font-family="sans-serif" font-size="28" font-weight="600" fill="#fff">${letter}</text></svg>`,
  );
}

/**
 * The server names the provider of each `identity:<slug>` scope it knows; a
 * slug no provider has is left out, and the page shows it as written.
 */
function consentProviders(
  config: MockConfig,
  scopes: readonly string[],
): Pick<ConsentRequest, "providers"> {
  const names = new Map(
    providerList(config).map((provider) => [
      provider.slug,
      provider.displayName,
    ]),
  );
  const providers: Record<string, string> = {};
  for (const scope of scopes) {
    const slug = scope.startsWith("identity:")
      ? scope.slice("identity:".length)
      : "";
    const name = names.get(slug);
    if (name !== undefined) providers[slug] = name;
  }
  return Object.keys(providers).length > 0 ? { providers } : {};
}

export function consentRequest(config: MockConfig): ConsentRequest {
  const consent = config.publicFlows.consent;
  const scopes = consent.scopes.split(/\s+/).filter((scope) => scope !== "");
  return {
    client: {
      clientId: "wiki",
      displayName: "Wiki",
      ...(consent.logo ? { logoUri: appLogoUrl("#3b6e4f", "W") } : {}),
      ...(consent.policy
        ? { policyUri: "https://wiki.example.test/privacy" }
        : {}),
      ...(consent.terms ? { tosUri: "https://wiki.example.test/terms" } : {}),
    },
    account: consentAccount(config),
    scopes,
    ...consentProviders(config, scopes),
    // The server reports only the requested scopes that were allowed before.
    ...(consent.grant === "incremental"
      ? {
          alreadyGranted: scopes.filter((scope) =>
            previouslyGranted.includes(scope),
          ),
        }
      : {}),
  };
}

export function samlConsentRequest(config: MockConfig): SamlConsentRequest {
  const consent = config.publicFlows.consent;
  return {
    sp: {
      id: "nextcloud",
      displayName: "Nextcloud",
      ...(consent.logo ? { logoUri: appLogoUrl("#0b6fb8", "N") } : {}),
    },
    account: consentAccount(config),
    attributes: samlAttributeLabels.slice(
      0,
      clampCount(consent.samlAttributes),
    ),
  };
}

function invalidConsentTicket(): MockReply {
  return { kind: "error", status: 400, code: "invalid_consent_ticket" };
}

/**
 * The request a require-identity ticket names. Both addresses stay on this
 * site, as the mock has no application to return to.
 */
export function requireIdentityRequest(
  config: MockConfig,
): RequireIdentityRequest {
  const { state, providers } = config.publicFlows.requireIdentity;
  return {
    client: { displayName: "Wiki", iconUrl: appLogoUrl("#3b6e4f", "W") },
    redirectUri: "/",
    entryUrl: "/",
    mismatch: state === "mismatch",
    satisfied: state === "satisfied",
    providers: range(Math.max(1, providers)).map((index) => ({
      slug: `provider-${index + 1}`,
      displayName: `Example IdP ${index + 1}`,
    })),
  };
}

/** A consent read: signed in, and a ticket the panel says is still good. */
function consentReply(config: MockConfig, body: () => unknown): MockReply {
  return guarded(config, () =>
    config.publicFlows.consent.ticketValid
      ? json(body())
      : invalidConsentTicket(),
  );
}

/**
 * Where an allowed OIDC consent resumes: the page's `return_to`, which the
 * server validates the same way. The mock has no authorization endpoint to
 * resume, so anything that is not a path on this site goes home.
 */
function consentApproveTarget(url: string): string {
  const target = new URL(url).searchParams.get("return_to") ?? "";
  return target.startsWith("/") &&
    !target.startsWith("//") &&
    !target.startsWith("/\\")
    ? target
    : "/";
}

/** The providers an enrollment preview offers, the first of them the bound one. */
const enrollmentProviderNames: readonly [string, string][] = [
  ["GitLab", "oidc"],
  ["VRChat", "vrchat"],
  ["Steam", "steam"],
];

function enrollmentProviders(config: MockConfig): Provider[] {
  return enrollmentProviderNames
    .slice(0, clampEnrollmentProviders(config.publicFlows.enrollment.providers))
    .map(([displayName, protocol]) => ({
      slug: displayName.toLowerCase(),
      displayName,
      protocol,
      allowSignIn: true,
    }));
}

/**
 * What any enrollment token previews. The administrator's first link only
 * ever offers a passkey, as the server's does, and a reset names the account
 * the panel is signed in as.
 */
export function enrollmentPreview(config: MockConfig): EnrollmentPreview {
  const enrollment = config.publicFlows.enrollment;
  const bootstrap = enrollment.intent === "bootstrap";
  const providers = bootstrap ? [] : enrollmentProviders(config);
  const bound = enrollment.bound ? providers[0]?.slug : undefined;
  const verified =
    enrollment.intent === "invite" &&
    enrollment.verified &&
    providers[0] !== undefined
      ? {
          providerSlug: providers[0].slug,
          providerDisplayName: providers[0].displayName,
          displayName: "Alice Liddell",
          ...(enrollment.bound ? {} : { avatarUrl: mockAvatarUrl }),
        }
      : undefined;
  return {
    intent: enrollment.intent,
    ...(enrollment.fixedUsername && enrollment.intent !== "reset"
      ? { username: "alice" }
      : {}),
    ...(enrollment.intent === "reset"
      ? {
          target: {
            username: config.session.username,
            displayName: config.session.displayName,
          },
        }
      : {}),
    expiresAt: iso(day * 5),
    ...(enrollment.intent === "invite" && enrollment.requireLocal
      ? { requireLocalCredential: true }
      : {}),
    ...(verified ? { verifiedIdentity: verified } : {}),
    allowedMethods:
      enrollment.passwordTotp && !bootstrap
        ? ["passkey", "password_totp"]
        : ["passkey"],
    ...(bound && !verified ? { expectedUpstreamIdpSlug: bound } : {}),
    // The server leaves the key out when there is nothing to offer.
    ...(providers.length > 0 ? { providers } : {}),
  };
}

function expiredEnrollment(): MockReply {
  return { kind: "error", status: 410, code: "enrollment_expired" };
}

/**
 * The password-and-authenticator enrollment: it signs the new account in and
 * hands out its first recovery codes. An invitation bound to a provider is
 * refused the way the server refuses it, naming the provider.
 */
function enrollPasswordTotp(config: MockConfig, body: unknown): MockReply {
  if (!config.publicFlows.enrollment.valid) return expiredEnrollment();
  const preview = enrollmentPreview(config);
  const bound = preview.providers?.find(
    (provider) => provider.slug === preview.expectedUpstreamIdpSlug,
  );
  if (bound) {
    return {
      kind: "error",
      status: 400,
      code: "enrollment_federation_required",
      details: { federationName: bound.displayName },
    };
  }
  const username =
    preview.target?.username ??
    preview.username ??
    stringField(body, "username") ??
    config.session.username;
  const displayName =
    preview.target?.displayName ??
    stringField(body, "displayName") ??
    config.session.displayName;
  return json(
    {
      session: { ...sessionView(config), username, displayName },
      recoveryCodes: freshRecoveryCodes(),
    },
    (draft) => {
      draft.session.signedIn = true;
      draft.session.username = username;
      draft.session.displayName = displayName;
      draft.factors.passwordSet = true;
      draft.factors.totpEnrolled = true;
      draft.factors.recoveryCodes = issuedRecoveryCodes;
    },
  );
}

function expiredSignIn(): MockReply {
  return { kind: "error", status: 401, code: "federation_state_invalid" };
}

export function federationConfirm(config: MockConfig): FederationConfirm {
  return {
    idpDisplayName: "GitLab",
    displayName: "Alice Liddell",
    username: "alice",
    email: "alice@example.com",
    ...(config.publicFlows.welcome.avatar ? { avatarUrl: mockAvatarUrl } : {}),
  };
}

/** A VRChat verification as the flow's own step and intent leave it. */
export function federationFlow(
  config: MockConfig,
  origin: string,
): FederationFlow {
  const flow = config.publicFlows.flow;
  const proof = flow.step === "proof";
  return {
    provider: { slug: "vrchat", displayName: "VRChat", protocol: "vrchat" },
    intent: flow.intent,
    step: flow.step,
    ...(proof
      ? {
          profileUrl:
            "https://vrchat.com/home/user/usr_3f2a1b9c-4d5e-6f70-8a9b-0c1d2e3f4a5b",
          proofUrl: `${origin}/verify/vrchat/mock-proof`,
        }
      : {}),
    requiresLocalUsername: proof && flow.requiresLocalUsername,
    expiresAt: iso(15 * 60_000),
  };
}

/** Where a verified flow goes: home, Security for a link, or back to the invite. */
function flowTarget(config: MockConfig) {
  const intent = config.publicFlows.flow.intent;
  if (intent === "link") return "/security";
  if (intent === "login") return "/";
  // A verify-then-create invite returns to the enrollment page; without the
  // local-credential requirement the account exists and the flow lands home.
  return config.publicFlows.enrollment.requireLocal
    ? "/enroll/mock-invitation"
    : "/";
}

function verifyFlow(config: MockConfig, body: unknown): MockReply {
  const flow = config.publicFlows.flow;
  if (flow.step !== "proof") {
    return { kind: "error", status: 409, code: "federation_action_invalid" };
  }
  if (flow.proofMissing) {
    return { kind: "error", status: 409, code: "vrchat_proof_missing" };
  }
  if (flow.requiresLocalUsername && !stringField(body, "localUsername")) {
    return { kind: "error", status: 409, code: "local_username_required" };
  }
  return json({ redirect: flowTarget(config) }, (draft) => {
    if (flow.intent === "login") draft.session.signedIn = true;
    // The next walkthrough starts from the beginning of a flow again.
    draft.publicFlows.flow.step = "identify";
  });
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

function pairing(code: string, config: MockConfig): DevicePairing {
  const expiresIn = clampPairingExpiry(config.pairing.expiresInSeconds);
  return {
    pairingId: `mock-pairing-${code}`,
    displayCode: code,
    initiatorUa: userAgents.chromeWindows,
    initiatorIp: config.pairing.sameNetwork ? currentSessionIp : "198.51.100.7",
    createdAt: iso(-60_000),
    expiresAt: iso(expiresIn * 1000),
    alreadyBound: config.pairing.alreadyBound,
  };
}

/**
 * The pairing `/pair` started, as the mock remembers it between the page's
 * reads: its id and code, when it runs out, and how many times its status has
 * been read. Starting a pairing replaces it, and completing one clears it.
 */
let startedPairing:
  | { id: string; code: string; expiresAt: number; reads: number }
  | undefined;
let pairingsStarted = 0;

const pairingCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789";

function startPairing(config: MockConfig): MockReply {
  pairingsStarted += 1;
  const code = Array.from(
    { length: 8 },
    (_, index) =>
      pairingCodeAlphabet[
        (pairingsStarted * 11 + index * 7) % pairingCodeAlphabet.length
      ] ?? "A",
  ).join("");
  const expiresIn = clampPairingExpiry(
    config.publicFlows.pairing.expiresInSeconds,
  );
  const expiresAt = Date.now() + expiresIn * 1000;
  startedPairing = {
    id: `mock-new-device-${pairingsStarted}`,
    code,
    expiresAt,
    reads: 0,
  };
  return json({
    pairingId: startedPairing.id,
    code,
    displayCode: `${code.slice(0, 4)}-${code.slice(4)}`,
    expiresAt: new Date(expiresAt).toISOString(),
  });
}

/** The started pairing `id` names, while it is still good. */
function livePairing(id: string | null | undefined) {
  if (startedPairing === undefined || startedPairing.id !== id) return;
  if (Date.now() >= startedPairing.expiresAt) return;
  return startedPairing;
}

/** Pending until the configured read, then approved; anything else has expired. */
function pairingStatus(request: MockRequest, config: MockConfig): MockReply {
  const live = livePairing(new URL(request.url).searchParams.get("id"));
  if (live === undefined) return json({ status: "expired" });
  live.reads += 1;
  const after = clampCount(config.publicFlows.pairing.approveAfterPolls);
  return json({
    status: after > 0 && live.reads >= after ? "approved" : "pending",
    expiresAt: new Date(live.expiresAt).toISOString(),
  });
}

function completePairing(request: MockRequest, config: MockConfig): MockReply {
  const live = livePairing(stringField(request.body, "pairingId"));
  if (live === undefined) {
    return { kind: "error", status: 410, code: "pairing_expired" };
  }
  const after = clampCount(config.publicFlows.pairing.approveAfterPolls);
  if (after === 0 || live.reads < after) {
    return { kind: "error", status: 428, code: "pairing_not_approved" };
  }
  startedPairing = undefined;
  const returnTo = new URL(request.url).searchParams.get("return_to");
  return json(
    {
      session: sessionView(config),
      redirect: returnTo?.startsWith("/") ? returnTo : "/",
    },
    (draft) => {
      draft.session.signedIn = true;
    },
  );
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

/** Accounts with an even id have none; the others have one of each level. */
function accountTokens(index: number): Token[] {
  return tokenList((index % 2) * 4).map((token, position) => ({
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
    // One invitation in three carries a verified snapshot, so the provider
    // delete confirmation can show a nonzero pending count.
    requireLocalCredential: index % 3 === 0,
  };
}

/**
 * The user-group directory. One manual group and one rule group are always
 * present, because the two edit shapes are the point of the page: a walkthrough
 * that only ever sees manual groups never reaches the rule editor.
 */
function groupsFrom(config: MockConfig): AppGroupView[] {
  if (config.admin.groupsNone) return [];
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

/** The directory row as the short account reference other records carry. */
function accountSummary({ id, username, displayName }: Account) {
  return { id, username, displayName };
}

/**
 * One page of the per-account allow/deny rows a manual group carries, paged by
 * an offset cursor like the server's opaque one. Not a panel control per
 * group: the decisions belong to one group rather than to the instance, so
 * every manual group shows the same rows.
 */
function groupDecisions(
  config: MockConfig,
  cursor?: string,
): PageManualDecisionView {
  const total = clampDecisionCount(config.admin.groupDecisions);
  const size = clampDecisionsPageSize(config.admin.groupDecisionsPageSize);
  const start = Math.min(total, Number(cursor) || 0);
  const end = Math.min(total, start + size);
  return {
    items: Array.from(
      { length: end - start },
      (_, offset): ManualDecisionView => {
        const index = start + offset;
        return {
          account: accountSummary(accountAt(index, config)),
          effect: index % 3 === 2 ? "deny" : "allow",
          updatedAt: iso(-day * (index + 1)),
        };
      },
    ),
    nextCursor: end < total ? String(end) : "",
  };
}

/** The server's default page size for a preview. */
const previewPageSize = 50;
/** The server clamps a larger limit to this. */
const previewPageSizeMax = 100;

/**
 * One page of a preview. Every active account is evaluated, so `matchedCount`
 * is the whole total and the cursor (an offset here) walks all of them, each
 * flagged matched or not, as the server's does.
 */
function groupPreview(
  config: MockConfig,
  { limit, cursor }: { limit?: number; cursor?: string } = {},
): RulePreviewPageView {
  const size =
    limit !== undefined && limit > 0
      ? Math.min(limit, previewPageSizeMax)
      : previewPageSize;
  const total = clampRuleAccounts(config.admin.ruleAccounts);
  const every = clampRuleMatchEvery(config.admin.ruleMatchEvery);
  const start = Math.min(total, Number(cursor) || 0);
  const end = Math.min(total, start + size);
  const items = Array.from({ length: end - start }, (_, offset) => {
    const index = start + offset;
    return {
      account: {
        id: index + 1,
        username: `mock-user-${index + 1}`,
        displayName: `Mock User ${index + 1}`,
      },
      matched: (index + 1) % every === 0,
    };
  });
  return {
    items,
    matchedCount: Math.floor(total / every),
    nextCursor: end < total ? String(end) : "",
  };
}

/**
 * The provider directory, cycling protocols and states.
 *
 * Each of the three protocols appears, and so does each state a row can be in:
 * ready and enabled (no mark), disabled (a grey dot) and not ready (an amber
 * dot). A walkthrough that only ever saw ready providers would never reach the
 * row treatments the list exists to show.
 */
// The protocol default for the sign-in switch, overridden per row by the mask.
function providerSignIn(
  config: MockConfig,
  index: number,
  protocol: string,
): boolean {
  const letter = config.admin.providerSignInMask[index];
  if (letter === "x") return false;
  if (letter === "o") return true;
  return protocol !== "vrchat";
}

function rowIndexOfSlug(config: MockConfig, slug: string): number {
  return identityProviders(config).findIndex(
    (provider) => provider.slug === slug,
  );
}

function identityProviders(config: MockConfig): IdentityProvider[] {
  const protocols: readonly string[] = ["oidc", "steam", "vrchat"];
  return range(config.admin.identityProviders).map((index) => {
    const protocol = protocols[index % protocols.length] ?? "oidc";
    // Every third row is disabled; every fourth is a public OIDC client when
    // the protocol cycle lands one there and not ready otherwise, so a
    // walkthrough sees both the amber dot and the public-client edit page.
    const disabled = index % 3 === 1;
    // The fourth row is the public OIDC client: a saved "none" method with no
    // secret, which the server counts as ready, so it takes the place the
    // every-fourth "not ready" row otherwise would.
    const publicClient = protocol === "oidc" && index % 4 === 3;
    // Readiness follows the credential the way the server derives it: a Steam
    // provider is ready only with a key, a VRChat one only with a valid
    // operator session, a confidential OIDC one only with a stored secret, and
    // a public client needs none. The not-ready mask changes that credential
    // (no key, an expired session, no secret) instead of forcing the flag; for
    // a public client it breaks the connection.
    const mask = config.admin.providerNotReadyMask[index];
    const operator = config.admin.operatorSession;
    let configured: boolean;
    let expired = false;
    if (protocol === "vrchat") {
      configured =
        operator === "fixture"
          ? index % 2 === 0
          : operator === "valid" || operator === "expired";
      expired = operator === "expired";
      if (mask === "x") {
        configured = true;
        expired = true;
      } else if (mask === "o") {
        configured = true;
        expired = false;
      }
    } else if (publicClient) {
      // A public client holds no secret by design.
      configured = false;
    } else {
      configured = index % 4 !== 3;
      if (mask === "x") configured = false;
      else if (mask === "o") configured = true;
    }
    // A public client has no credential to lose, so the mask takes away its
    // connection instead: the row is not ready because its provider cannot be
    // reached.
    const ready = publicClient ? mask !== "x" : configured && !expired;
    return {
      slug: `provider-${index + 1}`,
      displayName: `Example ${protocol.toUpperCase()} ${index + 1}`,
      protocol,
      // VRChat verifies identities by default; everyone else backs a button.
      // The mask records what the panel's write switch changed.
      allowSignIn: providerSignIn(config, index, protocol),
      // One row in three has pending invitations riding on it, so the delete
      // confirmation can show a nonzero count.
      pendingInvitationCount: index % 3 === 0 ? 2 : 0,
      mode:
        protocol === "vrchat"
          ? "link_only"
          : index % 2 === 0
            ? "auto_provision"
            : "invite_only",
      disabled,
      ready,
      secretConfigured: configured,
      secretStatus: !configured
        ? "unconfigured"
        : expired
          ? "invalid"
          : "valid",
      secretValidatedAt: configured ? iso(-day) : null,
      createdAt: iso(-day * (index + 2)),
      ...(index % 2 === 0 ? { iconUrl: mockAvatarUrl } : {}),
      config: config0(protocol, index),
      supportsOperator: protocol === "vrchat",
      searchFields: [
        { key: "email", operators: ["eq", "contains"] },
        { key: "subject", operators: ["eq"] },
      ],
      // Varied on purpose: the list shows this as a column, and a column of
      // identical numbers says nothing about whether it is being read from the
      // right field. Zero is a real case — a provider nobody has used is the one
      // an operator is most likely to touch.
      linkedAccountCount: index % 4 === 3 ? 0 : (index + 1) * 7,
    } satisfies IdentityProvider;
  });
}

/**
 * A complete OIDC config for the rows that carry one, `{}` for the rest. Every
 * fourth row is a public client: it names no endpoints, because discovery is
 * the mode that reports them, and no secret. Row 7 (index 6) is the plain
 * OAuth 2.0 provider: manual endpoints with JWKS `none`, a UserInfo address
 * and no scopes, so that shape can be reviewed without a backend.
 */
function config0(protocol: string, index: number): unknown {
  if (protocol !== "oidc") return {};
  const issuer = `https://idp${index + 1}.example.test`;
  const publicClient = index % 4 === 3;
  const plainOAuth2 = index === 6;
  const manual = (index % 2 !== 0 && !publicClient) || plainOAuth2;
  const base = {
    issuerUrl: issuer,
    clientId: `client-${index + 1}`,
    scopes: plainOAuth2 ? [] : ["openid", "profile", "email"],
    allowedDomains: [],
    requireVerifiedEmail: true,
    allowPrivateNetwork: false,
    usernameClaim: "preferred_username",
    displayNameClaim: "name",
    emailClaim: "email",
    pictureClaim: "picture",
    subjectClaim: plainOAuth2 ? "id" : "sub",
    // A manual configuration names its endpoints and its token auth method,
    // as the server requires; a discovered one overrides nothing.
    configurationMode: manual ? "manual" : "discovery",
    endpoints: manual
      ? {
          authorization: `${issuer}/authorize`,
          token: `${issuer}/token`,
          userinfo: plainOAuth2 ? `${issuer}/user` : null,
          jwks: plainOAuth2 ? "none" : `${issuer}/jwks`,
        }
      : { authorization: null, token: null, userinfo: null, jwks: null },
    tokenAuthMethod: publicClient
      ? "none"
      : manual
        ? "client_secret_post"
        : "discovery",
    pkceMethod: "S256",
    // The other providers leave it out, as a provider saved before the
    // setting existed does. The one public client the panel can show (its
    // list caps at ten providers) carries the token variant, so both the
    // third option and its hint can be reviewed without a backend.
    ...(publicClient ? { responseType: "id_token token" as const } : {}),
    ...(index % 4 === 1 ? { authenticateAvatarFetch: true } : {}),
  };
  return base;
}

/**
 * OIDC applications, cycling the two client types, the two access states, and
 * the redirect shapes the list's diagnostic column has to render.
 *
 * The list does not carry a status column: a row that is disabled recedes, and a
 * restricted one carries a padlock, so the fixture has to produce all of those
 * as well as the ordinary rows, or a walkthrough never sees them. The redirect
 * column is the one that replaced the scopes column, so it deliberately includes
 * a client with a single address, one with several (the `+n` count), and one
 * with none at all — that last one being the misconfiguration the column exists
 * to surface.
 */
function oidcApplications(
  config: MockConfig,
): components["schemas"]["OIDCApplicationView"][] {
  return adminRange(config.admin.oidcApps).map((index) => {
    const redirectCount = index % 4 === 3 ? 0 : (index % 3) + 1;
    return {
      clientId: `mock-client-${index + 1}`,
      displayName: `Sample application ${index + 1}`,
      redirectUris: range(redirectCount).map(
        (n) => `https://app${index + 1}-${n + 1}.example.test/callback`,
      ),
      postLogoutRedirectUris: [
        `https://app${index + 1}.example.test/signed-out`,
      ],
      // The first reads a provider's identities; the second still allows one
      // for a provider that is gone, which the scope field draws to be cleared.
      allowedScopes: [
        "openid",
        "profile",
        "email",
        ...(index === 0 ? ["identity:provider-1"] : []),
        ...(index === 1 ? ["identity:retired-sso"] : []),
      ],
      clientAuthMethod: index % 3 === 2 ? "none" : "client_secret",
      disabled: index % 4 === 1,
      accessRestricted: index % 3 === 1,
      subjectSource: "sub",
      // Always an object, never a conditional: `claimAliases` is an index
      // signature, and a spread that sometimes contributes a key widens it to
      // include `undefined`.
      claimAliases:
        index % 2 === 0
          ? { nickname: "preferred_username" }
          : ({} as Record<string, string>),
      ...(index % 2 === 0 ? { iconUrl: mockAvatarUrl } : {}),
      ...(index % 4 === 2
        ? { launchUrl: `https://app${index + 1}.example.test` }
        : {}),
      requireConsent: index % 2 === 1,
      requirePkce: true,
      createdAt: iso(-day * (index + 2)),
    };
  });
}

const samlAttributeShapes = [
  {
    name: "mail",
    name_format: "urn:oasis:names:tc:SAML:2.0:attrname-format:basic",
    friendly_name: "Mail address",
    source: "attributes.mail",
    multi: false,
  },
  {
    name: "groups",
    name_format: "urn:oasis:names:tc:SAML:2.0:attrname-format:uri",
    source: "groups",
    multi: true,
  },
  {
    name: "uid",
    name_format: "urn:example:names:custom",
    source: "username",
    multi: false,
  },
];

/** `count` attributes, cycling the three shapes under names that stay unique. */
function samlAttributeMap(count: number) {
  return Array.from({ length: clampAdminCount(count) }, (_, position) => {
    const shape = samlAttributeShapes[position % samlAttributeShapes.length];
    const round = Math.floor(position / samlAttributeShapes.length);
    return shape === undefined || round === 0
      ? { ...shape }
      : { ...shape, name: `${shape.name}${round + 1}` };
  });
}

/**
 * SAML applications, cycling ACS shapes, the session-lifetime field, and the
 * three certificate states the list's expiry column has to render: a healthy
 * key, one that has already expired (the only state on this list that is
 * already broken), and a service provider with no signing key published at all.
 *
 * Two in three carry an attribute map covering the three shapes a source takes
 * — an account attribute, a named fact that is multi-valued, a named fact that
 * is not — plus a name format outside the standard list, which the editor has
 * to offer as it is rather than rewrite. Every third has none, for the empty
 * editor.
 */
function samlApplications(
  config: MockConfig,
): components["schemas"]["SAMLApplicationView"][] {
  return adminRange(config.admin.samlApps).map((index) => {
    const keyState = index % 3;
    return {
      id: index + 1,
      entityId: `https://saml${index + 1}.example.test/metadata`,
      displayName: `SAML application ${index + 1}`,
      nameIdFormat:
        index % 2 === 0
          ? "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent"
          : "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress",
      attributeMap:
        index % 3 === 2 ? [] : samlAttributeMap(config.admin.samlAttributes),
      requireSignedAuthnRequest: index % 2 === 0,
      allowIdpInitiated: index % 3 === 0,
      disabled: index % 4 === 1,
      accessRestricted: index % 3 === 1,
      ...(index % 4 === 2 ? { sessionLifetimeSecs: 3600 } : {}),
      ...(index % 2 === 0 ? { iconUrl: mockAvatarUrl } : {}),
      acs: [
        {
          binding: "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST",
          location: `https://saml${index + 1}.example.test/acs`,
          index: 0,
          isDefault: true,
        },
      ],
      // The expiry column looks at `signing` keys only, so the fixture carries an
      // encryption key alongside each signing one — a walkthrough that saw only
      // signing keys would not catch a column that read the wrong `use`.
      keys:
        keyState === 2
          ? []
          : [
              {
                use: "signing",
                notAfter:
                  keyState === 1
                    ? iso(-day * 12)
                    : iso(day * (30 * (index + 1))),
              },
              { use: "encryption", notAfter: iso(day * 365) },
            ],
      createdAt: iso(-day * (index + 2)),
    };
  });
}

/** Forward-auth applications. */
function forwardAuthApplications(
  config: MockConfig,
): components["schemas"]["ForwardAuthAppView"][] {
  return adminRange(config.admin.forwardAuthApps).map((index) => ({
    clientId: `mock-forward-auth-${index + 1}`,
    displayName: `Protected service ${index + 1}`,
    forwardAuthHost: `service${index + 1}.example.test`,
    accessRestricted: index % 3 === 1,
    disabled: index % 4 === 1,
    remoteUserSource: index % 2 === 0 ? "username" : "verified_email",
    ...(index % 2 === 0 ? { iconUrl: mockAvatarUrl } : {}),
    createdAt: iso(-day * (index + 2)),
  }));
}

/**
 * The access workspace the access panel reads: the application's summary, its
 * restriction flag and the user groups selected for it.
 *
 * The same shape answers for all three protocols and for an administrator and a
 * delegated manager alike, which is why the fixture does not branch on the
 * caller. What it does vary is the group selection — rotated by the application
 * id — so a walkthrough sees both "no groups selected" and a populated list.
 */
function accessWorkspace(
  config: MockConfig,
  kind: string,
  appId: string,
): AppAccessWorkspace {
  const groups = groupsFrom(config);
  const offset = (appId.length + kind.length) % 3;
  const selected = groups.slice(offset, offset + ((appId.length % 3) + 1));

  const oidc = oidcApplications(config).find((app) => app.clientId === appId);
  const saml = samlApplications(config).find((app) => String(app.id) === appId);
  const forwardAuth = forwardAuthApplications(config).find(
    (app) => app.clientId === appId,
  );

  return {
    app: {
      kind,
      appId,
      displayName:
        oidc?.displayName ??
        saml?.displayName ??
        forwardAuth?.displayName ??
        appId,
      accessRestricted:
        oidc?.accessRestricted ??
        saml?.accessRestricted ??
        forwardAuth?.accessRestricted ??
        false,
      ...((oidc?.iconUrl ?? saml?.iconUrl ?? forwardAuth?.iconUrl) === undefined
        ? {}
        : { iconUrl: oidc?.iconUrl ?? saml?.iconUrl ?? forwardAuth?.iconUrl }),
      ...(forwardAuth === undefined
        ? {}
        : { forwardAuthHost: forwardAuth.forwardAuthHost }),
      ...(saml === undefined ? {} : { entityId: saml.entityId }),
    },
    accessRestricted:
      oidc?.accessRestricted ??
      saml?.accessRestricted ??
      forwardAuth?.accessRestricted ??
      false,
    providers: groupProviders(),
    groups: selected.map((group) => ({
      ...group,
      applicationCount: 1,
    })),
  };
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
    // Deliberately not this origin, so a page that fell back to the origin
    // instead of reading the document cannot be mistaken for one that did.
    case "/.well-known/openid-configuration":
      return json({ issuer: "https://id.mock.example.test" });
    case "/api/prohibitorum/auth/status":
      return json({ bootstrapped: config.instance.bootstrapped });
    // The sign-in link's target app, answered like the server would: a label
    // and host (no icon, so the initials tile shows) for this origin's
    // authorize/SSO URLs, no context otherwise.
    case "/api/prohibitorum/forward-auth/login-context": {
      const origin = new URL(request.url).origin;
      const raw = new URL(request.url).searchParams.get("return_to") ?? "";
      let target: URL | null = null;
      try {
        target = new URL(raw);
      } catch {
        target = null;
      }
      const label =
        target !== null &&
        target.origin === origin &&
        (target.pathname === "/oauth/authorize" ||
          target.pathname === "/saml/sso" ||
          target.pathname === "/saml/sso/init")
          ? consentRequest(config).client.displayName
          : null;
      return json({
        application:
          label === null
            ? null
            : {
                label,
                host: "wiki.example.test",
                iconUrl: appLogoUrl("#3b6e4f", "W"),
              },
      });
    }
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
    case "/api/prohibitorum/me/avatar":
      return guarded(config, () => json(myAvatarView(config)));
    case "/api/prohibitorum/auth/federation":
      return json(providerList(config));
    case "/api/prohibitorum/me/sudo/methods":
      return guarded(config, () => json(sudoMethods(config)));
    case "/api/prohibitorum/consent":
      return consentReply(config, () => consentRequest(config));
    case "/api/prohibitorum/saml-consent":
      return consentReply(config, () => samlConsentRequest(config));
    case "/api/prohibitorum/enrollments/{token}":
      return config.publicFlows.enrollment.valid
        ? json(enrollmentPreview(config))
        : expiredEnrollment();
    case "/api/prohibitorum/auth/federation/-/confirm":
      return config.publicFlows.welcome.valid
        ? json(federationConfirm(config))
        : expiredSignIn();
    case "/api/prohibitorum/require-identity/{ticket}":
      return guarded(config, () =>
        config.publicFlows.requireIdentity.state === "expired"
          ? {
              kind: "error",
              status: 410,
              code: "require_identity_ticket_expired",
            }
          : json(requireIdentityRequest(config)),
      );
    case "/api/prohibitorum/auth/federation/-/flows/{flow}":
      return json(federationFlow(config, new URL(request.url).origin));
    case "/api/prohibitorum/auth/devices/pair/status":
      return pairingStatus(request, config);
    case "/api/prohibitorum/me/devices/pair/lookup": {
      const code = new URL(request.url).searchParams.get("code") ?? "";
      return guarded(config, () => json(pairing(code, config)));
    }

    /* -------------------------------------------------- admin directory -- */

    // The management reads answer with the same account the panel signed in as,
    // so a walkthrough that edits its own profile sees one account rather than
    // two. A member is answered with the whole directory here too: the real
    // server would refuse it, but the guard that keeps a member out of these
    // pages is the role on `/me`, which this fixture also drives.
    case "/api/prohibitorum/accounts": {
      // `q` matches the username or the display name, ignoring case, before
      // the result pages — so a search reaches past the first page.
      const params = new URL(request.url).searchParams;
      const q = (params.get("q") ?? "").toLowerCase();
      const all = accounts(config).filter(
        (account) =>
          q === "" ||
          account.username.toLowerCase().includes(q) ||
          account.displayName.toLowerCase().includes(q),
      );
      const cursor = params.get("cursor");
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
      // The id is the segment before `tokens`, and the list is a page.
      const id = Number(new URL(request.url).pathname.split("/").at(-2));
      return guarded(config, () =>
        json({
          items: accountTokens(Number.isFinite(id) ? id - 1 : 0),
          nextCursor: "",
        }),
      );
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
    case "/api/prohibitorum/identity-providers": {
      const all = identityProviders(config);
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
    case "/api/prohibitorum/identity-providers/{slug}": {
      const slug = new URL(request.url).pathname.split("/").pop() ?? "";
      const found = identityProviders(config).find(
        (provider) => provider.slug === slug,
      );
      return guarded(config, () =>
        found
          ? json(found)
          : {
              kind: "error",
              status: 404,
              code: "upstream_idp_not_found",
            },
      );
    }
    // Both diagnostic reads are GETs, so they answer with the other reads
    // rather than waiting on the writes switch.
    case "/api/prohibitorum/identity-providers/{slug}/effective-config": {
      const slug = new URL(request.url).pathname.split("/").at(-2) ?? "";
      // The plain OAuth 2.0 row (see config0): everything manual, a UserInfo
      // endpoint, the JWKS sentinel and no scopes, so the table's special
      // values can be reviewed against it.
      if (slug === "provider-7") {
        return json({
          mode: "manual",
          fetchedAt: iso(0),
          callbackUrl:
            "http://localhost:8080/api/prohibitorum/auth/federation/provider-7/test/callback",
          fields: {
            issuer: { value: "https://idp7.example.test", source: "manual" },
            authorizationEndpoint: {
              value: "https://idp7.example.test/authorize",
              source: "manual",
            },
            tokenEndpoint: {
              value: "https://idp7.example.test/token",
              source: "manual",
            },
            userinfoEndpoint: {
              value: "https://idp7.example.test/user",
              source: "manual",
            },
            jwksEndpoint: { value: "none", source: "manual" },
            tokenAuthMethod: {
              value: "client_secret_post",
              source: "manual",
            },
            pkceMethod: { value: "S256", source: "manual" },
            responseType: { value: "code", source: "manual" },
            scopes: { value: [], source: "manual" },
          },
        });
      }
      return json({
        mode: "discovery",
        fetchedAt: iso(0),
        callbackUrl: `http://localhost:8080/api/prohibitorum/auth/federation/provider-1/test/callback`,
        // Every field the server resolves, with all three sources and an empty
        // value among them, so the table's every cell shape can be reviewed.
        fields: {
          issuer: { value: "https://idp1.example.test", source: "discovery" },
          authorizationEndpoint: {
            value: "https://idp1.example.test/authorize",
            source: "discovery",
          },
          tokenEndpoint: {
            value: "https://idp1.example.test/token",
            source: "discovery",
          },
          userinfoEndpoint: { value: "", source: "discovery" },
          jwksEndpoint: {
            value: "https://keys.idp1.example.test/jwks",
            source: "override",
          },
          tokenAuthMethod: {
            value: "client_secret_basic",
            source: "discovery",
          },
          pkceMethod: { value: "S256", source: "manual" },
          responseType: { value: "code", source: "manual" },
          scopes: { value: ["openid", "profile", "email"], source: "manual" },
        },
      });
    }

    case "/api/prohibitorum/identity-providers/{slug}/tests/latest":
      return json(latestConnectionTest(config));

    case "/api/prohibitorum/identity-providers/{slug}/tests/{id}":
      return json(diagnosticResult(config.admin.diagnosticOutcome));

    case "/api/prohibitorum/oidc-applications": {
      const all = assignedTo(config, "oidc", oidcApplications(config));
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
    case "/api/prohibitorum/oidc-applications/{clientId}": {
      const clientId = new URL(request.url).pathname.split("/").pop() ?? "";
      const found = oidcApplications(config).find(
        (application) => application.clientId === clientId,
      );
      return guarded(config, () =>
        found
          ? json(found)
          : { kind: "error", status: 404, code: "client_not_found" },
      );
    }
    case "/api/prohibitorum/saml-applications": {
      const all = assignedTo(config, "saml", samlApplications(config));
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
    case "/api/prohibitorum/saml-applications/{id}": {
      const id = pathTail(request);
      const found = samlApplications(config).find(
        (application) => application.id === id,
      );
      return guarded(config, () =>
        found
          ? json(found)
          : { kind: "error", status: 404, code: "client_not_found" },
      );
    }
    case "/api/prohibitorum/forward-auth-apps": {
      const all = assignedTo(
        config,
        "forwardAuth",
        forwardAuthApplications(config),
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
    case "/api/prohibitorum/forward-auth-apps/{clientId}": {
      const clientId = new URL(request.url).pathname.split("/").pop() ?? "";
      const found = forwardAuthApplications(config).find(
        (application) => application.clientId === clientId,
      );
      return guarded(config, () =>
        found
          ? json(found)
          : { kind: "error", status: 404, code: "client_not_found" },
      );
    }
    case "/api/prohibitorum/managed-applications/{kind}/{appId}/access": {
      const segments = new URL(request.url).pathname.split("/");
      const kind = segments[segments.length - 3] ?? "oidc";
      const appId = segments[segments.length - 2] ?? "";
      return guarded(config, () => json(accessWorkspace(config, kind, appId)));
    }
    // Every application is managed by accounts 2 and 3, so the assign dialog
    // has candidates it must refuse as already assigned (and account 4, which
    // the directory disables, one it refuses as disabled).
    case "/api/prohibitorum/oidc-applications/{clientId}/managers":
    case "/api/prohibitorum/forward-auth-apps/{clientId}/managers":
    case "/api/prohibitorum/saml-applications/{id}/managers":
      return guarded(config, () =>
        json(
          accounts(config)
            .filter((account) => account.id === 2 || account.id === 3)
            .map((account) => ({
              id: account.id,
              username: account.username,
              displayName: account.displayName,
              disabled: account.disabled,
              assignedAt: iso(-day * account.id * 5),
            })),
        ),
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
    case "/api/prohibitorum/groups/{groupId}/decisions": {
      const cursor = new URL(request.url).searchParams.get("cursor");
      return guarded(config, () =>
        cursor !== null && config.admin.groupDecisionsLaterPagesFail
          ? { kind: "error", status: 500, code: "internal_error" }
          : json(groupDecisions(config, cursor ?? undefined)),
      );
    }
    case "/api/prohibitorum/groups/{groupId}/preview":
      return guarded(config, () =>
        json(
          groupPreview(config, {
            limit: Number(new URL(request.url).searchParams.get("limit")),
          }),
        ),
      );
    case "/api/prohibitorum/groups/{groupId}/applications": {
      // As many applications as the group's own count says, so the list and the
      // count never disagree about whether the group can be deleted.
      const id = Number(
        new URL(request.url).pathname.match(/\/groups\/(\d+)\//)?.[1],
      );
      const found = groupsFrom(config).find((group) => group.id === id);
      const kinds = ["oidc", "saml", "forward_auth"];
      return guarded(config, () =>
        found
          ? json({
              items: range(found.applicationCount ?? 0).map((index) => ({
                appId: `mock-client-${index + 1}`,
                kind: kinds[index % kinds.length],
                displayName: `Sample application ${index + 1}`,
                iconUrl: mockAvatarUrl,
              })),
              nextCursor: "",
            })
          : { kind: "error", status: 404, code: "not_found" },
      );
    }
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
    case "/branding/wallpaper": {
      const background = config.instance.loginAppearance.background;
      if (background.source === "bing") {
        return json(
          mockWallpaper(
            "bing",
            background.bing.market,
            background.bing.showCaption,
          ),
        );
      }
      if (background.source === "unsplash") {
        return json(mockWallpaper("unsplash", background.unsplash.query, true));
      }
      return { kind: "error", status: 404, code: "wallpaper_not_configured" };
    }
    case "/api/prohibitorum/admin/settings/login-appearance":
      return guarded(config, () =>
        json({
          appearance: config.instance.loginAppearance,
          hasUnsplashKey: config.instance.hasUnsplashKey,
        }),
      );
    case "/api/prohibitorum/admin/settings/login-appearance/wallpaper": {
      const params = new URL(request.url).searchParams;
      const source = params.get("source");
      if (source === "bing") {
        return guarded(config, () =>
          json(mockWallpaper("bing", params.get("market") ?? "", true)),
        );
      }
      if (source === "unsplash") {
        if (!config.instance.hasUnsplashKey) {
          return { kind: "error", status: 400, code: "unsplash_key_required" };
        }
        return guarded(config, () =>
          json(mockWallpaper("unsplash", params.get("query") ?? "", true)),
        );
      }
      return { kind: "error", status: 400, code: "bad_request" };
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
            label: "all",
            result: accountId % 2 === 0,
            children: [
              {
                path: "$.children[0]",
                label: "login_method=passkey",
                result: true,
              },
              {
                path: "$.children[1]",
                label: "any",
                result: accountId % 2 === 0,
                children: [
                  {
                    path: "$.children[1].children[0]",
                    label: "connection.provider=provider-1",
                    result: accountId % 2 === 0,
                  },
                  {
                    path: "$.children[1].children[1]",
                    label: "connection.protocol=steam",
                    result: false,
                  },
                ],
              },
              {
                path: "$.children[2]",
                label: "not",
                result: true,
                children: [
                  {
                    path: "$.children[2].child",
                    label: "avatar=user_uploaded",
                    result: false,
                  },
                ],
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

    // Allowing an OIDC consent resumes the authorization, which the mock
    // cannot, and a SAML one posts the assertion from `/saml/sso/resume`,
    // which it cannot either; both land on a page of the console instead.
    // Denying goes to the application's callback, and home here.
    case "/api/prohibitorum/consent":
      return guarded(config, () =>
        !config.publicFlows.consent.ticketValid
          ? invalidConsentTicket()
          : json({
              redirect:
                stringField(body, "decision") === "approve"
                  ? consentApproveTarget(request.url)
                  : "/",
            }),
      );

    case "/api/prohibitorum/saml-consent":
      return guarded(config, () =>
        config.publicFlows.consent.ticketValid
          ? json({ redirect: "/" })
          : invalidConsentTicket(),
      );

    // A passkey cannot be made from a fabricated challenge, so the passkey
    // enrollment's begin falls through to the unmocked failure.
    case "/api/prohibitorum/enrollments/{token}/password-totp/verify":
      return enrollPasswordTotp(config, body);

    case "/api/prohibitorum/auth/federation/-/confirm": {
      if (!config.publicFlows.welcome.valid) return expiredSignIn();
      return json(
        {
          redirect: "/",
          offerLocalSignin: config.publicFlows.welcome.offerLocalSignin,
        },
        (draft) => {
          draft.session.signedIn = true;
        },
      );
    }

    case "/api/prohibitorum/auth/federation/-/confirm/decline":
      return empty();

    case "/api/prohibitorum/auth/federation/-/flows/{flow}/prepare":
      return json(
        federationFlow(
          {
            ...config,
            publicFlows: {
              ...config.publicFlows,
              flow: { ...config.publicFlows.flow, step: "proof" },
            },
          },
          new URL(request.url).origin,
        ),
        (draft) => {
          draft.publicFlows.flow.step = "proof";
        },
      );

    case "/api/prohibitorum/auth/federation/-/flows/{flow}/verify":
      return verifyFlow(config, body);

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
      const access =
        tokenAccessLevels.find(
          (level) => level === stringField(body, "access"),
        ) ?? "all_apps";
      const token = mockTokenPlaintext();
      return json(
        {
          token,
          pat: {
            id,
            name,
            tokenHint: mockTokenHint(token),
            access,
            apps: (access === "selected_apps"
              ? stringListField(body, "appClientIds")
              : []
            ).map((clientId) => ({
              clientId,
              displayName:
                forwardAuthAppList(config).find(
                  (app) => app.clientId === clientId,
                )?.displayName ?? clientId,
            })),
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

    case "/api/prohibitorum/auth/devices/pair/begin":
      return startPairing(config);

    case "/api/prohibitorum/auth/devices/pair/complete":
      return completePairing(request, config);

    // The approved device signs in on its own a moment later; here it is
    // simply the next session the list reads.
    case "/api/prohibitorum/me/devices/pair/approve":
      return empty(204, (draft) => {
        draft.lists.sessions = clampCount(draft.lists.sessions + 1);
      });

    case "/api/prohibitorum/me/devices/pair/cancel":
      return empty();

    // Choosing records that the account has chosen, so a later upload no
    // longer takes over; a source that is not stored is refused.
    case "/api/prohibitorum/me/avatar/selection": {
      const source = stringField(body, "source") ?? "";
      const stored =
        source === "none" ||
        avatarSources(config).some((entry) => entry.source === source);
      if (!stored) {
        return {
          kind: "error",
          status: 400,
          code: "avatar_source_unavailable",
        };
      }
      return empty(204, (draft) => {
        draft.avatar.active = source;
        draft.avatar.selected = true;
      });
    }

    // An upload is shown at once only by an account that has never chosen;
    // removing the picture in use falls back to the first provider's.
    case "/api/prohibitorum/me/avatar":
      if (method === "PUT") {
        return empty(204, (draft) => {
          draft.avatar.upload = true;
          if (!draft.avatar.selected) draft.avatar.active = "user";
        });
      }
      if (method === "DELETE") {
        return empty(204, (draft) => {
          draft.avatar.upload = false;
          if (draft.avatar.active === "user") {
            draft.avatar.active =
              draft.avatar.upstreams > 0 ? "upstream:provider-1" : "none";
          }
        });
      }
      return undefined;

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
      const slug = stringField(body, "expectedUpstreamIdpSlug");
      if (slug !== undefined && slug !== "") {
        const bound = identityProviders(config).find(
          (provider) => provider.slug === slug,
        );
        if (bound === undefined) {
          return { kind: "error", status: 404, code: "upstream_idp_not_found" };
        }
        if (bound.mode === "link_only") {
          return {
            kind: "error",
            status: 400,
            code: "upstream_idp_link_only",
          };
        }
      }
      const forcedLocal =
        booleanField(body, "requireLocalCredential") === true ||
        (slug !== undefined &&
          slug !== "" &&
          !identityProviders(config).some(
            (provider) => provider.slug === slug && provider.allowSignIn,
          ));
      return json(
        {
          url: `http://localhost:8080/enroll/mock-invitation-${id}`,
          expiresAt: iso(day * 7),
          groupIds: (field(body, "groupIds") as number[] | undefined) ?? null,
          username: stringField(body, "username") ?? "",
          requireLocalCredential: forcedLocal,
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
      // An existing group's identifier is taken, as the server answers.
      if (groupsFrom(config).some((group) => group.slug === slugOf(body))) {
        return { kind: "error", status: 409, code: "group_slug_conflict" };
      }
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
        account: accountSummary(accountAt(accountId - 1, config)),
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

    case "/api/prohibitorum/admin/settings/login-appearance": {
      if (method !== "PUT") return undefined;
      const appearance = field(body, "appearance") as
        | LoginAppearance
        | undefined;
      const key = stringField(body, "unsplashAccessKey");
      if (appearance === undefined) {
        return { kind: "error", status: 400, code: "bad_request" };
      }
      // A key starting with "bad" stands for one Unsplash refuses.
      if (key?.startsWith("bad")) {
        return { kind: "error", status: 400, code: "unsplash_key_invalid" };
      }
      if (
        appearance.background.source === "unsplash" &&
        key === undefined &&
        !config.instance.hasUnsplashKey
      ) {
        return { kind: "error", status: 400, code: "unsplash_key_required" };
      }
      return empty(204, (draft) => {
        draft.instance.loginAppearance = structuredClone(appearance);
        if (key !== undefined) draft.instance.hasUnsplashKey = true;
      });
    }

    case "/api/prohibitorum/admin/settings/login-appearance/unsplash-key":
      if (method !== "DELETE") return undefined;
      if (config.instance.loginAppearance.background.source === "unsplash") {
        return { kind: "error", status: 409, code: "unsplash_key_in_use" };
      }
      return empty(204, (draft) => {
        draft.instance.hasUnsplashKey = false;
      });

    case "/api/prohibitorum/admin/settings/login-images": {
      if (method !== "POST") return undefined;
      const count = config.instance.loginImageCount;
      if (count >= mockLoginImagesMax) {
        return { kind: "error", status: 409, code: "login_images_full" };
      }
      const revision = config.instance.imageRevision + 1;
      const id = count + 1;
      return {
        kind: "json",
        status: 201,
        body: {
          id,
          url: mockLoginImageUrl(
            {
              ...config,
              instance: { ...config.instance, imageRevision: revision },
            },
            id,
          ),
          etag: `login-image-${id}-${revision}`,
        } satisfies LoginImage,
        effect: (draft) => {
          draft.instance.loginImageCount = id;
          draft.instance.imageRevision = revision;
        },
      };
    }

    case "/api/prohibitorum/admin/settings/login-images/{id}": {
      if (method !== "DELETE") return undefined;
      const id = pathTail(request);
      if (id < 1 || id > config.instance.loginImageCount) {
        return { kind: "error", status: 404, code: "login_image_not_found" };
      }
      return empty(204, (draft) => {
        draft.instance.loginImageCount -= 1;
        draft.instance.imageRevision += 1;
      });
    }

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

    case "/api/prohibitorum/identity-providers": {
      const slug = stringField(body, "slug") ?? `provider-${Date.now()}`;
      const protocol = stringField(body, "protocol") ?? "oidc";
      // An existing provider's identifier is taken, as the server answers.
      if (
        identityProviders(config).some((provider) => provider.slug === slug)
      ) {
        return {
          kind: "error",
          status: 409,
          code: "upstream_idp_already_exists",
        };
      }
      const created: IdentityProvider = {
        slug,
        displayName: stringField(body, "displayName") ?? slug,
        protocol,
        allowSignIn:
          (body as { allowSignIn?: boolean }).allowSignIn ??
          protocol !== "vrchat",
        mode: stringField(body, "mode") ?? "invite_only",
        // Nothing can have verified through a provider created this moment.
        pendingInvitationCount: 0,
        disabled: false,
        ready: false,
        secretConfigured: stringField(body, "secret") !== undefined,
        secretStatus:
          stringField(body, "secret") === undefined
            ? "unconfigured"
            : "configured",
        secretValidatedAt: null,
        createdAt: iso(0),
        config: field(body, "config") ?? {},
        supportsOperator: protocol === "vrchat",
        searchFields: [],
        linkedAccountCount: 0,
      };
      return {
        kind: "json",
        status: 201,
        body: created,
        // The new provider is not part of the generated set, so it is remembered
        // rather than derived: a walkthrough that creates one must find it again
        // on the list and on its own page.
        effect: (draft) => {
          draft.admin.identityProviders = Math.min(
            mockAdminListMax,
            draft.admin.identityProviders + 1,
          );
        },
      };
    }

    case "/api/prohibitorum/identity-providers/{slug}":
    case "/api/prohibitorum/identity-providers/set-disabled": {
      const slug =
        stringField(body, "slug") ??
        new URL(request.url).pathname.split("/").pop() ??
        "";
      const found = identityProviders(config).find(
        (provider) => provider.slug === slug,
      );
      if (found === undefined) {
        return { kind: "error", status: 404, code: "upstream_idp_not_found" };
      }
      const disabled = booleanField(body, "disabled");
      const allowSignIn = booleanField(body, "allowSignIn");
      const next: IdentityProvider =
        disabled !== undefined
          ? { ...found, disabled }
          : {
              ...found,
              displayName:
                stringField(body, "displayName") ?? found.displayName,
              mode: stringField(body, "mode") ?? found.mode,
              allowSignIn: allowSignIn ?? found.allowSignIn,
              config: field(body, "config") ?? found.config,
            };
      // Enabling a provider the server considers unready is refused, which is
      // the one branch of this endpoint a member view cannot produce.
      if (disabled === false && !found.ready) {
        return { kind: "error", status: 503, code: "provider_not_ready" };
      }
      // Record the sign-in switch in the mask so the change survives a reload.
      const effect =
        allowSignIn === undefined
          ? undefined
          : (draft: MockConfig) => {
              const index = rowIndexOfSlug(config, slug);
              if (index < 0) return;
              const mask = draft.admin.providerSignInMask
                .padEnd(config.admin.identityProviders, ".")
                .split("");
              mask[index] = allowSignIn ? "o" : "x";
              draft.admin.providerSignInMask = mask.join("");
            };
      return json(next, effect);
    }

    case "/api/prohibitorum/identity-providers/delete":
      return { kind: "empty", status: 204 };

    case "/api/prohibitorum/identity-providers/rotate-secret":
      return { kind: "empty", status: 204 };

    case "/api/prohibitorum/oidc-applications": {
      const clientId = stringField(body, "clientId") ?? `mock-client-new`;
      const publicClient = booleanField(body, "public") ?? false;
      // An existing application's Client ID is taken, as the server answers.
      if (oidcApplications(config).some((app) => app.clientId === clientId)) {
        return {
          kind: "error",
          status: 409,
          code: "oidc_client_already_exists",
        };
      }
      return {
        kind: "json",
        status: 201,
        body: {
          clientId,
          displayName: stringField(body, "displayName") ?? "",
          ...(publicClient ? {} : { secret: "mock-client-secret-value" }),
        },
      };
    }

    case "/api/prohibitorum/oidc-applications/{clientId}": {
      const clientId = new URL(request.url).pathname.split("/").pop() ?? "";
      const found = oidcApplications(config).find(
        (application) => application.clientId === clientId,
      );
      if (found === undefined) {
        return { kind: "error", status: 404, code: "client_not_found" };
      }
      // The PUT replaces the whole record, so the answer is the body with the
      // identity fields kept — which is what makes a walkthrough able to see
      // that an omitted field really does reset.
      return json({
        ...found,
        displayName: stringField(body, "displayName") ?? found.displayName,
        redirectUris:
          (field(body, "redirectUris") as string[] | undefined) ?? null,
        postLogoutRedirectUris:
          (field(body, "postLogoutRedirectUris") as string[] | undefined) ??
          null,
        allowedScopes:
          (field(body, "allowedScopes") as string[] | undefined) ?? null,
        requireConsent: booleanField(body, "requireConsent") ?? false,
        requirePkce: booleanField(body, "requirePkce") ?? found.requirePkce,
        disabled: booleanField(body, "disabled") ?? false,
        launchUrl: stringField(body, "launchUrl"),
      });
    }

    case "/api/prohibitorum/oidc-applications/set-disabled": {
      const clientId = stringField(body, "clientId") ?? "";
      const found = oidcApplications(config).find(
        (application) => application.clientId === clientId,
      );
      if (found === undefined) {
        return { kind: "error", status: 404, code: "client_not_found" };
      }
      return json({
        ...found,
        disabled: booleanField(body, "disabled") ?? false,
      });
    }

    case "/api/prohibitorum/oidc-applications/rotate-secret":
      return json({
        clientId: stringField(body, "clientId") ?? "",
        secret: "mock-rotated-client-secret",
      });

    case "/api/prohibitorum/oidc-applications/delete":
    case "/api/prohibitorum/saml-applications/delete":
    case "/api/prohibitorum/forward-auth-apps/delete":
      return { kind: "empty", status: 204 };

    case "/api/prohibitorum/saml-applications": {
      const id = clampAdminCount(config.admin.samlApps) + 1;
      return {
        kind: "json",
        status: 201,
        body: {
          id,
          entityId:
            stringField(body, "entityId") ?? `https://saml${id}.example.test`,
          displayName:
            stringField(body, "displayName") ?? `SAML application ${id}`,
          nameIdFormat: stringField(body, "nameIdFormat") ?? "",
          attributeMap: [],
          requireSignedAuthnRequest:
            booleanField(body, "requireSignedAuthnRequest") ?? false,
          allowIdpInitiated: booleanField(body, "allowIdpInitiated") ?? false,
          disabled: false,
          accessRestricted: booleanField(body, "accessRestricted") ?? false,
          acs: [],
          keys: [],
          createdAt: iso(0),
        },
      };
    }

    case "/api/prohibitorum/saml-applications/{id}": {
      const id = pathTail(request);
      const found = samlApplications(config).find(
        (application) => application.id === id,
      );
      if (found === undefined) {
        return { kind: "error", status: 404, code: "client_not_found" };
      }
      const seconds = field(body, "sessionLifetimeSecs");
      return json({
        ...found,
        displayName: stringField(body, "displayName") ?? found.displayName,
        nameIdFormat: stringField(body, "nameIdFormat") ?? found.nameIdFormat,
        attributeMap: field(body, "attributeMap") ?? [],
        requireSignedAuthnRequest:
          booleanField(body, "requireSignedAuthnRequest") ?? false,
        allowIdpInitiated: booleanField(body, "allowIdpInitiated") ?? false,
        ...(typeof seconds === "number"
          ? { sessionLifetimeSecs: seconds }
          : {}),
      });
    }

    case "/api/prohibitorum/saml-applications/set-disabled": {
      const id = Number(field(body, "id") ?? 0);
      const found = samlApplications(config).find(
        (application) => application.id === id,
      );
      if (found === undefined) {
        return { kind: "error", status: 404, code: "client_not_found" };
      }
      return json({
        ...found,
        disabled: booleanField(body, "disabled") ?? false,
      });
    }

    // The document is not read: the application comes back as it was, which is
    // all a walkthrough needs to see the replacement confirmed and settled.
    case "/api/prohibitorum/saml-applications/{id}/reingest-metadata": {
      const segments = new URL(request.url).pathname.split("/");
      const id = Number(segments[segments.length - 2]);
      const found = samlApplications(config).find(
        (application) => application.id === id,
      );
      if (found === undefined) {
        return { kind: "error", status: 404, code: "client_not_found" };
      }
      return json(found);
    }

    case "/api/prohibitorum/forward-auth-apps": {
      const clientId = stringField(body, "clientId") ?? "mock-forward-auth-new";
      return {
        kind: "json",
        status: 201,
        body: {
          clientId,
          displayName: stringField(body, "displayName") ?? "",
          forwardAuthHost: stringField(body, "host") ?? "",
          accessRestricted: booleanField(body, "accessRestricted") ?? false,
          disabled: false,
          remoteUserSource: "username",
          createdAt: iso(0),
        },
      };
    }

    case "/api/prohibitorum/forward-auth-apps/{clientId}": {
      const clientId = new URL(request.url).pathname.split("/").pop() ?? "";
      const found = forwardAuthApplications(config).find(
        (application) => application.clientId === clientId,
      );
      if (found === undefined) {
        return { kind: "error", status: 404, code: "client_not_found" };
      }
      return json({
        ...found,
        displayName: stringField(body, "displayName") ?? found.displayName,
        forwardAuthHost: stringField(body, "host") ?? found.forwardAuthHost,
      });
    }

    // The projection's own endpoints answer with the record as the write left
    // it. Like the whole-record PUTs above, the mock keeps no per-record edit,
    // so the next read shows the fixture again.
    case "/api/prohibitorum/oidc-applications/{clientId}/identity-projection": {
      const clientId = new URL(request.url).pathname.split("/").at(-2) ?? "";
      const found = oidcApplications(config).find(
        (application) => application.clientId === clientId,
      );
      if (found === undefined) {
        return { kind: "error", status: 404, code: "client_not_found" };
      }
      return json({
        ...found,
        subjectSource:
          stringField(body, "subjectSource") ?? found.subjectSource,
        claimAliases: field(body, "claimAliases") ?? found.claimAliases,
      });
    }

    case "/api/prohibitorum/forward-auth-apps/{clientId}/identity-projection": {
      const clientId = new URL(request.url).pathname.split("/").at(-2) ?? "";
      const found = forwardAuthApplications(config).find(
        (application) => application.clientId === clientId,
      );
      if (found === undefined) {
        return { kind: "error", status: 404, code: "client_not_found" };
      }
      return json({
        ...found,
        remoteUserSource:
          stringField(body, "remoteUserSource") ?? found.remoteUserSource,
      });
    }

    case "/api/prohibitorum/forward-auth-apps/set-disabled": {
      const clientId = stringField(body, "clientId") ?? "";
      const found = forwardAuthApplications(config).find(
        (application) => application.clientId === clientId,
      );
      if (found === undefined) {
        return { kind: "error", status: 404, code: "client_not_found" };
      }
      return json({
        ...found,
        disabled: booleanField(body, "disabled") ?? false,
      });
    }

    case "/api/prohibitorum/managed-applications/{kind}/{appId}/access/set-restricted": {
      const segments = new URL(request.url).pathname.split("/");
      const kind = segments[segments.length - 4] ?? "oidc";
      const appId = segments[segments.length - 3] ?? "";
      const workspace = accessWorkspace(config, kind, appId);
      const restricted = booleanField(body, "restricted") ?? false;
      return json({
        ...workspace.app,
        accessRestricted: restricted,
      });
    }

    case "/api/prohibitorum/managed-applications/{kind}/{appId}/groups": {
      // The path names the application, but the answer is only the groups the
      // caller asked to keep: the endpoint replaces the selection, so what comes
      // back is the ids that were sent, resolved against the directory.
      const ids = (field(body, "groupIds") as number[] | undefined) ?? [];
      const groups = groupsFrom(config).filter((group) =>
        ids.includes(group.id),
      );
      return json(groups.map((group) => ({ ...group, applicationCount: 1 })));
    }

    // Assigning a manager answers 204: the console refetches the manager list,
    // which is where the new row comes from.
    case "/api/prohibitorum/oidc-applications/{clientId}/managers":
    case "/api/prohibitorum/forward-auth-apps/{clientId}/managers":
    case "/api/prohibitorum/saml-applications/{id}/managers":
      return { kind: "empty", status: 204 };

    case "/api/prohibitorum/oidc-applications/{clientId}/managers/remove":
    case "/api/prohibitorum/forward-auth-apps/{clientId}/managers/remove":
    case "/api/prohibitorum/saml-applications/{id}/managers/remove":
      return { kind: "empty", status: 204 };

    case "/api/prohibitorum/identity-providers/{slug}/tests": {
      const id = "mock-diagnostic-run-id-0123456789abcdefghijkl";
      return json({
        id,
        authorizationUrl: "about:blank",
        expiresAt: iso(600_000),
      });
    }

    case "/api/prohibitorum/identity-providers/{slug}/tests/{id}/complete":
      return json({
        status: "running",
        expiresAt: iso(600_000),
        stages: [{ name: "discovery", status: "succeeded" }],
      });

    case "/api/prohibitorum/identity-providers/{slug}/operator-session/start": {
      const username = stringField(body, "username") ?? "";
      if (username === "challenge") {
        return json({
          status: "challenge",
          challenge: "mock-challenge",
          methods: ["totp", "emailOtp"],
          expiresAt: iso(600_000),
        });
      }
      return json({ status: "valid" });
    }

    case "/api/prohibitorum/identity-providers/{slug}/operator-session/verify": {
      const code = stringField(body, "code") ?? "";
      if (code !== "123456") {
        return {
          kind: "error",
          status: 422,
          code: "vrchat_operator_code_invalid",
        };
      }
      return json({ status: "valid" });
    }

    case "/api/prohibitorum/identity-providers/{slug}/operator-session/validate":
      if (config.admin.operatorCheckFails) {
        return { kind: "error", status: 429, code: "rate_limited" };
      }
      return json({ status: "valid" });

    case "/api/prohibitorum/groups/rule-preview": {
      // The draft is what is being previewed, so the answer is derived from it
      // rather than from the saved groups: a rule with no conditions matches
      // nobody, which is what the editor should show for an empty draft.
      const condition = field(body, "condition");
      const matches = hasLeaf(condition)
        ? groupPreview(config, {
            ...(numberField(body, "limit") === undefined
              ? {}
              : { limit: numberField(body, "limit") as number }),
            ...(stringField(body, "cursor") === undefined
              ? {}
              : { cursor: stringField(body, "cursor") as string }),
          })
        : emptyPreview();
      return json(matches);
    }

    default:
      return undefined;
  }
}

function slugOf(body: unknown): string | undefined {
  return stringField(body, "slug");
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

/**
 * The outcome the provider's latest connection test left behind: where each
 * mapped field is read and whether it came back, never the value. A failed test
 * stopped before any identity was read, so it carries no fields.
 */
function latestConnectionTest(config: MockConfig) {
  const outcome = config.admin.latestConnectionTest;
  if (outcome === "none") return { latest: null };
  const finishedAt = iso(-2 * 60_000);
  if (outcome === "failed" || outcome === "failed-stale") {
    return {
      latest: {
        status: "failed",
        finishedAt,
        stale: outcome === "failed-stale",
        fields: {},
      },
    };
  }
  const absent = config.admin.latestConnectionTestAbsent.split(/\s+/);
  const field = (name: string, source: string, claim: string) => [
    name,
    { source, claim, present: !absent.includes(name) },
  ];
  return {
    latest: {
      status: "succeeded",
      finishedAt,
      stale: outcome === "stale",
      fields: Object.fromEntries([
        field("issuer", "id_token", "iss"),
        field("subject", "id_token", "sub"),
        field("username", "id_token", "preferred_username"),
        field("displayName", "id_token", "name"),
        field("email", "id_token", "email"),
        field("emailVerified", "id_token", "email_verified"),
        field("picture", "userinfo", "picture"),
      ]),
    },
  };
}

/**
 * A finished connection test in each shape the result dialog draws.
 *
 * - `succeeded`: the provider returned an ID token without a picture and
 *   UserInfo supplied it, so one row of the mapped fields is sourced from
 *   UserInfo and both documents are present.
 * - `fallback`: a provider that returns no ID token (a GitHub OAuth App): the
 *   ID token stage is skipped, every field comes from UserInfo, the issuer from
 *   configuration, and the subject is a numeric id past a double's precision.
 * - `failed`: the code exchange fails, so there is nothing to show under the
 *   stages.
 */
export function diagnosticResult(
  outcome: MockConfig["admin"]["diagnosticOutcome"],
) {
  const before = [
    { name: "discovery", status: "succeeded", durationMs: 42 },
    { name: "authorize", status: "succeeded", durationMs: 12 },
    { name: "callback", status: "succeeded", durationMs: 8 },
  ];
  const endpoint = "https://idp1.example.test";
  if (outcome === "running") {
    return {
      status: "running",
      expiresAt: iso(600_000),
      stages: [...before.slice(0, 2), { name: "callback", status: "pending" }],
    };
  }
  if (outcome === "failed") {
    // The failure mirrors what the server can honestly report: the provider
    // refused the authorization, so the callback stage carries the
    // front-channel error code and description and nothing runs after it.
    return {
      status: "failed",
      expiresAt: iso(600_000),
      stages: [
        { name: "discovery", status: "succeeded", durationMs: 42 },
        { name: "authorize", status: "succeeded", durationMs: 12 },
        {
          name: "callback",
          status: "failed",
          durationMs: 8,
          errorCode: "invalid_request",
          errorDescription:
            "AADSTS9002346: Application 'mock-client-1' is configured for use by Microsoft Account users only. Please use the /consumers endpoint to serve this request.",
        },
      ],
    };
  }
  const exchange = {
    name: "token_exchange",
    status: "succeeded",
    durationMs: 55,
    endpoint: `${endpoint}/token`,
    httpStatus: 200,
  };
  const userinfoStage = {
    name: "userinfo",
    status: "succeeded",
    durationMs: 31,
    endpoint: `${endpoint}/userinfo`,
    httpStatus: 200,
  };
  if (outcome === "fallback") {
    const field = (value: string | boolean | null, claim: string) => ({
      value,
      source: "userinfo",
      claim,
    });
    return {
      status: "succeeded",
      expiresAt: iso(600_000),
      stages: [
        ...before,
        exchange,
        { name: "id_token", status: "skipped" },
        userinfoStage,
      ],
      identity: {
        issuer: { value: "https://github.com", source: "configuration" },
        subject: field("9007199254740993", "id"),
        username: field("octocat", "login"),
        displayName: field(null, "name"),
        email: field("octocat@example.test", "email"),
        emailVerified: field(false, "email_verified"),
        picture: field(
          "https://avatars.example.test/u/9007199254740993",
          "avatar_url",
        ),
      },
      userinfo: {
        json: '{"login":"octocat","id":9007199254740993,"node_id":"MDQ6VXNlcjE=","avatar_url":"https://avatars.example.test/u/9007199254740993","type":"User","site_admin":false,"name":null,"company":"@example","blog":"","location":"San Francisco","email":"octocat@example.test","bio":null,"public_repos":8,"created_at":"2011-01-25T18:44:36Z"}',
      },
    };
  }
  const token = (value: string | boolean | null, claim: string) => ({
    value,
    source: "id_token",
    claim,
  });
  return {
    status: "succeeded",
    expiresAt: iso(600_000),
    stages: [
      ...before,
      exchange,
      { name: "id_token", status: "succeeded", durationMs: 9 },
      userinfoStage,
    ],
    identity: {
      issuer: token(endpoint, "iss"),
      subject: token("248289761001", "sub"),
      username: token("mock-user", "preferred_username"),
      displayName: token("Mock User", "name"),
      email: token("mock-user@example.test", "email"),
      emailVerified: token(true, "email_verified"),
      picture: {
        value: `${endpoint}/avatars/mock-user.png`,
        source: "userinfo",
        claim: "picture",
      },
    },
    idToken: {
      json: `{"iss":"${endpoint}","sub":"248289761001","aud":"mock-client-1","exp":1790000600,"iat":1790000000,"auth_time":1789999990,"nonce":"n-0S6_WzA2Mj","amr":["pwd","mfa"],"preferred_username":"mock-user","name":"Mock User","email":"mock-user@example.test","email_verified":true,"groups":[],"address":{}}`,
    },
    userinfo: {
      json: `{"sub":"248289761001","name":"Mock User","preferred_username":"mock-user","email":"mock-user@example.test","email_verified":true,"picture":"${endpoint}/avatars/mock-user.png","locale":"en-US","note":"say \\"hi\\" {, }"}`,
    },
  };
}
