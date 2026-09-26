import type {
  OidcProviderConfig,
  PrincipalSource,
  ProviderMode,
  ProviderProtocol,
  SamlAttributeMapping,
} from "@/api/raw-admin-paths";

/**
 * Reads and narrows the loose fields the server sends.
 *
 * The OpenAPI schema types the provider's `config` as `unknown`, `mode` and
 * `protocol` as plain strings, and a SAML application's `attributeMap` as
 * `unknown`, because the Go side hands Huma a `json.RawMessage`. Narrowing them
 * here — rather than casting at each call site — means a page can switch on the
 * protocol and trust the shape, and one unexpected value is reported as such
 * instead of being rendered as a half-empty form.
 *
 * These live outside `raw-admin-paths.ts` because that file is types only: a
 * page imports a type from it and a function from here, and neither pulls in the
 * other's weight.
 */

const providerModes: readonly ProviderMode[] = [
  "auto_provision",
  "invite_only",
  "link_only",
];

const providerProtocols: readonly ProviderProtocol[] = [
  "oidc",
  "steam",
  "vrchat",
];

const principalSources: readonly PrincipalSource[] = [
  "sub",
  "username",
  "verified_email",
];

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isStringArray(value: unknown): value is string[] {
  return (
    Array.isArray(value) && value.every((item) => typeof item === "string")
  );
}

function isOneOf<T extends string>(
  value: unknown,
  allowed: readonly T[],
): value is T {
  return (
    typeof value === "string" && (allowed as readonly string[]).includes(value)
  );
}

/** A protocol string the console knows how to render. */
export function readProviderProtocol(
  value: unknown,
): ProviderProtocol | undefined {
  return isOneOf(value, providerProtocols) ? value : undefined;
}

/** A provisioning mode the console knows how to render. */
export function readProviderMode(value: unknown): ProviderMode | undefined {
  return isOneOf(value, providerModes) ? value : undefined;
}

/** A `sub`-style principal source; unknown values read as absent. */
export function readPrincipalSource(
  value: unknown,
): PrincipalSource | undefined {
  return isOneOf(value, principalSources) ? value : undefined;
}

function nullableString(value: unknown): string | null | undefined {
  if (value === null) return null;
  return typeof value === "string" ? value : undefined;
}

/**
 * Narrows a provider's `config` into the exact fifteen-key object the server
 * validates. Returns null for anything that is not a complete OIDC config —
 * including the `{}` a Steam or VRChat provider carries — so the caller treats
 * "not an OIDC config" and "an OIDC config we cannot read" the same way.
 */
export function readOidcProviderConfig(
  value: unknown,
): OidcProviderConfig | null {
  if (!isRecord(value)) return null;
  const endpoints = value.endpoints;
  if (!isRecord(endpoints)) return null;

  const scalars = {
    issuerUrl: value.issuerUrl,
    clientId: value.clientId,
    usernameClaim: value.usernameClaim,
    displayNameClaim: value.displayNameClaim,
    emailClaim: value.emailClaim,
    pictureClaim: value.pictureClaim,
    subjectClaim: value.subjectClaim,
  };
  for (const field of Object.values(scalars)) {
    if (typeof field !== "string") return null;
  }
  if (
    typeof value.requireVerifiedEmail !== "boolean" ||
    typeof value.allowPrivateNetwork !== "boolean" ||
    !isStringArray(value.scopes) ||
    !isStringArray(value.allowedDomains) ||
    !isOneOf(value.configurationMode, ["discovery", "manual"] as const) ||
    !isOneOf(value.tokenAuthMethod, [
      "discovery",
      "client_secret_basic",
      "client_secret_post",
      "none",
    ] as const) ||
    !isOneOf(value.pkceMethod, ["S256", "plain", "off"] as const)
  ) {
    return null;
  }

  const authorization = nullableString(endpoints.authorization);
  const token = nullableString(endpoints.token);
  const userinfo = nullableString(endpoints.userinfo);
  const jwks = nullableString(endpoints.jwks);
  if (
    authorization === undefined ||
    token === undefined ||
    userinfo === undefined ||
    jwks === undefined
  ) {
    return null;
  }

  return {
    issuerUrl: value.issuerUrl as string,
    clientId: value.clientId as string,
    scopes: value.scopes,
    allowedDomains: value.allowedDomains,
    requireVerifiedEmail: value.requireVerifiedEmail,
    allowPrivateNetwork: value.allowPrivateNetwork,
    usernameClaim: value.usernameClaim as string,
    displayNameClaim: value.displayNameClaim as string,
    emailClaim: value.emailClaim as string,
    pictureClaim: value.pictureClaim as string,
    subjectClaim: value.subjectClaim as string,
    configurationMode: value.configurationMode,
    endpoints: { authorization, token, userinfo, jwks },
    tokenAuthMethod: value.tokenAuthMethod,
    pkceMethod: value.pkceMethod,
  };
}

/**
 * The claims a new OIDC provider starts with, and what the console falls back to
 * when a provider's config cannot be read. These are the server's own defaults,
 * so an unreadable config still produces a form that would save something valid.
 */
export function defaultOidcProviderConfig(): OidcProviderConfig {
  return {
    issuerUrl: "",
    clientId: "",
    scopes: ["openid", "profile", "email"],
    allowedDomains: [],
    requireVerifiedEmail: true,
    allowPrivateNetwork: false,
    usernameClaim: "preferred_username",
    displayNameClaim: "name",
    emailClaim: "email",
    pictureClaim: "picture",
    subjectClaim: "sub",
    configurationMode: "discovery",
    endpoints: { authorization: null, token: null, userinfo: null, jwks: null },
    tokenAuthMethod: "discovery",
    pkceMethod: "S256",
  };
}

/**
 * Narrows a SAML application's `attributeMap`. Anything that is not an array of
 * well-formed mappings reads as empty: the server never validates these, so a
 * malformed one would only surface as a broken sign-in later.
 */
export function readAttributeMap(value: unknown): SamlAttributeMapping[] {
  if (!Array.isArray(value)) return [];
  const mappings: SamlAttributeMapping[] = [];
  for (const item of value) {
    if (!isRecord(item)) return [];
    if (typeof item.name !== "string" || typeof item.name_format !== "string") {
      return [];
    }
    if (typeof item.source !== "string" || typeof item.multi !== "boolean") {
      return [];
    }
    if (
      item.friendly_name !== undefined &&
      typeof item.friendly_name !== "string"
    ) {
      return [];
    }
    mappings.push({
      name: item.name,
      name_format: item.name_format,
      ...(item.friendly_name === undefined
        ? {}
        : { friendly_name: item.friendly_name }),
      source: item.source,
      multi: item.multi,
    });
  }
  return mappings;
}

/** Narrows a claim-alias map: output name to source claim. */
export function readClaimAliases(value: unknown): Record<string, string> {
  if (!isRecord(value)) return {};
  const aliases: Record<string, string> = {};
  for (const [key, source] of Object.entries(value)) {
    if (typeof source !== "string") return {};
    aliases[key] = source;
  }
  return aliases;
}

/**
 * The claim names an alias may read from, and the reserved output names it may
 * not take. Mirrors the server's own lists (`handle_admin_oidc_clients.go`); the
 * client's check is the only one the reader sees before saving.
 */
export const aliasSourceClaims = [
  "name",
  "preferred_username",
  "email",
  "picture",
] as const;

export const reservedAliasNames: readonly string[] = [
  "iss",
  "sub",
  "aud",
  "exp",
  "iat",
  "auth_time",
  "sid",
  "amr",
  "nonce",
  "acr",
  "at_hash",
  "azp",
];
