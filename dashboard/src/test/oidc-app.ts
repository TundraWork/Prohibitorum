import type { components } from "@/api/generated/schema";

export type OidcApp = components["schemas"]["OIDCApplicationView"];

/** A confidential, enabled, open OIDC application; `overrides` change what a test cares about. */
export function oidcAppFixture(overrides: Partial<OidcApp> = {}): OidcApp {
  return {
    clientId: "grafana",
    displayName: "Grafana",
    redirectUris: ["https://grafana.example.test/login/generic_oauth"],
    postLogoutRedirectUris: [],
    allowedScopes: ["openid", "profile", "email"],
    clientAuthMethod: "client_secret_basic",
    disabled: false,
    accessRestricted: false,
    subjectSource: "sub",
    claimAliases: {},
    requireConsent: false,
    requirePkce: true,
    createdAt: "2026-09-01T00:00:00Z",
    launchUrl: "https://grafana.example.test",
    ...overrides,
  };
}
