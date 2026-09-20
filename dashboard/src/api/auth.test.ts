import { base32 } from "@scure/base";
import {
  browserSupportsWebAuthn,
  startAuthentication,
} from "@simplewebauthn/browser";
import { MutationObserver, type QueryClient } from "@tanstack/react-query";
import { waitFor } from "@testing-library/react";
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  expectTypeOf,
  it,
  vi,
} from "vitest";
import {
  buildTotpUri,
  cancelPasskeyAuthentication,
  generateTotpSecret,
  isValidLoginPassword,
  isValidRecoveryCode,
  isValidTotpCode,
  parseReturnTo,
} from "@/api/auth";
import { ApiError, describeError, isCancellation } from "@/api/errors";
import {
  passkeyMutationOptions,
  passwordMutationOptions,
  recoveryMutationOptions,
} from "@/api/mutations";
import type { RecoveryRequest } from "@/api/raw-paths";
import { createQueryClient } from "@/app/query-client";

vi.mock("@simplewebauthn/browser", () => ({
  browserSupportsWebAuthn: vi.fn(),
  startAuthentication: vi.fn(),
  WebAuthnAbortService: { cancelCeremony: vi.fn() },
  WebAuthnError: class extends Error {
    code = "";
  },
}));

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const clients: QueryClient[] = [];
const origin = "https://id.example";

beforeEach(() => {
  vi.resetAllMocks();
  vi.stubGlobal("fetch", fetchBoundary);
  vi.stubGlobal("isSecureContext", true);
  vi.mocked(browserSupportsWebAuthn).mockReturnValue(true);
});

afterEach(() => {
  cancelPasskeyAuthentication();
  for (const client of clients) client.clear();
  clients.length = 0;
  vi.unstubAllGlobals();
});

function createClient() {
  const notices = vi.fn<(error: unknown) => void>();
  const queryClient = createQueryClient(notices);
  clients.push(queryClient);
  return { queryClient, notices };
}

describe("authentication redirects", () => {
  it("preserves the parameter value exactly and distinguishes absence from an empty parameter", () => {
    expect(parseReturnTo("?unrelated=value")).toBeUndefined();
    for (const value of [
      "/oauth/authorize?state=a%2Bb",
      `${origin}/saml/sso?RelayState=x`,
      "/",
      "",
    ]) {
      expect(parseReturnTo(`?return_to=${encodeURIComponent(value)}`)).toBe(
        value,
      );
    }
  });

  it("rejects duplicate parameters including encoded names", () => {
    expect(() => parseReturnTo("?return_to=%2F&return%5fto=%2F")).toThrow(
      ApiError,
    );
  });
});

describe("strict authentication inputs", () => {
  it("measures passwords as UTF-8 bytes without imposing a new-password minimum", () => {
    expect(isValidLoginPassword("x")).toBe(true);
    expect(isValidLoginPassword(" ")).toBe(true);
    expect(isValidLoginPassword("")).toBe(false);
    expect(isValidLoginPassword("é".repeat(512))).toBe(true);
    expect(isValidLoginPassword("é".repeat(513))).toBe(false);
  });

  it("preserves leading zeroes and rejects normalized or non-ASCII factors", () => {
    expect(isValidTotpCode("001234", 6)).toBe(true);
    expect(isValidTotpCode("00123456", 8)).toBe(true);
    expect(isValidTotpCode("1234", 6)).toBe(false);
    expect(isValidTotpCode("１２３４５６", 6)).toBe(false);
    expect(isValidTotpCode("12345\n", 6)).toBe(false);
    expect(isValidRecoveryCode("ABCD-EFGH-IJKL-MN23")).toBe(true);
    expect(isValidRecoveryCode("abcd-efgh-ijkl-mn23")).toBe(false);
    expect(isValidRecoveryCode("ABCDEFGHIJKLMNOP")).toBe(false);
    expect(isValidRecoveryCode("ABCD-EFGH-IJKL-MN23\n")).toBe(false);
  });

  it("rejects incomplete or contradictory recovery field combinations at the type boundary", () => {
    expectTypeOf<{
      partial_session_token: string;
      code: string;
      reset_authenticator: true;
    }>().not.toMatchTypeOf<RecoveryRequest>();
    expectTypeOf<{
      partial_session_token: string;
      code: string;
      reset_authenticator: false;
      totp_secret_base32: string;
      totp_code: string;
    }>().not.toMatchTypeOf<RecoveryRequest>();
  });

  it("encodes generated secret bytes and keeps URI labels and parameters separate", () => {
    const secret = generateTotpSecret();
    expect(secret).toMatch(/^[A-Z2-7]{32}$/);
    expect(base32.decode(secret)).toHaveLength(20);
    const config = {
      issuer: "Example & Co",
      algorithm: "SHA1",
      digits: 8,
      period: 45,
    };
    const uri = new URL(buildTotpUri(secret, "user+name/a?b", config));
    expect(uri.protocol).toBe("otpauth:");
    expect(uri.hostname).toBe("totp");
    expect(decodeURIComponent(uri.pathname)).toBe(
      "/Example & Co:user+name/a?b",
    );
    expect(Object.fromEntries(uri.searchParams)).toEqual({
      secret,
      issuer: config.issuer,
      algorithm: "SHA1",
      digits: "8",
      period: "45",
    });
    expect(() =>
      buildTotpUri(secret, "user", { ...config, period: 0 }),
    ).toThrow(ApiError);
    expect(() =>
      buildTotpUri(secret, "user", { ...config, issuer: "" }),
    ).toThrow(ApiError);
    expect(() =>
      buildTotpUri(secret, "user", { ...config, algorithm: "SHA256" }),
    ).toThrow(ApiError);
  });
});

describe("authentication mutations", () => {
  it("rejects reset success without newly issued codes", async () => {
    const { queryClient, notices } = createClient();
    fetchBoundary.mockResolvedValueOnce(Response.json({ redirect: "/" }));
    const recovery = new MutationObserver(
      queryClient,
      recoveryMutationOptions(),
    );
    await expect(
      recovery.mutate({
        partial_session_token: "single-use",
        code: "ABCD-EFGH-IJKL-MN23",
        reset_authenticator: true,
        totp_secret_base32: "A".repeat(32),
        totp_code: "001234",
      }),
    ).rejects.toMatchObject({ kind: "invalid-response" });
    expect(notices).toHaveBeenCalledTimes(1);
    expect(fetchBoundary).toHaveBeenCalledTimes(1);
    recovery.reset();
  });

  it("removes sensitive password results and variables after reset", async () => {
    const { queryClient } = createClient();
    fetchBoundary.mockResolvedValueOnce(
      Response.json({ partial_session_token: "one-use-token" }),
    );
    const mutation = new MutationObserver(
      queryClient,
      passwordMutationOptions(),
    );
    const unsubscribe = mutation.subscribe(() => {});
    await mutation.mutate({ username: "example", password: "secret-password" });
    mutation.reset();
    await waitFor(() =>
      expect(queryClient.getMutationCache().getAll()).toHaveLength(0),
    );
    expect(mutation.getCurrentResult().data).toBeUndefined();
    expect(mutation.getCurrentResult().variables).toBeUndefined();
    unsubscribe();
  });

  it("reports browser refusal once and begins a new ceremony on retry", async () => {
    const { queryClient, notices } = createClient();
    fetchBoundary.mockImplementation(async () =>
      Response.json({ challenge: "new-challenge" }),
    );
    vi.mocked(startAuthentication).mockRejectedValue(
      new DOMException("private browser details", "NotAllowedError"),
    );
    const mutation = new MutationObserver(
      queryClient,
      passkeyMutationOptions(),
    );
    const error = await mutation.mutate().catch((failure: unknown) => failure);
    expect(error).toMatchObject({ code: "passkey_incomplete" });
    expect(JSON.stringify(describeError(error))).not.toContain(
      "private browser details",
    );
    expect(notices).toHaveBeenCalledTimes(1);
    expect(fetchBoundary).toHaveBeenCalledTimes(1);
    await expect(mutation.mutate()).rejects.toMatchObject({
      code: "passkey_incomplete",
    });
    expect(fetchBoundary).toHaveBeenCalledTimes(2);
    expect(vi.mocked(startAuthentication)).toHaveBeenCalledTimes(2);
    mutation.reset();
  });

  it("does not open a browser ceremony when a cancelled begin response arrives late", async () => {
    const { queryClient, notices } = createClient();
    let release!: (response: Response) => void;
    fetchBoundary.mockImplementation(
      () =>
        new Promise<Response>((resolve) => {
          release = resolve;
        }),
    );
    const mutation = new MutationObserver(
      queryClient,
      passkeyMutationOptions(),
    );
    const pending = mutation.mutate().catch((error: unknown) => error);
    await waitFor(() => expect(fetchBoundary).toHaveBeenCalledTimes(1));
    cancelPasskeyAuthentication();
    release(Response.json({ challenge: "late-challenge" }));
    expect(isCancellation(await pending)).toBe(true);
    expect(startAuthentication).not.toHaveBeenCalled();
    expect(notices).not.toHaveBeenCalled();
    mutation.reset();
  });
});
