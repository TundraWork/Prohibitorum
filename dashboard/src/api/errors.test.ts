import { describe, expect, it } from "vitest";
import {
  ApiError,
  describeError,
  describeErrorLanding,
  describeRouteFailure,
} from "@/api/errors";

function refused(code: string) {
  return new ApiError({ kind: "http", status: 404, code, requestId: "r1" });
}

describe("describeError", () => {
  it("reads a scoped wording before the general one", () => {
    // The signing-key handlers reuse the passkey's "not found" code.
    expect(describeError(refused("credential_not_found")).id).toBe(
      "error.credential_not_found",
    );
    expect(
      describeError(refused("credential_not_found"), "signing-key").id,
    ).toBe("error.signing-key.credential_not_found");
  });

  it("falls back to the general wording for a code the scope does not cover", () => {
    expect(
      describeError(refused("active_key_no_replacement"), "signing-key").id,
    ).toBe("error.active_key_no_replacement");
    expect(describeError(refused("mystery"), "signing-key")).toMatchObject({
      id: "error.request_failed",
      requestId: "r1",
    });
  });
});

describe("the federation scope", () => {
  function federated(code: string, federationName?: unknown) {
    return new ApiError({
      kind: "http",
      status: 400,
      code,
      requestId: "r1",
      details: federationName === undefined ? {} : { federationName },
    });
  }

  it("names the provider the failure carries", () => {
    expect(
      describeError(federated("upstream_error", "GitLab"), "federation"),
    ).toMatchObject({
      id: "error.federation.upstream_error",
      values: { provider: "GitLab" },
      requestId: "r1",
    });
  });

  it.each([
    ["no name", undefined],
    ["an empty name", ""],
    ["a name that is not text", 7],
  ])("falls back rather than say 'the provider' with %s", (_, name) => {
    expect(
      describeError(federated("invite_required", name), "federation").id,
    ).toBe("error.sign_in_failed");
  });

  it("reads the general table for a code it does not hold", () => {
    expect(
      describeError(federated("rate_limited", "GitLab"), "federation").id,
    ).toBe("error.rate_limited");
  });

  it("leaves the console's own wording alone outside the scope", () => {
    expect(describeError(federated("provider_not_ready", "GitLab")).id).toBe(
      "error.provider_not_ready",
    );
  });
});

describe("the enrollment and public federation codes", () => {
  it.each([
    "enrollment_consumed",
    "enrollment_expired",
    "enrollment_method_not_allowed",
    "invalid_display_name",
    "credential_already_registered",
    "registration_failed",
    "federation_action_invalid",
    "username_collision",
    "vrchat_identity_invalid",
    "vrchat_proof_missing",
    "local_username_required",
  ])("describes %s in its own words", (code) => {
    expect(describeError(refused(code)).id).toBe(`error.${code}`);
  });

  it("tells the page that adds a sign-in that its step has timed out", () => {
    expect(
      describeError(refused("sudo_required"), "setup-signin"),
    ).toMatchObject({
      id: "error.setup-signin.sudo_required",
      message: "This step has timed out. Add it later from Security.",
    });
    expect(describeError(refused("sudo_required")).id).toBe(
      "error.sudo_required",
    );
  });

  it("reads an expired federation state as a sign-in, and as a test in the diagnostic scope", () => {
    expect(describeError(refused("federation_state_invalid"))).toMatchObject({
      message: "This sign-in has expired. Sign in again.",
    });
    expect(
      describeError(refused("federation_state_invalid"), "diagnostic"),
    ).toMatchObject({
      message: "This test is no longer valid. Start a new one.",
    });
  });

  it("says how long a rate-limited provider asks to wait when the response does", () => {
    const limited = (headers: [string, string][]) =>
      new ApiError({
        kind: "http",
        status: 429,
        code: "upstream_rate_limited",
        requestId: "r1",
        details: { federationName: "VRChat" },
        exchange: {
          method: "POST",
          path: "/api/prohibitorum/auth/federation/flows/f/verify",
          response: { status: 429, headers },
        },
      });
    expect(
      describeError(limited([["retry-after", "12"]]), "federation"),
    ).toMatchObject({
      id: "error.federation.upstream_rate_limited.seconds",
      values: { provider: "VRChat", seconds: 12 },
    });
    for (const headers of [[], [["retry-after", "soon"]]] as [
      string,
      string,
    ][][]) {
      expect(describeError(limited(headers), "federation")).toMatchObject({
        id: "error.federation.upstream_rate_limited",
        values: { provider: "VRChat" },
      });
    }
  });
});

describe("describeErrorLanding", () => {
  const instance = "Test instance";

  it("puts a denied application before any code", () => {
    expect(
      describeErrorLanding({
        code: "upstream_error",
        reason: "app_access_denied",
        instance,
      }).id,
    ).toBe("error.landing.app_access_denied");
  });

  it("reads the codes only a redirect carries, with the instance's name", () => {
    expect(
      describeErrorLanding({ code: "saml_sp_unknown", instance }),
    ).toMatchObject({
      id: "error.landing.saml_sp_unknown",
      values: { instance },
    });
  });

  it("names the provider from the link, and falls back without one", () => {
    expect(
      describeErrorLanding({
        code: "upstream_error",
        federationName: "GitLab",
        instance,
      }),
    ).toMatchObject({ values: { provider: "GitLab" } });
    expect(describeErrorLanding({ code: "upstream_error", instance }).id).toBe(
      "error.sign_in_failed",
    );
  });

  it("then reads the general table", () => {
    expect(
      describeErrorLanding({ code: "invalid_consent_ticket", instance }).id,
    ).toBe("error.invalid_consent_ticket");
  });

  it.each([undefined, "", "made_up", "__proto__", "toString"])(
    "falls back for %s",
    (code) => {
      expect(describeErrorLanding({ code, instance }).id).toBe(
        "error.sign_in_failed",
      );
    },
  );
});

function http(status: number, code?: string) {
  return new ApiError({ kind: "http", status, code, requestId: "req-1" });
}

describe("describeRouteFailure", () => {
  it.each([
    ["a lost connection", new ApiError({ kind: "network" }), "retry"],
    [
      "an unreadable reply",
      new ApiError({ kind: "invalid-response" }),
      "retry",
    ],
    ["a server fault", http(500, "server_error"), "retry"],
    ["maintenance", http(503, "maintenance_mode"), "retry"],
    ["a rate limit", http(429, "rate_limited"), "retry"],
    ["an anonymous session", http(401, "no_session"), "sign-in"],
    ["a step-up request", http(401, "sudo_required"), "none"],
    ["a disabled account", http(403, "account_disabled"), "sign-out"],
    ["a member on an admin page", http(403, "not_admin"), "none"],
    ["a missing record", http(404, "account_not_found"), "none"],
    ["a bad request", http(400, "bad_request"), "none"],
    ["a local refusal", new ApiError({ kind: "local" }), "none"],
  ] as const)("offers %s the %s recovery", (_, error, recovery) => {
    const failure = describeRouteFailure(error);
    expect(failure.recovery).toBe(recovery);
    const { requestId, ...message } = describeError(error);
    expect(failure.message).toEqual(message);
    expect(failure.facts.requestId).toBe(requestId);
  });

  it("lists the request, its status, code and request ID", () => {
    const exchange = {
      method: "GET",
      path: "/api/prohibitorum/admin/accounts/42",
      response: { status: 503, headers: [] },
    };
    const failure = describeRouteFailure(
      new ApiError({
        kind: "http",
        status: 503,
        code: "server_error",
        requestId: "req-abc123",
        exchange,
      }),
    );
    expect(failure.facts).toEqual({
      request: "GET /api/prohibitorum/admin/accounts/42",
      status: 503,
      code: "server_error",
      requestId: "req-abc123",
    });
    expect(failure.exchange).toBe(exchange);
  });

  it("leaves out a request ID that is not one", () => {
    const failure = describeRouteFailure(
      new ApiError({ kind: "http", status: 500, requestId: "<script>" }),
    );
    expect(failure.facts).not.toHaveProperty("requestId", "<script>");
    expect(failure.facts.requestId).toBeUndefined();
  });

  it("reads a thrown error as the page breaking", () => {
    const failure = describeRouteFailure(new TypeError("x is undefined"));
    expect(failure).toEqual({
      message: expect.objectContaining({ id: "route.error.crashed" }),
      recovery: "reload",
      facts: { exception: "TypeError: x is undefined" },
    });
  });

  it("names a thrown value that is not an error", () => {
    expect(describeRouteFailure("boom").facts.exception).toBe("boom");
    expect(describeRouteFailure({ reason: "gone" }).facts.exception).toBe(
      '{"reason":"gone"}',
    );
    const cyclic: Record<string, unknown> = {};
    cyclic.self = cyclic;
    expect(describeRouteFailure(cyclic).facts.exception).toBe(
      "[object Object]",
    );
  });
});
