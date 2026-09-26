/**
 * The Traefik dynamic configuration one forward-auth application needs.
 *
 * Prohibitorum answers the ForwardAuth middleware but sits outside the request
 * path, so a protected host is only protected once Traefik has been told to ask
 * it — and only usable once the OIDC callback on that host reaches Prohibitorum
 * rather than the application. Both halves live here, because an operator who
 * pastes one without the other gets a host that redirects into a sign-in it can
 * never finish.
 *
 * The text is the same text `docs/forward-auth.md` carries: an operator who read
 * the manual and one who copied from the console have to end up with one
 * deployment, so the generator is a pure function of two strings and its test
 * holds it against the document.
 *
 * The two strings are the only per-application values: the origin this console
 * is served from (what Traefik calls) and the host being protected (the
 * router's rule, and the scope of the per-domain cookie). Nothing else varies
 * between forward-auth applications.
 */

/** The address a browser reaches the console at, e.g. `https://auth.example.com`. */
export interface SnippetInput {
  /** This console's origin, with or without a trailing slash. */
  baseUrl: string;
  /** The application's registered host, e.g. `app.acme.io`. */
  host: string;
}

/** The verify endpoint Traefik calls for every gated request. */
export const verifyPath = "/api/prohibitorum/forward-auth/verify";

/** The cookie the verify endpoint sets and renews, forwarded back through Traefik. */
export const authCookieName = "__Host-prohibitorum_forward_auth";

/**
 * The path the OIDC callback lands on. It must be routed to Prohibitorum rather
 * than the application, and must not be gated by the forward-auth middleware —
 * it is where the request that has no cookie yet proves who it is.
 */
export const callbackPrefix = "/.prohibitorum-forward-auth/";

/** The five identity headers the gateway sets, all unconditionally. */
export const identityHeaders = [
  "Remote-User",
  "Remote-Name",
  "Remote-Email",
  "Remote-Groups",
  "Remote-Scopes",
] as const;

/** A trailing slash on the origin would produce a doubled one in the address. */
function withoutTrailingSlash(value: string): string {
  return value.endsWith("/") ? value.slice(0, -1) : value;
}

/**
 * The dynamic configuration for one protected host.
 *
 * ## Why the PAT stripping is unconditional
 *
 * Traefik runs this console as a verifier, not a proxy, so an inbound
 * `X-Prohibitorum-PAT` is forwarded upstream untouched unless a headers
 * middleware removes it — the protected service, not Prohibitorum, would then
 * see the bearer credential. The manual says `authResponseHeaders` is not a
 * substitute for stripping it explicitly, and the chain order matters: the
 * guard runs first, then the header is removed.
 *
 * It is emitted on every application because adding it to a browser-only router
 * costs nothing, while omitting it on one that does carry PATs leaks the token.
 *
 * ## Why all five headers
 *
 * `authResponseHeaders` replaces the listed headers with the gateway's verified
 * values on an allowed request. A header left off the list is one a client can
 * supply itself, so the full five are listed whatever identifier the
 * application is configured to emit — the value that varies is inside
 * `Remote-User`, not which headers exist.
 */
export function forwardAuthSnippet({ baseUrl, host }: SnippetInput): string {
  const origin = withoutTrailingSlash(baseUrl);
  const service = `${host}-backend`;
  return `http:
  middlewares:
    prohibitorum-forwardauth:
      forwardAuth:
        address: "${origin}${verifyPath}"
        trustForwardHeader: true
        addAuthCookiesToResponse:
          - ${authCookieName}
        authResponseHeaders:
${identityHeaders.map((header) => `          - ${header}`).join("\n")}

    strip-prohibitorum-pat:
      headers:
        customRequestHeaders:
          X-Prohibitorum-PAT: ""   # empty string → Traefik removes the header

  routers:
    # The protected app — gated by the forward-auth middleware, and reaching the
    # backend only after the raw PAT header has been stripped.
    ${host}:
      rule: "Host(\`${host}\`)"
      entryPoints: ["websecure"]
      middlewares:
        - prohibitorum-forwardauth
        - strip-prohibitorum-pat
      service: ${service}
      tls: {}

    # The per-domain auth/callback path — routed to Prohibitorum, NOT the app,
    # and NOT gated by the forward-auth middleware. This is where the OIDC
    # callback lands so the per-domain cookie is scoped to ${host}.
    ${host}-forwardauth:
      rule: "Host(\`${host}\`) && PathPrefix(\`${callbackPrefix}\`)"
      entryPoints: ["websecure"]
      service: prohibitorum
      tls: {}
`;
}
