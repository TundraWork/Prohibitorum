import type { components } from "@/api/generated/schema";

/**
 * Management writes and the whole user-group resource, hand-written because the
 * OpenAPI schema does not cover them: Huma registers the admin *reads* and four
 * account writes, but every other admin route is a bare chi handler, and the
 * user-group resource is bare chi throughout (see `api.md`). `raw-paths.ts`
 * stays what it is — sign-in and session — so the two remain separable.
 *
 * Shapes here are taken from the Go handlers, not from `api.md`, wherever the
 * two differ; the divergences are noted on the affected entries.
 */

/** `POST /invitations` — the account exists only once the invite is consumed. */
export interface CreateInvitationRequest {
  role: string;
  attributes?: Record<string, unknown>;
  /** A disabled or unknown slug is rejected at create, not at redemption. */
  expectedUpstreamIdpSlug?: string;
  username?: string;
  groupIds?: number[];
}

/** `POST /invitations/revoke` — by opaque bearer token, never by index. */
export interface RevokeInvitationRequest {
  token: string;
}

/** `POST /accounts/set-disabled` — flips only the flag, never the profile. */
export interface SetAccountDisabledRequest {
  id: number;
  disabled: boolean;
}

/** `POST /accounts/credentials/delete` — force-revoke someone else's passkey. */
export interface DeleteAccountCredentialRequest {
  accountId: number;
  credentialId: number;
}

/** `POST /accounts/tokens/revoke` — force-revoke a personal access token. */
export interface RevokeAccountTokenRequest {
  id: number;
}

/** `POST /accounts/{id}/sessions/revoke` — one session of one account. */
export interface RevokeAccountSessionRequest {
  id: string;
}

/** `POST /accounts/revoke-sessions` — every session of one account. */
export interface RevokeAccountSessionsRequest {
  id: number;
}

export interface RevokeAccountSessionsResult {
  revoked: number;
}

/** One entry of `GET /groups/providers`; the rule editor's provider vocabulary. */
export interface ProviderDescriptorView {
  slug: string;
  displayName: string;
}

/**
 * One rule AST node, mirroring `appaccess.Condition`. Exactly one shape is ever
 * populated: a combinator (`op` plus `children` for `all`/`any`, or `child` for
 * `not`), or a leaf (`fact` plus the one value its kind uses). A `not` holds a
 * leaf and never another combinator.
 */
export interface AppAccessCondition {
  op?: string;
  children?: AppAccessCondition[];
  child?: AppAccessCondition;
  fact?: string;
  provider?: string;
  protocol?: string;
  method?: string;
  source?: string;
}

export interface AppAccessRule {
  version: number;
  condition: AppAccessCondition;
}

/**
 * One reusable global policy group. `rule` is present only for `kind: "rule"`,
 * and `applicationCount` only for an admin caller — `GET /groups` answers a
 * non-admin with their own memberships, which carry neither. `description` is
 * omitted when empty.
 */
export interface AppGroupView {
  id: number;
  kind: string;
  slug: string;
  displayName: string;
  description?: string;
  exposedToDownstream: boolean;
  rule?: AppAccessRule;
  applicationCount?: number;
}

export interface CreateGroupRequest {
  kind: string;
  slug: string;
  displayName: string;
  description: string;
  exposedToDownstream?: boolean;
  rule?: AppAccessRule;
}

/** `kind` is rejected on update: a group's kind is fixed once created. */
export interface UpdateGroupRequest {
  slug: string;
  displayName: string;
  description: string;
  exposedToDownstream?: boolean;
  /** Omitted means "keep the current rule"; a manual group must never send one. */
  rule?: AppAccessRule;
}

/** The account fields a policy screen may see: no identity facts, no secrets. */
export interface AccountSummaryView {
  id: number;
  username: string;
  displayName: string;
}

/** `POST /groups/{groupId}/decisions` — write one allow or deny. */
export interface UpsertDecisionRequest {
  accountId: number;
  effect: "allow" | "deny";
}

/** `POST /groups/{groupId}/decisions/clear` — drop one account's decision. */
export interface ClearDecisionRequest {
  accountId: number;
}

/** One per-account allow or deny in a manual group. */
export interface ManualDecisionView {
  account: AccountSummaryView;
  effect: string;
  updatedAt: string;
}

/** One account that a rule group matches, or does not. */
export interface GroupPreviewView {
  account: AccountSummaryView;
  matched: boolean;
}

export interface PageManualDecisionView {
  items: ManualDecisionView[] | null;
  nextCursor: string;
}

export interface PageGroupPreviewView {
  items: GroupPreviewView[] | null;
  nextCursor: string;
}

/** A bounded condition-result tree; it never carries the account's own facts. */
export interface ExplanationView {
  path: string;
  label: string;
  result: boolean;
  children?: ExplanationView[];
}

export interface GroupExplanationView {
  account: AccountSummaryView;
  explanation: ExplanationView;
}

/** An application that currently selects this group, for the impact view. */
export interface GroupApplicationView {
  iconUrl?: string;
  kind: string;
  appId: string;
  displayName: string;
}

export interface PageGroupApplicationView {
  items: GroupApplicationView[] | null;
  nextCursor: string;
}

/**
 * `POST /groups/rule-preview` validates an unsaved draft and pages its matches.
 * `matchedCount` is the whole-draft total, not the size of this page.
 */
export interface RulePreviewRequest {
  version: number;
  condition: AppAccessCondition;
  cursor?: string;
  limit?: number;
}

export interface RulePreviewPageView {
  items: GroupPreviewView[] | null;
  matchedCount: number;
  nextCursor: string;
}

/**
 * `GET`/`PUT /admin/settings/client-ip` — how the server finds a request's
 * address behind a proxy. The write replaces the whole policy.
 */
export interface ClientIpSettings {
  strategy: "direct" | "forwarded" | "header";
  /** Only read for `header`; kept as sent otherwise. */
  header: string;
  trustedProxies: string[];
}

/** `PUT /admin/settings/maintenance`. The message is at most 500 characters. */
export interface MaintenanceSettings {
  maintenanceMode: boolean;
  maintenanceMessage: string;
}

/**
 * The lifecycle states `pkg/protocol/oidc` moves a signing key through. The
 * generated `SigningKeyView` types `status` as a plain string.
 */
export type SigningKeyStatus =
  | "pending"
  | "active"
  | "decommissioning"
  | "retired";

type NoParameters = {
  query?: never;
  header?: never;
  path?: never;
  cookie?: never;
};

/** An empty JSON object: the sudo wrapper only accepts JSON bodies. */
type EmptyJsonBody = { content: { "application/json": Record<string, never> } };

/* ----------------------------------------------------- federation writes -- */

/**
 * The protocol a provider speaks. The server also accepts these on the wire as
 * plain strings; the view types are narrowed here so a page can switch on the
 * protocol without re-checking what it got.
 */
export type ProviderProtocol = "oidc" | "steam" | "vrchat";

/** Who the provider may create accounts for. VRChat is `link_only` by rule. */
export type ProviderMode = "auto_provision" | "invite_only" | "link_only";

/**
 * `config` for an OIDC provider. The server validates this as an exact object —
 * all fifteen keys present, none null, none extra, under 8192 bytes — so the
 * type lists them all as required rather than leaving the shape open.
 */
export interface OidcProviderConfig {
  issuerUrl: string;
  clientId: string;
  scopes: string[];
  allowedDomains: string[];
  requireVerifiedEmail: boolean;
  allowPrivateNetwork: boolean;
  usernameClaim: string;
  displayNameClaim: string;
  emailClaim: string;
  pictureClaim: string;
  subjectClaim: string;
  configurationMode: "discovery" | "manual";
  /** Null means "no override" in discovery mode, and "do not use" for userinfo. */
  endpoints: {
    authorization: string | null;
    token: string | null;
    userinfo: string | null;
    jwks: string | null;
  };
  tokenAuthMethod:
    | "discovery"
    | "client_secret_basic"
    | "client_secret_post"
    | "none";
  /** A public client (`none`) must keep this at `S256`. */
  pkceMethod: "S256" | "plain" | "off";
}

/**
 * `POST /identity-providers` and `PUT /identity-providers/{slug}`. `slug` and
 * `protocol` are create-only: the update handler ignores them, and a rename is
 * not offered. `config` is `{}` for Steam and VRChat, which take no config.
 */
export interface ProviderWriteBody {
  slug?: string;
  displayName: string;
  protocol?: ProviderProtocol;
  mode: ProviderMode;
  config: OidcProviderConfig | Record<string, never>;
  /** Required for OIDC unless the client is public; required for Steam. */
  secret?: string;
}

/** One resolved value of the effective OIDC configuration and where it came from. */
export interface EffectiveConfigField {
  value: string | string[];
  source: string;
}

export interface EffectiveConfigView {
  mode: string;
  fetchedAt: string;
  callbackUrl: string;
  fields: Record<string, EffectiveConfigField>;
}

/** One stage of a diagnostic run. `status` is pending/succeeded/skipped/failed. */
export interface DiagnosticStageView {
  name: string;
  status: string;
  durationMs?: number;
  endpoint?: string;
  httpStatus?: number;
  errorCode?: string;
  requestId?: string;
}

export interface DiagnosticResultView {
  status: string;
  expiresAt: string;
  stages: DiagnosticStageView[] | null;
  claims?: Record<string, unknown>;
}

/** `POST /identity-providers/{slug}/tests` — where to send the browser. */
export interface DiagnosticStartView {
  id: string;
  authorizationUrl: string;
  expiresAt: string;
}

/**
 * The operator-session responses. `challenge` is present while a second factor
 * is still needed; `provider` comes back only once the session is stored, and
 * carries no icon because the provider row did not change.
 */
export interface OperatorSessionView {
  status: string;
  challenge?: string;
  methods?: string[];
  expiresAt?: string;
  provider?: components["schemas"]["IdentityProviderView"];
}

export interface OperatorSessionStartRequest {
  username: string;
  password: string;
}

export interface OperatorSessionVerifyRequest {
  challenge: string;
  method: string;
  code: string;
}

/** `POST /oidc-applications` — a confidential client answers with its secret once. */
export interface CreateOidcAppRequest {
  clientId: string;
  displayName?: string;
  redirectUris: string[];
  postLogoutRedirectUris?: string[];
  scopes?: string[];
  public?: boolean;
  requireConsent?: boolean;
  requirePkce?: boolean;
  accessRestricted?: boolean;
}

export interface CreateOidcAppResponse {
  secret?: string;
  clientId: string;
  displayName: string;
}

/**
 * `PUT /oidc-applications/{clientId}` replaces the whole record: `disabled` and
 * `launchUrl` must carry the current value, because omitting either resets it
 * (enabled, and no launch URL). `update-bodies.ts` builds this from the saved
 * view plus the edited fields so no page has to remember that.
 */
export interface UpdateOidcAppRequest {
  displayName: string;
  redirectUris: string[];
  postLogoutRedirectUris: string[];
  allowedScopes: string[];
  requireConsent: boolean;
  disabled: boolean;
  launchUrl: string | null;
  requirePkce: boolean;
}

export interface RotateOidcSecretResponse {
  clientId: string;
  secret: string;
}

/** Subject source for OIDC's `sub`, and the forward-auth `Remote-User`. */
export type PrincipalSource = "sub" | "username" | "verified_email";

export interface UpdateOidcProjectionRequest {
  subjectSource: PrincipalSource;
  claimAliases: Record<string, string>;
}

export interface UpdateForwardAuthProjectionRequest {
  remoteUserSource: PrincipalSource;
}

export interface CreateForwardAuthAppRequest {
  clientId: string;
  host: string;
  displayName?: string;
  scopes?: { name: string; description?: string }[];
  accessRestricted?: boolean;
}

export interface UpdateForwardAuthAppRequest {
  displayName: string;
  host: string;
  /** The whole vocabulary; omitting it clears it. */
  scopes: { name: string; description?: string }[];
}

export interface SamlAcsEndpointRequest {
  binding: string;
  location: string;
  index: number;
  isDefault: boolean;
}

/**
 * One SAML attribute mapping. `source` is either one of the named account facts
 * or `attributes.<key>`. The server does not validate any of this, so the only
 * check a mapping ever gets is the client's.
 */
export interface SamlAttributeMapping {
  name: string;
  name_format: string;
  friendly_name?: string;
  source: string;
  multi: boolean;
}

export interface CreateSamlAppRequest {
  displayName?: string;
  accessRestricted?: boolean;
  /** Wins over `acs` when present: the server parses it instead. */
  metadataXml?: string;
  entityId?: string;
  /** Empty string means the instance default. */
  nameIdFormat?: string;
  requireSignedAuthnRequest?: boolean;
  allowIdpInitiated?: boolean;
  acs?: SamlAcsEndpointRequest[];
}

/** `PUT /saml-applications/{id}`; `attributeMap` must carry the current value. */
export interface UpdateSamlAppRequest {
  displayName: string;
  nameIdFormat: string;
  attributeMap: SamlAttributeMapping[];
  requireSignedAuthnRequest: boolean;
  allowIdpInitiated: boolean;
  /** Omitted or non-positive means no limit. */
  sessionLifetimeSecs?: number;
}

/** One account that may manage an application. */
export interface AppManagerView {
  id: number;
  username: string;
  displayName: string;
  disabled: boolean;
  assignedAt: string;
}

/** `kind` selects the resource family; `appId` is its own identifier. */
export type ManagedApplicationKind = "oidc" | "forward_auth" | "saml";

/** The protocol-neutral identity of one application, as the access API sees it. */
export interface AppSummaryView {
  iconUrl?: string;
  kind: string;
  appId: string;
  displayName: string;
  launchUrl?: string;
  entityId?: string;
  forwardAuthHost?: string;
  accessRestricted: boolean;
}

/**
 * `GET /managed-applications/{kind}/{appId}/access` — the workspace the access
 * panel reads. The admin path answers the same shape, which is why the panel is
 * one component for all three protocols.
 */
export interface AppAccessWorkspace {
  app: AppSummaryView;
  accessRestricted: boolean;
  providers: ProviderDescriptorView[];
  groups: AppGroupView[];
}

export interface SetAppAccessRestrictedRequest {
  restricted: boolean;
}

export interface ReplaceAppGroupsRequest {
  groupIds: number[];
}

/** What every client-shaped route carries: the OIDC / forward-auth client id. */
export interface ClientKindBody {
  clientId: string;
}

export interface RawAdminPaths {
  "/api/prohibitorum/accounts/set-disabled": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: {
        content: { "application/json": SetAccountDisabledRequest };
      };
      responses: {
        200: {
          content: { "application/json": components["schemas"]["AccountView"] };
        };
      };
    };
  };
  "/api/prohibitorum/accounts/credentials/delete": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: {
        content: { "application/json": DeleteAccountCredentialRequest };
      };
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/accounts/tokens/revoke": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: {
        content: { "application/json": RevokeAccountTokenRequest };
      };
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/accounts/{id}/sessions/revoke": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { id: number };
        cookie?: never;
      };
      requestBody: {
        content: { "application/json": RevokeAccountSessionRequest };
      };
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/accounts/revoke-sessions": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: {
        content: { "application/json": RevokeAccountSessionsRequest };
      };
      responses: {
        200: {
          content: { "application/json": RevokeAccountSessionsResult };
        };
      };
    };
  };
  "/api/prohibitorum/groups": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        /** A bare array, not a `{ items, nextCursor }` envelope. */
        200: { content: { "application/json": AppGroupView[] } };
      };
    };
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: { content: { "application/json": CreateGroupRequest } };
      responses: {
        201: { content: { "application/json": AppGroupView } };
      };
    };
  };
  "/api/prohibitorum/groups/providers": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        /** A bare array. */
        200: { content: { "application/json": ProviderDescriptorView[] } };
      };
    };
  };
  "/api/prohibitorum/groups/rule-preview": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: { content: { "application/json": RulePreviewRequest } };
      responses: {
        200: { content: { "application/json": RulePreviewPageView } };
      };
    };
  };
  "/api/prohibitorum/groups/{groupId}": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path: { groupId: number };
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": AppGroupView } };
      };
    };
    put: {
      parameters: {
        query?: never;
        header?: never;
        path: { groupId: number };
        cookie?: never;
      };
      requestBody: { content: { "application/json": UpdateGroupRequest } };
      responses: {
        200: { content: { "application/json": AppGroupView } };
      };
    };
  };
  "/api/prohibitorum/groups/{groupId}/delete": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { groupId: number };
        cookie?: never;
      };
      requestBody?: never;
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/groups/{groupId}/decisions": {
    get: {
      parameters: {
        query?: { cursor?: string; limit?: number };
        header?: never;
        path: { groupId: number };
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": PageManualDecisionView } };
      };
    };
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { groupId: number };
        cookie?: never;
      };
      requestBody: { content: { "application/json": UpsertDecisionRequest } };
      responses: {
        200: { content: { "application/json": ManualDecisionView } };
      };
    };
  };
  "/api/prohibitorum/groups/{groupId}/decisions/clear": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { groupId: number };
        cookie?: never;
      };
      requestBody: { content: { "application/json": ClearDecisionRequest } };
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/groups/{groupId}/preview": {
    get: {
      parameters: {
        query?: { limit?: number };
        header?: never;
        path: { groupId: number };
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": PageGroupPreviewView } };
      };
    };
  };
  "/api/prohibitorum/groups/{groupId}/explain/{accountId}": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path: { groupId: number; accountId: number };
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": GroupExplanationView } };
      };
    };
  };
  "/api/prohibitorum/groups/{groupId}/applications": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path: { groupId: number };
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": PageGroupApplicationView } };
      };
    };
  };
  "/api/prohibitorum/signing-keys/generate": {
    post: {
      parameters: NoParameters;
      requestBody: EmptyJsonBody;
      responses: {
        201: {
          content: {
            "application/json": components["schemas"]["SigningKeyView"];
          };
        };
      };
    };
  };
  /**
   * Only a `pending` key can be activated. Any other state, like an unknown
   * kid, answers 404 `credential_not_found` — not the 409 `api.md` used to say.
   */
  "/api/prohibitorum/signing-keys/{kid}/activate": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { kid: string };
        cookie?: never;
      };
      requestBody: EmptyJsonBody;
      responses: {
        200: {
          content: {
            "application/json": components["schemas"]["SigningKeyView"];
          };
        };
      };
    };
  };
  /**
   * Always sets `retire_after` to now plus the grace period, so on a key that
   * is already decommissioning it postpones retirement. The active key answers
   * 409 `active_key_no_replacement`.
   */
  "/api/prohibitorum/signing-keys/{kid}/retire": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { kid: string };
        cookie?: never;
      };
      requestBody: EmptyJsonBody;
      responses: {
        200: {
          content: {
            "application/json": components["schemas"]["SigningKeyView"];
          };
        };
      };
    };
  };
  /** An empty name clears the override and falls back to the configured one. */
  "/api/prohibitorum/admin/settings": {
    put: {
      parameters: NoParameters;
      requestBody: {
        content: { "application/json": { instanceName: string } };
      };
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/admin/settings/maintenance": {
    put: {
      parameters: NoParameters;
      requestBody: {
        content: { "application/json": MaintenanceSettings };
      };
      responses: { 204: { content?: never } };
    };
  };
  /**
   * Raw image bytes, not multipart, like the avatar upload. The handler checks
   * sudo itself, because the sudo wrapper only accepts JSON bodies.
   */
  "/api/prohibitorum/admin/settings/icon": {
    put: {
      parameters: NoParameters;
      requestBody: { content: { "application/octet-stream": Blob } };
      responses: { 204: { content?: never } };
    };
    delete: {
      parameters: NoParameters;
      requestBody?: never;
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/admin/settings/background": {
    put: {
      parameters: NoParameters;
      requestBody: { content: { "application/octet-stream": Blob } };
      responses: { 204: { content?: never } };
    };
    delete: {
      parameters: NoParameters;
      requestBody?: never;
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/admin/settings/client-ip": {
    get: {
      parameters: NoParameters;
      requestBody?: never;
      responses: {
        200: { content: { "application/json": ClientIpSettings } };
      };
    };
    put: {
      parameters: NoParameters;
      requestBody: { content: { "application/json": ClientIpSettings } };
      responses: { 204: { content?: never } };
    };
  };

  /* -------------------------------------------------- identity providers -- */

  "/api/prohibitorum/identity-providers": {
    post: {
      parameters: NoParameters;
      requestBody: { content: { "application/json": ProviderWriteBody } };
      responses: {
        201: {
          content: {
            "application/json": components["schemas"]["IdentityProviderView"];
          };
        };
      };
    };
  };
  "/api/prohibitorum/identity-providers/{slug}": {
    put: {
      parameters: {
        query?: never;
        header?: never;
        path: { slug: string };
        cookie?: never;
      };
      requestBody: { content: { "application/json": ProviderWriteBody } };
      responses: {
        200: {
          content: {
            "application/json": components["schemas"]["IdentityProviderView"];
          };
        };
      };
    };
  };
  "/api/prohibitorum/identity-providers/rotate-secret": {
    post: {
      parameters: NoParameters;
      requestBody: {
        content: { "application/json": { slug: string; secret: string } };
      };
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/identity-providers/set-disabled": {
    post: {
      parameters: NoParameters;
      requestBody: {
        content: { "application/json": { slug: string; disabled: boolean } };
      };
      responses: {
        200: {
          content: {
            "application/json": components["schemas"]["IdentityProviderView"];
          };
        };
      };
    };
  };
  "/api/prohibitorum/identity-providers/delete": {
    post: {
      parameters: NoParameters;
      requestBody: { content: { "application/json": { slug: string } } };
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/identity-providers/{slug}/icon": {
    put: {
      parameters: {
        query?: never;
        header?: never;
        path: { slug: string };
        cookie?: never;
      };
      requestBody: { content: { "application/octet-stream": Blob } };
      responses: { 204: { content?: never } };
    };
    delete: {
      parameters: {
        query?: never;
        header?: never;
        path: { slug: string };
        cookie?: never;
      };
      requestBody?: never;
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/identity-providers/{slug}/effective-config": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path: { slug: string };
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": EffectiveConfigView } };
      };
    };
  };
  "/api/prohibitorum/identity-providers/{slug}/tests": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { slug: string };
        cookie?: never;
      };
      requestBody: EmptyJsonBody;
      responses: {
        200: { content: { "application/json": DiagnosticStartView } };
      };
    };
  };
  "/api/prohibitorum/identity-providers/{slug}/tests/{id}": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path: { slug: string; id: string };
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": DiagnosticResultView } };
      };
    };
  };
  "/api/prohibitorum/identity-providers/{slug}/tests/{id}/complete": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { slug: string; id: string };
        cookie?: never;
      };
      requestBody: EmptyJsonBody;
      responses: {
        200: { content: { "application/json": DiagnosticResultView } };
      };
    };
  };
  "/api/prohibitorum/identity-providers/{slug}/operator-session/start": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { slug: string };
        cookie?: never;
      };
      requestBody: {
        content: { "application/json": OperatorSessionStartRequest };
      };
      responses: {
        200: { content: { "application/json": OperatorSessionView } };
      };
    };
  };
  "/api/prohibitorum/identity-providers/{slug}/operator-session/verify": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { slug: string };
        cookie?: never;
      };
      requestBody: {
        content: { "application/json": OperatorSessionVerifyRequest };
      };
      responses: {
        200: { content: { "application/json": OperatorSessionView } };
      };
    };
  };
  "/api/prohibitorum/identity-providers/{slug}/operator-session/validate": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { slug: string };
        cookie?: never;
      };
      requestBody: EmptyJsonBody;
      responses: {
        200: { content: { "application/json": OperatorSessionView } };
      };
    };
  };

  /* --------------------------------------------------- OIDC applications -- */

  "/api/prohibitorum/oidc-applications": {
    post: {
      parameters: NoParameters;
      requestBody: { content: { "application/json": CreateOidcAppRequest } };
      responses: {
        201: { content: { "application/json": CreateOidcAppResponse } };
      };
    };
  };
  "/api/prohibitorum/oidc-applications/{clientId}": {
    put: {
      parameters: {
        query?: never;
        header?: never;
        path: { clientId: string };
        cookie?: never;
      };
      requestBody: { content: { "application/json": UpdateOidcAppRequest } };
      responses: {
        200: {
          content: {
            "application/json": components["schemas"]["OIDCApplicationView"];
          };
        };
      };
    };
  };
  "/api/prohibitorum/oidc-applications/{clientId}/identity-projection": {
    put: {
      parameters: {
        query?: never;
        header?: never;
        path: { clientId: string };
        cookie?: never;
      };
      requestBody: {
        content: { "application/json": UpdateOidcProjectionRequest };
      };
      responses: {
        200: {
          content: {
            "application/json": components["schemas"]["OIDCApplicationView"];
          };
        };
      };
    };
  };
  "/api/prohibitorum/oidc-applications/rotate-secret": {
    post: {
      parameters: NoParameters;
      requestBody: { content: { "application/json": ClientKindBody } };
      responses: {
        200: { content: { "application/json": RotateOidcSecretResponse } };
      };
    };
  };
  "/api/prohibitorum/oidc-applications/set-disabled": {
    post: {
      parameters: NoParameters;
      requestBody: {
        content: {
          "application/json": { clientId: string; disabled: boolean };
        };
      };
      responses: {
        200: {
          content: {
            "application/json": components["schemas"]["OIDCApplicationView"];
          };
        };
      };
    };
  };
  "/api/prohibitorum/oidc-applications/delete": {
    post: {
      parameters: NoParameters;
      requestBody: { content: { "application/json": ClientKindBody } };
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/oidc-applications/{clientId}/icon": {
    put: {
      parameters: {
        query?: never;
        header?: never;
        path: { clientId: string };
        cookie?: never;
      };
      requestBody: { content: { "application/octet-stream": Blob } };
      responses: { 204: { content?: never } };
    };
    delete: {
      parameters: {
        query?: never;
        header?: never;
        path: { clientId: string };
        cookie?: never;
      };
      requestBody?: never;
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/oidc-applications/{clientId}/managers": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path: { clientId: string };
        cookie?: never;
      };
      requestBody?: never;
      responses: { 200: { content: { "application/json": AppManagerView[] } } };
    };
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { clientId: string };
        cookie?: never;
      };
      requestBody: { content: { "application/json": { accountId: number } } };
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/oidc-applications/{clientId}/managers/remove": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { clientId: string };
        cookie?: never;
      };
      requestBody: { content: { "application/json": { accountId: number } } };
      responses: { 204: { content?: never } };
    };
  };

  /* -------------------------------------------- forward-auth applications -- */

  "/api/prohibitorum/forward-auth-apps": {
    post: {
      parameters: NoParameters;
      requestBody: {
        content: { "application/json": CreateForwardAuthAppRequest };
      };
      responses: {
        201: {
          content: {
            "application/json": components["schemas"]["ForwardAuthAppView"];
          };
        };
      };
    };
  };
  "/api/prohibitorum/forward-auth-apps/{clientId}": {
    put: {
      parameters: {
        query?: never;
        header?: never;
        path: { clientId: string };
        cookie?: never;
      };
      requestBody: {
        content: { "application/json": UpdateForwardAuthAppRequest };
      };
      responses: {
        200: {
          content: {
            "application/json": components["schemas"]["ForwardAuthAppView"];
          };
        };
      };
    };
  };
  "/api/prohibitorum/forward-auth-apps/{clientId}/identity-projection": {
    put: {
      parameters: {
        query?: never;
        header?: never;
        path: { clientId: string };
        cookie?: never;
      };
      requestBody: {
        content: { "application/json": UpdateForwardAuthProjectionRequest };
      };
      responses: {
        200: {
          content: {
            "application/json": components["schemas"]["ForwardAuthAppView"];
          };
        };
      };
    };
  };
  "/api/prohibitorum/forward-auth-apps/set-disabled": {
    post: {
      parameters: NoParameters;
      requestBody: {
        content: {
          "application/json": { clientId: string; disabled: boolean };
        };
      };
      responses: {
        200: {
          content: {
            "application/json": components["schemas"]["ForwardAuthAppView"];
          };
        };
      };
    };
  };
  "/api/prohibitorum/forward-auth-apps/delete": {
    post: {
      parameters: NoParameters;
      requestBody: { content: { "application/json": ClientKindBody } };
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/forward-auth-apps/{clientId}/icon": {
    put: {
      parameters: {
        query?: never;
        header?: never;
        path: { clientId: string };
        cookie?: never;
      };
      requestBody: { content: { "application/octet-stream": Blob } };
      responses: { 204: { content?: never } };
    };
    delete: {
      parameters: {
        query?: never;
        header?: never;
        path: { clientId: string };
        cookie?: never;
      };
      requestBody?: never;
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/forward-auth-apps/{clientId}/managers": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path: { clientId: string };
        cookie?: never;
      };
      requestBody?: never;
      responses: { 200: { content: { "application/json": AppManagerView[] } } };
    };
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { clientId: string };
        cookie?: never;
      };
      requestBody: { content: { "application/json": { accountId: number } } };
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/forward-auth-apps/{clientId}/managers/remove": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { clientId: string };
        cookie?: never;
      };
      requestBody: { content: { "application/json": { accountId: number } } };
      responses: { 204: { content?: never } };
    };
  };

  /* --------------------------------------------------- SAML applications -- */

  "/api/prohibitorum/saml-applications": {
    post: {
      parameters: NoParameters;
      requestBody: { content: { "application/json": CreateSamlAppRequest } };
      responses: {
        201: {
          content: {
            "application/json": components["schemas"]["SAMLApplicationView"];
          };
        };
      };
    };
  };
  "/api/prohibitorum/saml-applications/{id}": {
    put: {
      parameters: {
        query?: never;
        header?: never;
        path: { id: number };
        cookie?: never;
      };
      requestBody: { content: { "application/json": UpdateSamlAppRequest } };
      responses: {
        200: {
          content: {
            "application/json": components["schemas"]["SAMLApplicationView"];
          };
        };
      };
    };
  };
  "/api/prohibitorum/saml-applications/{id}/reingest-metadata": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { id: number };
        cookie?: never;
      };
      requestBody: { content: { "application/json": { metadataXml: string } } };
      responses: {
        200: {
          content: {
            "application/json": components["schemas"]["SAMLApplicationView"];
          };
        };
      };
    };
  };
  "/api/prohibitorum/saml-applications/set-disabled": {
    post: {
      parameters: NoParameters;
      requestBody: {
        content: { "application/json": { id: number; disabled: boolean } };
      };
      responses: {
        200: {
          content: {
            "application/json": components["schemas"]["SAMLApplicationView"];
          };
        };
      };
    };
  };
  "/api/prohibitorum/saml-applications/delete": {
    post: {
      parameters: NoParameters;
      requestBody: { content: { "application/json": { id: number } } };
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/saml-applications/{id}/icon": {
    put: {
      parameters: {
        query?: never;
        header?: never;
        path: { id: number };
        cookie?: never;
      };
      requestBody: { content: { "application/octet-stream": Blob } };
      responses: { 204: { content?: never } };
    };
    delete: {
      parameters: {
        query?: never;
        header?: never;
        path: { id: number };
        cookie?: never;
      };
      requestBody?: never;
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/saml-applications/{id}/managers": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path: { id: number };
        cookie?: never;
      };
      requestBody?: never;
      responses: { 200: { content: { "application/json": AppManagerView[] } } };
    };
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { id: number };
        cookie?: never;
      };
      requestBody: { content: { "application/json": { accountId: number } } };
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/saml-applications/{id}/managers/remove": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { id: number };
        cookie?: never;
      };
      requestBody: { content: { "application/json": { accountId: number } } };
      responses: { 204: { content?: never } };
    };
  };

  /* ------------------------------------------------ managed applications -- */

  "/api/prohibitorum/managed-applications/{kind}/{appId}/access": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path: { kind: ManagedApplicationKind; appId: string };
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": AppAccessWorkspace } };
      };
    };
  };
  "/api/prohibitorum/managed-applications/{kind}/{appId}/access/set-restricted": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { kind: ManagedApplicationKind; appId: string };
        cookie?: never;
      };
      requestBody: {
        content: { "application/json": SetAppAccessRestrictedRequest };
      };
      responses: {
        200: { content: { "application/json": AppSummaryView } };
      };
    };
  };
  "/api/prohibitorum/managed-applications/{kind}/{appId}/groups": {
    put: {
      parameters: {
        query?: never;
        header?: never;
        path: { kind: ManagedApplicationKind; appId: string };
        cookie?: never;
      };
      requestBody: { content: { "application/json": ReplaceAppGroupsRequest } };
      responses: {
        200: { content: { "application/json": AppGroupView[] } };
      };
    };
  };
}
