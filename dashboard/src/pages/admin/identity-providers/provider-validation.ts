import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import type { OidcProviderConfig } from "@/api/raw-admin-paths";
import type {
  ClientAuthMethod,
  EndpointName,
} from "@/pages/admin/identity-providers/provider-options";
import { endpointNames } from "@/pages/admin/identity-providers/provider-options";

/**
 * The checks an identity provider's forms make before anything is sent.
 *
 * They mirror the server (`federationoidc.validateConfig`, `validateEndpoint`,
 * `ValidateOutboundURL`), which answers every one of these with a bare
 * `bad_request` and no field. A mistake the server catches is therefore one the
 * reader is never told the location of, so the console has to catch it first
 * and say which field is wrong. The create page and the detail page share these
 * so the two cannot disagree about what a provider may hold.
 *
 * Values are checked as typed. A scope, a domain or an address with a space at
 * one end is refused rather than trimmed, since trimming would save something
 * other than what the reader sees.
 */

const spaces = msg({
  id: "admin.federation.value.spaces",
  message: "Remove the spaces at either end.",
});

const duplicate = msg({
  id: "admin.federation.value.duplicate",
  message: "Already in the list.",
});

const scopeCharacters = msg({
  id: "admin.federation.scope.invalid",
  message: "A scope cannot contain spaces, quotes or backslashes.",
});

export const openidRequired = msg({
  id: "admin.federation.scope.openid-required",
  message: "The openid scope is required.",
});

const domainInvalid = msg({
  id: "admin.federation.claims.domains.invalid",
  message: "Enter a domain such as example.com, without a scheme or a path.",
});

const draftPending = msg({
  id: "admin.federation.tags.draft-pending",
  message: "Press Enter to add it, or clear the box.",
});

/**
 * A list of short values edited as tags, with the text still in its input.
 *
 * The draft is part of the value so a submit can see it: text typed and never
 * added is a mistake worth stopping for, and the form cannot report what it
 * does not hold.
 */
export interface TagListValue {
  tags: string[];
  draft: string;
}

export function tagListValue(tags: readonly string[]): TagListValue {
  return { tags: [...tags], draft: "" };
}

/** Why a list cannot be saved yet: text in its input that was never added. */
export function tagDraftProblem(
  value: TagListValue,
): MessageDescriptor | undefined {
  return value.draft === "" ? undefined : draftPending;
}

/** RFC 6749 `scope-token`: no whitespace, no `"`, no `\`. */
export function scopeProblem(
  value: string,
  tags: readonly string[],
): MessageDescriptor | undefined {
  if (value !== value.trim()) return spaces;
  if (/[\s"\\]/.test(value)) return scopeCharacters;
  if (tags.includes(value)) return duplicate;
  return undefined;
}

/** Everything a submit checks about the scopes list. */
export function scopesProblem(
  value: TagListValue,
): MessageDescriptor | undefined {
  return (
    tagDraftProblem(value) ??
    (value.tags.includes("openid") ? undefined : openidRequired)
  );
}

/** A bare domain: labels of letters, digits and hyphens, at least one dot. */
function isDomain(value: string): boolean {
  return /^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$/i.test(value);
}

export function domainProblem(
  value: string,
  tags: readonly string[],
): MessageDescriptor | undefined {
  if (value !== value.trim()) return spaces;
  if (!isDomain(value)) return domainInvalid;
  if (tags.includes(value)) return duplicate;
  return undefined;
}

const urlInvalid = msg({
  id: "admin.federation.url.invalid",
  message: "Enter a full address, such as https://idp.example.com.",
});

const urlHttps = msg({
  id: "admin.federation.url.https",
  message: "Use an https address.",
});

const urlScheme = msg({
  id: "admin.federation.url.scheme",
  message: "Use an http or https address.",
});

const urlFragment = msg({
  id: "admin.federation.url.fragment",
  message: "Remove the part from # onwards.",
});

const urlUserinfo = msg({
  id: "admin.federation.url.userinfo",
  message: "Remove the user name and password from the address.",
});

const urlIpLiteral = msg({
  id: "admin.federation.url.ip",
  message: "Use a host name, not an IP address.",
});

/**
 * Why an issuer or endpoint address would be refused, or undefined.
 *
 * Every address the server fetches has a host, no credentials and no fragment.
 * Unless the provider may reach a private network — an option the console does
 * not offer, and which is off by default — it must also be `https` and name its
 * host rather than an IP address, since the server will not connect to a
 * literal address it cannot screen.
 */
export function endpointUrlProblem(
  value: string,
  allowPrivateNetwork: boolean,
): MessageDescriptor | undefined {
  if (value !== value.trim()) return spaces;
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return urlInvalid;
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    return allowPrivateNetwork ? urlScheme : urlHttps;
  }
  if (value.includes("#")) return urlFragment;
  // `new URL` drops an empty `user@`; the server treats it as credentials.
  const authority = /^[a-z][a-z0-9+.-]*:\/\/([^/?#]*)/i.exec(value)?.[1] ?? "";
  if (authority.includes("@")) return urlUserinfo;
  if (url.hostname === "") return urlInvalid;
  if (!allowPrivateNetwork) {
    if (url.protocol !== "https:") return urlHttps;
    if (isIpLiteral(url.hostname)) return urlIpLiteral;
  }
  return undefined;
}

function isIpLiteral(hostname: string): boolean {
  return hostname.startsWith("[") || /^\d+(\.\d+){3}$/.test(hostname);
}

const claimRequired = msg({
  id: "admin.federation.claims.required",
  message: "Enter a claim name.",
});

export function claimNameProblem(value: string): MessageDescriptor | undefined {
  if (value === "") return claimRequired;
  if (value !== value.trim()) return spaces;
  return undefined;
}

/**
 * Whether a save would switch the claim linked accounts are matched on. The
 * server matches a returning person by issuer and this claim's value, so a
 * change leaves every existing link pointing at a value that no longer arrives.
 */
export function subjectChanged(saved: string, next: string): boolean {
  return saved !== next;
}

/** The endpoints field: which mode, what is typed, and what is overridden. */
export interface EndpointsValue {
  mode: "discovery" | "manual";
  values: Record<EndpointName, string>;
  /** Discovery mode's overrides, in the order they were added. */
  overridden: EndpointName[];
}

export function endpointsValue(config: OidcProviderConfig): EndpointsValue {
  const values: Record<EndpointName, string> = {
    authorization: config.endpoints.authorization ?? "",
    token: config.endpoints.token ?? "",
    userinfo: config.endpoints.userinfo ?? "",
    jwks: config.endpoints.jwks ?? "",
  };
  return {
    mode: config.configurationMode,
    values,
    overridden:
      config.configurationMode === "discovery"
        ? endpointNames
            .map((entry) => entry.name)
            .filter((name) => config.endpoints[name] !== null)
        : [],
  };
}

/** The endpoints a mode shows, in the fixed order. */
export function shownEndpoints(value: EndpointsValue): EndpointName[] {
  return endpointNames
    .map((entry) => entry.name)
    .filter(
      (name) => value.mode === "manual" || value.overridden.includes(name),
    );
}

/**
 * The wire form of the field. Only what the current mode shows is sent: an
 * endpoint that is not overridden under discovery is `null` ("use what
 * discovery found"), and an empty UserInfo endpoint under manual is `null`
 * ("do not request UserInfo"). Text typed in the other mode is kept in the form
 * but never leaves it.
 */
export function endpointsBody(
  value: EndpointsValue,
): Pick<OidcProviderConfig, "configurationMode" | "endpoints"> {
  const shown = new Set(shownEndpoints(value));
  const entry = (name: EndpointName) =>
    shown.has(name) && value.values[name] !== "" ? value.values[name] : null;
  return {
    configurationMode: value.mode,
    endpoints: {
      authorization: entry("authorization"),
      token: entry("token"),
      userinfo: entry("userinfo"),
      jwks: entry("jwks"),
    },
  };
}

/** One endpoint's problem, drawn under that endpoint's input. */
export interface EndpointProblem {
  endpoint: EndpointName;
  message: MessageDescriptor;
}

export function isEndpointProblem(value: unknown): value is EndpointProblem {
  return (
    typeof value === "object" &&
    value !== null &&
    "endpoint" in value &&
    "message" in value
  );
}

const endpointRequired: Record<
  Exclude<EndpointName, "userinfo">,
  MessageDescriptor
> = {
  authorization: msg({
    id: "admin.federation.connection.authorization.required",
    message: "Enter the authorization endpoint.",
  }),
  token: msg({
    id: "admin.federation.connection.token.required",
    message: "Enter the token endpoint.",
  }),
  jwks: msg({
    id: "admin.federation.connection.jwks.required",
    message: "Enter the JWKS endpoint.",
  }),
};

const overrideRequired = msg({
  id: "admin.federation.connection.override.required",
  message: "Enter the address, or remove this override.",
});

/** Every shown endpoint's problem, in order; empty when all are usable. */
export function endpointProblems(
  value: EndpointsValue,
  allowPrivateNetwork: boolean,
): EndpointProblem[] {
  const problems: EndpointProblem[] = [];
  for (const name of shownEndpoints(value)) {
    const text = value.values[name];
    if (text === "") {
      // A manual configuration may leave UserInfo out; an override row that
      // is empty overrides nothing and is a row to fill or remove.
      if (value.mode === "discovery") {
        problems.push({ endpoint: name, message: overrideRequired });
      } else if (name !== "userinfo") {
        problems.push({ endpoint: name, message: endpointRequired[name] });
      }
      continue;
    }
    const problem = endpointUrlProblem(text, allowPrivateNetwork);
    if (problem !== undefined)
      problems.push({ endpoint: name, message: problem });
  }
  return problems;
}

const issuerRequired = msg({
  id: "admin.federation.connection.issuer.required",
  message: "Enter the issuer URL.",
});

export function issuerProblem(
  value: string,
  allowPrivateNetwork: boolean,
): MessageDescriptor | undefined {
  if (value === "") return issuerRequired;
  return endpointUrlProblem(value, allowPrivateNetwork);
}

const clientIdRequired = msg({
  id: "admin.federation.connection.client-id.required",
  message: "Enter the client ID.",
});

export function clientIdProblem(value: string): MessageDescriptor | undefined {
  if (value === "") return clientIdRequired;
  if (value !== value.trim()) return spaces;
  return undefined;
}

const authNeedsExplicit = msg({
  id: "admin.federation.auth.manual-needs-method",
  message: "With endpoints entered by hand, choose a specific method.",
});

const authPublicNeedsS256 = msg({
  id: "admin.federation.auth.public-needs-s256",
  message:
    "A public client needs S256 PKCE, and this provider is set to use a different PKCE method.",
});

/**
 * Why a token auth method cannot be saved with the rest of the connection.
 * The PKCE method is not edited on this page, so it is read from the saved
 * configuration.
 */
export function clientAuthMethodProblem(
  method: ClientAuthMethod,
  mode: EndpointsValue["mode"],
  pkceMethod: OidcProviderConfig["pkceMethod"],
): MessageDescriptor | undefined {
  if (method === "discovery" && mode === "manual") return authNeedsExplicit;
  if (method === "none" && pkceMethod !== "S256") return authPublicNeedsS256;
  return undefined;
}

/** The connection form, as the detail page holds it. */
export interface ConnectionValues {
  issuerUrl: string;
  clientId: string;
  tokenAuthMethod: ClientAuthMethod;
  scopes: TagListValue;
  endpoints: EndpointsValue;
}

/**
 * Everything wrong with a connection form, by field. The detail page draws
 * each under its own field; this is the whole rule in one place, for the page
 * and its tests.
 */
export function connectionProblems(
  values: ConnectionValues,
  saved: Pick<OidcProviderConfig, "allowPrivateNetwork" | "pkceMethod">,
): {
  issuerUrl?: MessageDescriptor;
  clientId?: MessageDescriptor;
  tokenAuthMethod?: MessageDescriptor;
  scopes?: MessageDescriptor;
  endpoints: EndpointProblem[];
} {
  const problems = {
    issuerUrl: issuerProblem(values.issuerUrl, saved.allowPrivateNetwork),
    clientId: clientIdProblem(values.clientId),
    tokenAuthMethod: clientAuthMethodProblem(
      values.tokenAuthMethod,
      values.endpoints.mode,
      saved.pkceMethod,
    ),
    scopes: scopesProblem(values.scopes),
    endpoints: endpointProblems(values.endpoints, saved.allowPrivateNetwork),
  };
  return Object.fromEntries(
    Object.entries(problems).filter(([, problem]) => problem !== undefined),
  ) as ReturnType<typeof connectionProblems>;
}
