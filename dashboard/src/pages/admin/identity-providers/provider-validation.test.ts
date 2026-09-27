import { describe, expect, it } from "vitest";
import { defaultOidcProviderConfig } from "@/api/federation";
import {
  type ConnectionValues,
  claimNameProblem,
  connectionProblems,
  domainProblem,
  type EndpointsValue,
  endpointProblems,
  endpointsBody,
  endpointsValue,
  endpointUrlProblem,
  openidRequired,
  scopeProblem,
  scopesProblem,
  subjectChanged,
  tagDraftProblem,
} from "@/pages/admin/identity-providers/provider-validation";

const blankEndpoints = { authorization: "", token: "", userinfo: "", jwks: "" };

function connection(
  overrides: Partial<ConnectionValues> = {},
): ConnectionValues {
  return {
    issuerUrl: "https://idp.example.com",
    clientId: "client",
    tokenAuthMethod: "discovery",
    scopes: { tags: ["openid", "profile"], draft: "" },
    endpoints: { mode: "discovery", values: blankEndpoints, overridden: [] },
    ...overrides,
  };
}

const saved = { allowPrivateNetwork: false, pkceMethod: "S256" as const };

describe("scopes", () => {
  it("accepts a scope token and refuses what RFC 6749 does not allow", () => {
    expect(scopeProblem("offline_access", ["openid"])).toBeUndefined();
    expect(scopeProblem("https://graph.example/.default", [])).toBeUndefined();
    expect(scopeProblem(" email", [])).toBeDefined();
    expect(scopeProblem("email ", [])).toBeDefined();
    expect(scopeProblem("read write", [])).toBeDefined();
    expect(scopeProblem("read\twrite", [])).toBeDefined();
    expect(scopeProblem('say"hi', [])).toBeDefined();
    expect(scopeProblem("back\\slash", [])).toBeDefined();
    expect(scopeProblem("openid", ["openid"])).toBeDefined();
  });

  it("keeps case: a scope differing only in case is a different scope", () => {
    expect(scopeProblem("OpenID", ["openid"])).toBeUndefined();
  });

  it("requires openid and refuses text left in the input", () => {
    expect(scopesProblem({ tags: ["openid"], draft: "" })).toBeUndefined();
    expect(scopesProblem({ tags: ["profile"], draft: "" })).toBe(
      openidRequired,
    );
    expect(scopesProblem({ tags: ["openid"], draft: "email" })).toBeDefined();
    expect(tagDraftProblem({ tags: [], draft: "" })).toBeUndefined();
    expect(tagDraftProblem({ tags: [], draft: " " })).toBeDefined();
  });
});

describe("domains", () => {
  it("accepts a bare domain and refuses anything else", () => {
    expect(domainProblem("example.com", [])).toBeUndefined();
    expect(domainProblem("mail.example.co.uk", [])).toBeUndefined();
    expect(domainProblem(" example.com", [])).toBeDefined();
    expect(domainProblem("https://example.com", [])).toBeDefined();
    expect(domainProblem("example.com/path", [])).toBeDefined();
    expect(domainProblem("localhost", [])).toBeDefined();
    expect(domainProblem("example.com", ["example.com"])).toBeDefined();
  });
});

describe("endpoint addresses", () => {
  it("requires https and a host name while private networks are off", () => {
    expect(
      endpointUrlProblem("https://idp.example.com/authorize", false),
    ).toBeUndefined();
    expect(endpointUrlProblem("http://idp.example.com", false)).toBeDefined();
    expect(endpointUrlProblem("https://192.0.2.1/token", false)).toBeDefined();
    expect(
      endpointUrlProblem("https://[2001:db8::1]/token", false),
    ).toBeDefined();
  });

  it("allows http and IP addresses when private networks are allowed", () => {
    expect(
      endpointUrlProblem("http://idp.internal:8080", true),
    ).toBeUndefined();
    expect(endpointUrlProblem("http://10.0.0.5/token", true)).toBeUndefined();
    expect(endpointUrlProblem("ftp://idp.internal", true)).toBeDefined();
  });

  it("refuses a fragment, credentials, a missing host and stray spaces", () => {
    expect(
      endpointUrlProblem("https://idp.example.com/#x", true),
    ).toBeDefined();
    expect(
      endpointUrlProblem("https://user:pass@idp.example.com", true),
    ).toBeDefined();
    expect(endpointUrlProblem("https://@idp.example.com", true)).toBeDefined();
    expect(endpointUrlProblem("idp.example.com", true)).toBeDefined();
    expect(endpointUrlProblem("https://", true)).toBeDefined();
    expect(endpointUrlProblem(" https://idp.example.com", true)).toBeDefined();
    expect(endpointUrlProblem("https://idp.example.com ", true)).toBeDefined();
  });
});

describe("claims", () => {
  it("requires a name without stray spaces", () => {
    expect(claimNameProblem("preferred_username")).toBeUndefined();
    expect(claimNameProblem("")).toBeDefined();
    expect(claimNameProblem(" sub")).toBeDefined();
  });

  it("notices a changed subject claim", () => {
    expect(subjectChanged("sub", "sub")).toBe(false);
    expect(subjectChanged("sub", "oid")).toBe(true);
  });
});

describe("endpoints", () => {
  it("reads the saved overrides in the fixed order", () => {
    const config = {
      ...defaultOidcProviderConfig(),
      endpoints: {
        authorization: null,
        token: null,
        userinfo: null,
        jwks: "https://keys.example.com",
      },
    };
    expect(endpointsValue(config)).toEqual({
      mode: "discovery",
      values: { ...blankEndpoints, jwks: "https://keys.example.com" },
      overridden: ["jwks"],
    });
  });

  it("sends only what the mode shows", () => {
    const typed = {
      authorization: "https://idp.example.com/a",
      token: "https://idp.example.com/t",
      userinfo: "",
      jwks: "https://idp.example.com/k",
    };
    const discovery: EndpointsValue = {
      mode: "discovery",
      values: typed,
      overridden: ["token"],
    };
    expect(endpointsBody(discovery)).toEqual({
      configurationMode: "discovery",
      endpoints: {
        authorization: null,
        token: "https://idp.example.com/t",
        userinfo: null,
        jwks: null,
      },
    });
    expect(endpointsBody({ ...discovery, mode: "manual" })).toEqual({
      configurationMode: "manual",
      endpoints: { ...typed, userinfo: null },
    });
  });

  it("requires three endpoints by hand and a value in every override", () => {
    const manual: EndpointsValue = {
      mode: "manual",
      values: blankEndpoints,
      overridden: [],
    };
    expect(
      endpointProblems(manual, false).map((problem) => problem.endpoint),
    ).toEqual(["authorization", "token", "jwks"]);
    expect(
      endpointProblems(
        { mode: "discovery", values: blankEndpoints, overridden: ["userinfo"] },
        false,
      ).map((problem) => problem.endpoint),
    ).toEqual(["userinfo"]);
    expect(
      endpointProblems(
        {
          mode: "discovery",
          values: { ...blankEndpoints, token: "http://idp.example.com" },
          overridden: ["token"],
        },
        false,
      ).map((problem) => problem.endpoint),
    ).toEqual(["token"]);
  });
});

describe("the connection form", () => {
  it("passes a valid discovery configuration", () => {
    expect(connectionProblems(connection(), saved)).toEqual({ endpoints: [] });
  });

  it("refuses discovery's auth method with manual endpoints", () => {
    const problems = connectionProblems(
      connection({
        endpoints: {
          mode: "manual",
          values: {
            authorization: "https://idp.example.com/a",
            token: "https://idp.example.com/t",
            userinfo: "",
            jwks: "https://idp.example.com/k",
          },
          overridden: [],
        },
      }),
      saved,
    );
    expect(problems.tokenAuthMethod).toBeDefined();
    expect(problems.endpoints).toEqual([]);
  });

  it("refuses a public client whose saved PKCE method is not S256", () => {
    const problems = connectionProblems(
      connection({ tokenAuthMethod: "none" }),
      {
        ...saved,
        pkceMethod: "plain",
      },
    );
    expect(problems.tokenAuthMethod).toBeDefined();
    expect(
      connectionProblems(connection({ tokenAuthMethod: "none" }), saved)
        .tokenAuthMethod,
    ).toBeUndefined();
  });

  it("names the missing openid scope, issuer, client ID and endpoints", () => {
    const problems = connectionProblems(
      connection({
        issuerUrl: "",
        clientId: "",
        scopes: { tags: ["profile"], draft: "" },
        endpoints: { mode: "manual", values: blankEndpoints, overridden: [] },
        tokenAuthMethod: "client_secret_basic",
      }),
      saved,
    );
    expect(problems.issuerUrl).toBeDefined();
    expect(problems.clientId).toBeDefined();
    expect(problems.scopes).toBe(openidRequired);
    expect(problems.endpoints).toHaveLength(3);
  });
});
