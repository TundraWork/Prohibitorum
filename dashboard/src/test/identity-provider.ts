import type { components } from "@/api/generated/schema";

export type Provider = components["schemas"]["IdentityProviderView"];

/** An enabled, ready OIDC provider with a stored secret and sign-in on. */
export function providerFixture(overrides: Partial<Provider> = {}): Provider {
  return {
    slug: "corp",
    displayName: "Corporate SSO",
    protocol: "oidc",
    allowSignIn: true,
    pendingInvitationCount: 0,
    mode: "invite_only",
    disabled: false,
    ready: true,
    secretConfigured: true,
    secretStatus: "valid",
    secretValidatedAt: null,
    createdAt: "2026-01-01T00:00:00Z",
    config: {},
    supportsOperator: false,
    searchFields: [],
    linkedAccountCount: 0,
    ...overrides,
  };
}

/** A stored OIDC configuration of a public client: no secret, PKCE only. */
export const publicOidcConfig = {
  issuerUrl: "https://idp.example.com",
  clientId: "client",
  scopes: ["openid", "profile"],
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
  tokenAuthMethod: "none",
  pkceMethod: "S256",
};
