import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  completeSudoWithPasskey,
  completeSudoWithPasswordTotp,
} from "@/api/mutations";

const startAuthentication = vi.fn();
vi.mock("@simplewebauthn/browser", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@simplewebauthn/browser")>()),
  startAuthentication: (...args: unknown[]) => startAuthentication(...args),
  startRegistration: vi.fn(),
}));

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();

beforeEach(() => {
  fetchBoundary.mockReset();
  startAuthentication.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
});

afterEach(() => vi.unstubAllGlobals());

function calls() {
  return fetchBoundary.mock.calls.map(([request]) => ({
    path: new URL(request.url).pathname,
    body: request.body === null ? null : request.clone(),
  }));
}

/** Reads the recorded call at `index`, failing the test if it is absent. */
async function call(index: number) {
  const recorded = calls()[index];
  expect(recorded, `request ${index} was never made`).toBeDefined();
  return recorded as { path: string; body: Request | null };
}

async function bodyOf(index: number): Promise<Record<string, unknown>> {
  const recorded = await call(index);
  expect(recorded.body).not.toBeNull();
  return JSON.parse(await (recorded.body as Request).text());
}

describe("sudo completion bodies", () => {
  it("submits the raw assertion for the passkey method, with no method field", async () => {
    const assertion = {
      id: "credential-id",
      rawId: "credential-id",
      type: "public-key",
      response: { clientDataJSON: "a", authenticatorData: "b", signature: "c" },
      clientExtensionResults: {},
    };
    startAuthentication.mockResolvedValue(assertion);
    fetchBoundary.mockImplementation(async (request) => {
      const path = new URL(request.url).pathname;
      if (path.endsWith("/me/sudo/begin")) {
        return Response.json({ challenge: "abc", rpId: "localhost" });
      }
      return new Response(null, { status: 204 });
    });

    await completeSudoWithPasskey();

    // The dialog must hand the browser exactly what /begin returned, unwrapped:
    // there is no `publicKey` envelope on this endpoint.
    expect(startAuthentication).toHaveBeenCalledWith({
      optionsJSON: { challenge: "abc", rpId: "localhost" },
    });

    expect((await call(0)).path).toBe("/api/prohibitorum/me/sudo/begin");
    expect(await bodyOf(0)).toEqual({ method: "webauthn" });
    expect((await call(1)).path).toBe("/api/prohibitorum/me/sudo/complete");
    const submitted = await bodyOf(1);
    expect(submitted).toEqual(assertion);
    // Dispatch is by the stored intent, so the body must not name a method.
    expect("method" in submitted).toBe(false);
  });

  it("submits the password and code for the other method, with no assertion fields", async () => {
    fetchBoundary.mockImplementation(
      async () => new Response(null, { status: 204 }),
    );

    await completeSudoWithPasswordTotp({
      current_password: "correct horse",
      totp_code: "123456",
    });

    expect(await bodyOf(0)).toEqual({ method: "password_totp" });
    const submitted = await bodyOf(1);
    expect(submitted).toEqual({
      current_password: "correct horse",
      totp_code: "123456",
    });
    expect("method" in submitted).toBe(false);
    expect("id" in submitted).toBe(false);
    // Each method begins its own ceremony: exactly two requests, never a reuse.
    expect(calls()).toHaveLength(2);
  });

  it("surfaces a rejected verification instead of reporting success", async () => {
    fetchBoundary.mockImplementation(async (request) =>
      new URL(request.url).pathname.endsWith("/me/sudo/complete")
        ? Response.json(
            { code: "bad_credentials", requestId: "sudo-test" },
            { status: 401 },
          )
        : new Response(null, { status: 204 }),
    );

    await expect(
      completeSudoWithPasswordTotp({
        current_password: "wrong",
        totp_code: "000000",
      }),
    ).rejects.toMatchObject({ code: "bad_credentials", status: 401 });
  });

  it("reports an expired ceremony as such, so the dialog can start over", async () => {
    fetchBoundary.mockImplementation(async (request) =>
      new URL(request.url).pathname.endsWith("/me/sudo/begin")
        ? Response.json(
            { code: "sudo_method_unavailable", requestId: "sudo-test" },
            { status: 400 },
          )
        : new Response(null, { status: 204 }),
    );

    await expect(
      completeSudoWithPasswordTotp({
        current_password: "correct horse",
        totp_code: "123456",
      }),
    ).rejects.toMatchObject({ code: "sudo_method_unavailable", status: 400 });
    // Nothing reaches /complete when the method was refused up front.
    expect(calls()).toHaveLength(1);
  });
});
