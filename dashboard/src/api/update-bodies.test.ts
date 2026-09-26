import { describe, expect, it } from "vitest";
import type { components } from "@/api/generated/schema";
import type { OidcProviderConfig, ProviderMode } from "@/api/raw-admin-paths";
import {
  forwardAuthAppUpdateBody,
  identityProviderUpdateBody,
  oidcAppUpdateBody,
  samlAppUpdateBody,
} from "@/api/update-bodies";

type OIDCApplicationView = components["schemas"]["OIDCApplicationView"];
type ForwardAuthAppView = components["schemas"]["ForwardAuthAppView"];
type SAMLApplicationView = components["schemas"]["SAMLApplicationView"];
type IdentityProviderView = components["schemas"]["IdentityProviderView"];

/**
 * Every write on these pages is a whole-record PUT, so a field left out of the
 * body is not "unchanged" — it is reset. These tests are the guard on that: they
 * assert that a form editing one section carries the other sections' fields, and
 * that a field the server reads as "clear it when absent" (an OIDC application's
 * `launchUrl`, a SAML application's `attributeMap`, a forward-auth application's
 * `scopes`) is always present.
 *
 * Each case is written as the page's own call — the patch a section would pass —
 * rather than as a direct assertion about object shape, so a reader can see which
 * page the rule is protecting.
 */

const oidcApp: OIDCApplicationView = {
  clientId: "app-1",
  displayName: "App One",
  redirectUris: ["https://app.example.test/callback"],
  postLogoutRedirectUris: ["https://app.example.test/goodbye"],
  allowedScopes: ["openid", "profile"],
  clientAuthMethod: "client_secret_basic",
  disabled: false,
  accessRestricted: false,
  subjectSource: "sub",
  claimAliases: {},
  requireConsent: true,
  requirePkce: true,
  launchUrl: "https://app.example.test",
  createdAt: "2026-01-01T00:00:00Z",
};

describe("OIDC application update body", () => {
  it("carries disabled and launchUrl when a form edits something else", () => {
    // What the general section does: it changes a redirect URI and must not
    // re-enable a disabled application or clear its launch address.
    const disabled = { ...oidcApp, disabled: true };
    expect(
      oidcAppUpdateBody(disabled, {
        redirectUris: ["https://app.example.test/other"],
      }),
    ).toMatchObject({ disabled: true, launchUrl: "https://app.example.test" });
  });

  it("carries launchUrl when the danger section flips the flag", () => {
    // The other direction: disabling from the danger section must not wipe the
    // launch address the general section holds.
    expect(oidcAppUpdateBody(oidcApp, { disabled: true })).toMatchObject({
      disabled: true,
      launchUrl: "https://app.example.test",
    });
  });

  it("distinguishes clearing the launch URL from leaving it alone", () => {
    expect(
      oidcAppUpdateBody(oidcApp, { launchUrl: null }).launchUrl,
    ).toBeNull();
    expect(oidcAppUpdateBody(oidcApp, {}).launchUrl).toBe(
      "https://app.example.test",
    );
  });

  it("reads an absent launch URL as nothing to carry", () => {
    const { launchUrl: _dropped, ...withoutLaunch } = oidcApp;
    expect(oidcAppUpdateBody(withoutLaunch).launchUrl).toBeNull();
  });

  it("sends empty arrays rather than omitting a list", () => {
    const body = oidcAppUpdateBody(oidcApp, { postLogoutRedirectUris: [] });
    expect(body.postLogoutRedirectUris).toEqual([]);
    expect(body.redirectUris).toEqual(["https://app.example.test/callback"]);
  });
});

describe("forward-auth application update body", () => {
  const app: ForwardAuthAppView = {
    clientId: "fa-1",
    displayName: "Service",
    forwardAuthHost: "service.example.test",
    scopes: [{ name: "read", description: "Read things" }],
    accessRestricted: false,
    disabled: false,
    remoteUserSource: "username",
    createdAt: "2026-01-01T00:00:00Z",
  };

  it("carries the whole scope vocabulary when a form edits the name", () => {
    expect(
      forwardAuthAppUpdateBody(app, { displayName: "Renamed" }).scopes,
    ).toEqual([{ name: "read", description: "Read things" }]);
  });

  it("carries the host when a form edits the vocabulary", () => {
    // The vocabulary field is the one an edit clears wholesale, so the host has
    // to travel with it or the application would lose its hostname too.
    const body = forwardAuthAppUpdateBody(app, { scopes: [] });
    expect(body.host).toBe("service.example.test");
    expect(body.scopes).toEqual([]);
  });

  it("drops an empty scope description rather than sending one", () => {
    const body = forwardAuthAppUpdateBody({
      ...app,
      scopes: [{ name: "read" }],
    });
    expect(body.scopes).toEqual([{ name: "read" }]);
  });
});

describe("SAML application update body", () => {
  const app: SAMLApplicationView = {
    id: 1,
    entityId: "https://saml.example.test",
    displayName: "SAML App",
    nameIdFormat: "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent",
    attributeMap: [
      {
        name: "mail",
        name_format: "urn:oasis:names:tc:SAML:2.0:attrname-format:basic",
        source: "attributes.email",
        multi: false,
      },
    ],
    requireSignedAuthnRequest: true,
    allowIdpInitiated: false,
    disabled: false,
    accessRestricted: false,
    sessionLifetimeSecs: 3600,
    acs: [],
    keys: [],
    createdAt: "2026-01-01T00:00:00Z",
  };

  it("carries the attribute map when a form edits the name", () => {
    // The server clears the map when it is absent, so an edit to any other field
    // must not delete every mapping.
    expect(
      samlAppUpdateBody(app, { displayName: "Renamed" }).attributeMap,
    ).toEqual(app.attributeMap);
  });

  it("converts the form's minutes into the wire's seconds", () => {
    expect(
      samlAppUpdateBody(app, { sessionLifetimeMinutes: 90 }),
    ).toMatchObject({
      sessionLifetimeSecs: 5400,
    });
  });

  it("omits the lifetime when the form is left empty", () => {
    expect(
      "sessionLifetimeSecs" in
        samlAppUpdateBody(app, { sessionLifetimeMinutes: null }),
    ).toBe(false);
  });

  it("reads back the stored lifetime in minutes", () => {
    // 3600 seconds must come back as 60 in the field, not 3600.
    expect(samlAppUpdateBody(app).sessionLifetimeSecs).toBe(3600);
  });

  it("omits the lifetime when there is none stored", () => {
    const { sessionLifetimeSecs: _dropped, ...withoutLifetime } = app;
    expect("sessionLifetimeSecs" in samlAppUpdateBody(withoutLifetime)).toBe(
      false,
    );
  });
});

describe("identity provider update body", () => {
  const config: OidcProviderConfig = {
    issuerUrl: "https://idp.example.test",
    clientId: "client-1",
    scopes: ["openid"],
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

  const provider: IdentityProviderView = {
    slug: "idp",
    displayName: "IdP",
    protocol: "oidc",
    mode: "invite_only",
    config,
    disabled: false,
    secretConfigured: true,
    secretStatus: "valid",
    secretValidatedAt: null,
    ready: true,
    supportsOperator: false,
    searchFields: [],
    createdAt: "2026-01-01T00:00:00Z",
  };

  it("carries the config when a form edits the name", () => {
    expect(
      identityProviderUpdateBody(provider, { displayName: "Renamed" }).config,
    ).toEqual(config);
  });

  it("carries the name when a form edits the config", () => {
    const next: OidcProviderConfig = {
      ...config,
      issuerUrl: "https://other.test",
    };
    const body = identityProviderUpdateBody(provider, { config: next });
    expect(body.displayName).toBe("IdP");
    expect((body.config as OidcProviderConfig).issuerUrl).toBe(
      "https://other.test",
    );
  });

  it("sends no secret", () => {
    // A secret is set through its own endpoint; the record body must never carry
    // one, or an ordinary save would look like a rotation.
    expect(
      "secret" in identityProviderUpdateBody(provider, { displayName: "x" }),
    ).toBe(false);
  });

  it("falls back to a mode the server accepts", () => {
    const body = identityProviderUpdateBody(
      { ...provider, mode: "something-else" },
      { displayName: "x" },
    );
    expect(body.mode).toBe("invite_only");
  });

  it("falls back to a usable config rather than sending an empty object", () => {
    // The server rejects `{}` for an OIDC provider, so an unreadable config must
    // not become one; the defaults are a config it accepts.
    const body = identityProviderUpdateBody(
      { ...provider, config: {} },
      { displayName: "x" },
    );
    expect(body.config).not.toEqual({});
    expect((body.config as OidcProviderConfig).scopes.length).toBeGreaterThan(
      0,
    );
  });

  it("keeps the empty config a Steam provider has", () => {
    const body = identityProviderUpdateBody(
      { ...provider, protocol: "steam", config: {} },
      { displayName: "x" },
    );
    expect(body.config).toEqual({});
  });

  it("accepts the mode the caller passes", () => {
    const mode: ProviderMode = "auto_provision";
    expect(identityProviderUpdateBody(provider, { mode }).mode).toBe(
      "auto_provision",
    );
  });
});
