import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { client } from "@/api/client";
import { ApiError } from "@/api/errors";
import { installApiMocks, refreshTarget } from "@/devtools/mock/install";
import { resetMockConfig, updateMockConfig } from "@/devtools/mock/model";

type Application = Parameters<typeof installApiMocks>[0];

/** The console surface the middleware drives when the config changes. */
function application(): Application {
  return {
    queryClient: {
      invalidateQueries: vi.fn(),
      fetchQuery: vi.fn(async () => null),
    },
    router: {
      invalidate: vi.fn(),
      navigate: vi.fn(),
      // `subscribe` returns its unsubscribe, as the router's own does.
      subscribe: vi.fn(() => () => {}),
      state: { location: { pathname: "/login" } },
    },
  } as unknown as Application;
}

beforeEach(() => {
  window.localStorage.clear();
  resetMockConfig();
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  resetMockConfig();
});

describe("mock middleware latency", () => {
  it("holds an answer back for the configured delay", async () => {
    updateMockConfig((draft) => {
      draft.enabled = true;
      draft.delayMs = 700;
    });
    const app = application();
    installApiMocks(app);

    // The URL control is wired at install time: the address bar is read once
    // here and again on every navigation, so a walkthrough can set a screen up
    // without the panel. Asserted on the latency test because `installApiMocks`
    // runs once per module.
    expect(app.router.subscribe).toHaveBeenCalledWith(
      "onResolved",
      expect.any(Function),
    );

    const pending = client.GET("/api/prohibitorum/me");
    let settled = false;
    void pending.then(() => {
      settled = true;
    });

    await vi.advanceTimersByTimeAsync(699);
    expect(settled).toBe(false);

    await vi.advanceTimersByTimeAsync(1);
    const result = await pending;
    expect(settled).toBe(true);
    expect(result.response.status).toBe(200);
  });
});

describe("mock failures", () => {
  it("carries the same request details as a real failure", async () => {
    updateMockConfig((draft) => {
      draft.enabled = true;
      draft.writes = true;
      draft.delayMs = 0;
    });
    installApiMocks(application());

    // A passkey begin has no fixture. The extra field stands in for a secret a
    // real unmocked write could carry.
    const error = await client
      .POST("/api/prohibitorum/me/sudo/begin", {
        body: { method: "webauthn", password: "hunter2" } as never,
      })
      .catch((failure: unknown) => failure);

    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({
      kind: "http",
      status: 501,
      code: "mock_unmocked",
      details: { method: "POST", path: "/api/prohibitorum/me/sudo/begin" },
      exchange: {
        method: "POST",
        path: "/api/prohibitorum/me/sudo/begin",
        response: { status: 501 },
      },
    });
    const { exchange, requestId } = error as ApiError;
    expect(requestId).toMatch(/^mock-/);
    expect(JSON.parse(exchange?.requestBody ?? "")).toEqual({
      method: "webauthn",
      password: "••••••",
    });
    expect(exchange?.response?.body).toContain('"code": "mock_unmocked"');
  });
});

describe("mock session refresh", () => {
  it("moves a signed-in reader off the sign-in steps only", () => {
    expect(refreshTarget("/login", true)).toBe("/");
    expect(refreshTarget("/login/totp", true)).toBe("/");
    expect(refreshTarget("/login/recovery", true)).toBe("/");
    for (const path of [
      "/consent",
      "/saml-consent",
      "/error",
      "/maintenance",
      "/enroll/abc",
      "/welcome",
      "/pair",
      "/",
      "/security",
    ]) {
      expect(refreshTarget(path, true)).toBeUndefined();
    }
  });

  it("sends an anonymous reader to sign in from the console only", () => {
    expect(refreshTarget("/", false)).toBe("/login");
    expect(refreshTarget("/admin/users", false)).toBe("/login");
    for (const path of [
      "/login",
      "/login/totp",
      "/consent",
      "/saml-consent",
      "/error",
      "/maintenance",
      "/enroll/abc",
      "/welcome",
      "/setup-signin",
      "/federation/flow/f1",
      "/verify/vrchat/p1",
      "/pair",
    ]) {
      expect(refreshTarget(path, false)).toBeUndefined();
    }
    // A prefix only counts with the token after it.
    expect(refreshTarget("/enrollments", false)).toBe("/login");
  });
});
