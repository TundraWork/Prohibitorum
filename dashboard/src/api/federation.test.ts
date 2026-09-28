import { describe, expect, it } from "vitest";
import { ApiError } from "@/api/errors";
import {
  defaultOidcProviderConfig,
  readAttributeMap,
  readClaimAliases,
  readFederationFlow,
  readOidcProviderConfig,
  readPrincipalSource,
  readProviderMode,
  readProviderProtocol,
} from "@/api/federation";

/**
 * The server hands the console a few loosely-typed fields — a provider's
 * `config` and `mode`, an application's `attributeMap` — because Huma documents
 * them as `unknown` or plain strings. `federation.ts` is where they are read.
 *
 * These tests are about what happens when the value is not what the console
 * expects. A cast at the call site would render a half-empty form and let the
 * reader save it; reading through here either produces the real values or
 * produces nothing, and the pages fall back to the server's own defaults.
 */

const fullConfig = {
  issuerUrl: "https://idp.example.test",
  clientId: "client-1",
  scopes: ["openid", "profile"],
  allowedDomains: ["example.test"],
  requireVerifiedEmail: true,
  allowPrivateNetwork: false,
  usernameClaim: "preferred_username",
  displayNameClaim: "name",
  emailClaim: "email",
  pictureClaim: "picture",
  subjectClaim: "sub",
  configurationMode: "discovery",
  endpoints: {
    authorization: "https://idp.example.test/authorize",
    token: "https://idp.example.test/token",
    userinfo: null,
    jwks: "https://idp.example.test/jwks",
  },
  tokenAuthMethod: "client_secret_basic",
  pkceMethod: "S256",
};

describe("reading an OIDC provider config", () => {
  it("reads a complete config", () => {
    expect(readOidcProviderConfig(fullConfig)).toEqual(fullConfig);
  });

  it("reads the empty config a Steam provider carries as no config", () => {
    expect(readOidcProviderConfig({})).toBeNull();
  });

  it.each([
    [
      "a missing key",
      () => {
        const { pkceMethod: _dropped, ...rest } = fullConfig;
        return rest;
      },
    ],
    [
      "a null endpoint",
      () => ({
        ...fullConfig,
        endpoints: { ...fullConfig.endpoints, token: 12 },
      }),
    ],
    ["a non-array scope list", () => ({ ...fullConfig, scopes: "openid" })],
    [
      "an unknown configuration mode",
      () => ({
        ...fullConfig,
        configurationMode: "whatever",
      }),
    ],
    [
      "an unknown auth method",
      () => ({
        ...fullConfig,
        tokenAuthMethod: "client_secret_jwt",
      }),
    ],
    ["an unknown pkce method", () => ({ ...fullConfig, pkceMethod: "s256" })],
  ])("returns null for %s", (_name, build) => {
    expect(readOidcProviderConfig(build())).toBeNull();
  });

  it("keeps a null endpoint, which means no override", () => {
    const read = readOidcProviderConfig(fullConfig);
    expect(read?.endpoints.userinfo).toBeNull();
  });

  it("starts a new provider from the server's own defaults", () => {
    const defaults = defaultOidcProviderConfig();
    // The defaults have to be a config the server accepts, or a provider created
    // from an unreadable record would be rejected on its first save.
    expect(readOidcProviderConfig(defaults)).toEqual(defaults);
  });
});

describe("reading the loose enum fields", () => {
  it("accepts the values the server sends", () => {
    expect(readProviderProtocol("oidc")).toBe("oidc");
    expect(readProviderMode("link_only")).toBe("link_only");
    expect(readPrincipalSource("verified_email")).toBe("verified_email");
  });

  it("reads an unknown value as absent rather than passing it on", () => {
    expect(readProviderProtocol("saml")).toBeUndefined();
    expect(readProviderMode("manual")).toBeUndefined();
    expect(readPrincipalSource("email")).toBeUndefined();
    expect(readProviderProtocol(undefined)).toBeUndefined();
  });
});

describe("reading a SAML attribute map", () => {
  const mapping = {
    name: "mail",
    name_format: "urn:oasis:names:tc:SAML:2.0:attrname-format:basic",
    friendly_name: "Email",
    source: "attributes.email",
    multi: false,
  };

  it("reads well-formed mappings", () => {
    expect(readAttributeMap([mapping])).toEqual([mapping]);
  });

  it("reads a missing map as empty", () => {
    expect(readAttributeMap(null)).toEqual([]);
    expect(readAttributeMap([])).toEqual([]);
  });

  it("gives up on the whole map when one entry is malformed", () => {
    // Partial success would be worse than none: the reader would save a form
    // showing three of their four mappings and quietly drop the fourth.
    expect(readAttributeMap([mapping, { name: "broken" }])).toEqual([]);
    expect(readAttributeMap([{ ...mapping, multi: "yes" }])).toEqual([]);
  });
});

describe("reading claim aliases", () => {
  it("reads a map of output name to source claim", () => {
    expect(readClaimAliases({ nickname: "preferred_username" })).toEqual({
      nickname: "preferred_username",
    });
  });

  it("reads a missing or malformed map as empty", () => {
    expect(readClaimAliases(undefined)).toEqual({});
    expect(readClaimAliases(["nickname"])).toEqual({});
    expect(readClaimAliases({ nickname: 3 })).toEqual({});
  });
});

describe("readFederationFlow", () => {
  const identify = {
    provider: { slug: "vrchat", displayName: "VRChat", protocol: "vrchat" },
    intent: "enroll",
    step: "identify",
    requiresLocalUsername: false,
    expiresAt: "2026-10-03T00:00:00Z",
  };
  const proof = {
    ...identify,
    step: "proof",
    profileUrl: "https://vrchat.com/home/user/usr_1",
    proofUrl: "https://id.example/verify/vrchat/p",
    requiresLocalUsername: true,
  };

  function refused(value: unknown) {
    try {
      readFederationFlow(value);
    } catch (error) {
      return error instanceof ApiError && error.kind === "invalid-response";
    }
    return false;
  }

  it("reads both steps as they come", () => {
    expect(readFederationFlow(identify)).toEqual(identify);
    expect(readFederationFlow(proof)).toEqual(proof);
  });

  it("refuses an intent or a step it does not know", () => {
    expect(refused({ ...identify, intent: "recover" })).toBe(true);
    expect(refused({ ...identify, step: "done" })).toBe(true);
  });

  it("refuses a proof step without the link to put in the bio", () => {
    const { proofUrl, ...withoutProof } = proof;
    expect(refused(withoutProof)).toBe(true);
    expect(refused({ ...proof, proofUrl: "" })).toBe(true);
  });

  it("refuses a malformed provider or field", () => {
    expect(refused(null)).toBe(true);
    expect(refused([])).toBe(true);
    expect(refused({ ...identify, provider: "vrchat" })).toBe(true);
    expect(
      refused({
        ...identify,
        provider: { slug: "vrchat", protocol: "vrchat" },
      }),
    ).toBe(true);
    expect(refused({ ...identify, requiresLocalUsername: "no" })).toBe(true);
    expect(refused({ ...identify, expiresAt: 1 })).toBe(true);
    expect(refused({ ...proof, profileUrl: 3 })).toBe(true);
  });
});
