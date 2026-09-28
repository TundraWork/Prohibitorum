import { describe, expect, it } from "vitest";
import { ApiError, describeError, describeRouteFailure } from "@/api/errors";

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
