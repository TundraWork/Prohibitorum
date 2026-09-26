import {
  defaultOidcProviderConfig,
  readAttributeMap,
  readOidcProviderConfig,
  readPrincipalSource,
  readProviderMode,
} from "@/api/federation";
import type { components } from "@/api/generated/schema";
import type {
  PrincipalSource,
  ProviderWriteBody,
  SamlAttributeMapping,
  UpdateForwardAuthAppRequest,
  UpdateOidcAppRequest,
  UpdateSamlAppRequest,
} from "@/api/raw-admin-paths";

/**
 * Whole-record request bodies, built in one place.
 *
 * Every write on the federation and application pages is a PUT that replaces the
 * record: the server does not merge, so omitting a field is not "leave it alone"
 * — it is "set it to the zero value". Omit `disabled` from an OIDC application
 * update and it is re-enabled; omit `launchUrl` and it is cleared; omit a SAML
 * application's `attributeMap` and every mapping is gone; omit a forward-auth
 * application's `scopes` and its vocabulary is empty.
 *
 * Each form therefore edits one section and submits the record it did not touch
 * unchanged. Building that merge here, once per resource, means a page cannot
 * forget it, and the tests below can assert the carried fields rather than each
 * page re-proving the same thing.
 */

type IdentityProviderView = components["schemas"]["IdentityProviderView"];
type OIDCApplicationView = components["schemas"]["OIDCApplicationView"];
type ForwardAuthAppView = components["schemas"]["ForwardAuthAppView"];
type SAMLApplicationView = components["schemas"]["SAMLApplicationView"];

/** The fields a provider form edits; anything absent is taken from the record. */
export interface IdentityProviderPatch {
  displayName?: string;
  mode?: ProviderWriteBody["mode"];
  config?: ProviderWriteBody["config"];
}

/**
 * `PUT /identity-providers/{slug}`. `slug` and `protocol` are not sent: the
 * server ignores both on update and neither is editable. A provider whose config
 * cannot be read falls back to the defaults rather than sending `{}`, which the
 * server rejects for an OIDC provider.
 */
export function identityProviderUpdateBody(
  view: IdentityProviderView,
  patch: IdentityProviderPatch = {},
): ProviderWriteBody {
  const config =
    patch.config ??
    (view.protocol === "oidc"
      ? (readOidcProviderConfig(view.config) ?? defaultOidcProviderConfig())
      : {});
  return {
    displayName: patch.displayName ?? view.displayName,
    mode: patch.mode ?? readProviderMode(view.mode) ?? "invite_only",
    config,
  };
}

/** The fields an OIDC application form edits. */
export interface OidcAppPatch {
  displayName?: string;
  redirectUris?: string[];
  postLogoutRedirectUris?: string[];
  allowedScopes?: string[];
  requireConsent?: boolean;
  requirePkce?: boolean;
  disabled?: boolean;
  launchUrl?: string | null;
}

/**
 * `PUT /oidc-applications/{clientId}`. `disabled` and `launchUrl` are always
 * carried: the danger section flipping the flag and the general form changing a
 * redirect URI submit the same body, and each must leave the other's field alone.
 */
export function oidcAppUpdateBody(
  view: OIDCApplicationView,
  patch: OidcAppPatch = {},
): UpdateOidcAppRequest {
  return {
    displayName: patch.displayName ?? view.displayName,
    redirectUris: patch.redirectUris ?? view.redirectUris ?? [],
    postLogoutRedirectUris:
      patch.postLogoutRedirectUris ?? view.postLogoutRedirectUris ?? [],
    allowedScopes: patch.allowedScopes ?? view.allowedScopes ?? [],
    requireConsent: patch.requireConsent ?? view.requireConsent,
    requirePkce: patch.requirePkce ?? view.requirePkce,
    disabled: patch.disabled ?? view.disabled,
    // An explicit null clears the launch URL; undefined means "unchanged".
    launchUrl:
      patch.launchUrl !== undefined
        ? patch.launchUrl
        : (view.launchUrl ?? null),
  };
}

/** The fields a forward-auth application form edits. */
export interface ForwardAuthAppPatch {
  displayName?: string;
  host?: string;
  scopes?: { name: string; description?: string }[];
}

/** `PUT /forward-auth-apps/{clientId}`. `scopes` carries the whole vocabulary. */
export function forwardAuthAppUpdateBody(
  view: ForwardAuthAppView,
  patch: ForwardAuthAppPatch = {},
): UpdateForwardAuthAppRequest {
  return {
    displayName: patch.displayName ?? view.displayName,
    host: patch.host ?? view.forwardAuthHost,
    scopes:
      patch.scopes ??
      (view.scopes ?? []).map((scope) => ({
        name: scope.name,
        ...(scope.description === undefined
          ? {}
          : { description: scope.description }),
      })),
  };
}

/** The fields a SAML application form edits. */
export interface SamlAppPatch {
  displayName?: string;
  nameIdFormat?: string;
  attributeMap?: SamlAttributeMapping[];
  requireSignedAuthnRequest?: boolean;
  allowIdpInitiated?: boolean;
  /** Minutes, as the form collects them; `null` means "no limit". */
  sessionLifetimeMinutes?: number | null;
}

/**
 * `PUT /saml-applications/{id}`. `attributeMap` is always carried, and the
 * session lifetime is converted from the form's minutes to the wire's seconds —
 * an empty field omits the key, which the server reads as "no limit".
 */
export function samlAppUpdateBody(
  view: SAMLApplicationView,
  patch: SamlAppPatch = {},
): UpdateSamlAppRequest {
  const minutes =
    patch.sessionLifetimeMinutes === undefined
      ? view.sessionLifetimeSecs === undefined ||
        view.sessionLifetimeSecs === null
        ? null
        : view.sessionLifetimeSecs / 60
      : patch.sessionLifetimeMinutes;

  return {
    displayName: patch.displayName ?? view.displayName,
    nameIdFormat: patch.nameIdFormat ?? view.nameIdFormat,
    attributeMap: patch.attributeMap ?? readAttributeMap(view.attributeMap),
    requireSignedAuthnRequest:
      patch.requireSignedAuthnRequest ?? view.requireSignedAuthnRequest,
    allowIdpInitiated: patch.allowIdpInitiated ?? view.allowIdpInitiated,
    ...(minutes === null || minutes <= 0
      ? {}
      : { sessionLifetimeSecs: Math.round(minutes * 60) }),
  };
}

/**
 * The default subject source for a record that carries none. The server answers
 * `sub` for OIDC and `username` for forward auth, and this is what the console
 * shows when a record predates the field.
 */
export function readViewPrincipalSource(
  value: unknown,
  fallback: PrincipalSource,
): PrincipalSource {
  return readPrincipalSource(value) ?? fallback;
}
