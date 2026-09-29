import { base32 } from "@scure/base";
import {
  browserSupportsWebAuthn,
  browserSupportsWebAuthnAutofill,
  startAuthentication,
  startRegistration,
  WebAuthnAbortService,
  WebAuthnError,
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
  authenticateWithPasskey,
  buildTotpUri,
  cancelPasskeyAuthentication,
  completePasskeyLogin,
  generateTotpSecret,
  isPasskeyAutofillAvailable,
  isValidLoginPassword,
  isValidRecoveryCode,
  isValidTotpCode,
  type PasskeyAssertion,
  readReturnTo,
  registerWithPasskey,
  waitForPasskeyAutofill,
} from "@/api/auth";
import { ApiError, describeError, isCancellation } from "@/api/errors";
import {
  passkeyMutationOptions,
  passwordMutationOptions,
  recoveryMutationOptions,
} from "@/api/mutations";
import type { RecoveryRequest } from "@/api/raw-paths";
import { createQueryClient } from "@/app/query-client";
import { parseSearch } from "@/app/search-params";

vi.mock("@simplewebauthn/browser", () => ({
  browserSupportsWebAuthn: vi.fn(),
  browserSupportsWebAuthnAutofill: vi.fn(),
  startAuthentication: vi.fn(),
  startRegistration: vi.fn(),
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
  // The sign-in page reads the search the router has already parsed.
  const read = (search: string) => readReturnTo(parseSearch(search).return_to);

  it("preserves the parameter value exactly and distinguishes absence from an empty parameter", () => {
    expect(read("?unrelated=value")).toBeUndefined();
    for (const value of [
      "/oauth/authorize?state=a%2Bb",
      `${origin}/saml/sso?RelayState=x`,
      "/",
      "",
    ]) {
      expect(read(`?return_to=${encodeURIComponent(value)}`)).toBe(value);
    }
  });

  it("rejects duplicate parameters including encoded names", () => {
    expect(() => read("?return_to=%2F&return_to=%2Fapps")).toThrow(ApiError);
    expect(() => read("?return_to=%2F&return%5fto=%2F")).toThrow(ApiError);
  });

  it("keeps a value that looks like a number or JSON as the address carried it", () => {
    expect(read("?return_to=12345")).toBe("12345");
    expect(read("?return_to=12e45678")).toBe("12e45678");
    expect(read("?return_to=%5B%22%2Fa%22%5D")).toBe('["/a"]');
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
      Response.json({ challenge: "new-challenge", rpId: "id.example" }),
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
    release(Response.json({ challenge: "late-challenge", rpId: "id.example" }));
    expect(isCancellation(await pending)).toBe(true);
    expect(startAuthentication).not.toHaveBeenCalled();
    expect(notices).not.toHaveBeenCalled();
    mutation.reset();
  });
});

describe("registerWithPasskey", () => {
  const attestation = { id: "new-credential" };

  function ceremony() {
    const begin = vi.fn(async () => ({ challenge: "c" }));
    const complete = vi.fn(async (value: unknown) => ({ created: value }));
    return { begin, complete };
  }

  async function failure(run: Promise<unknown>): Promise<unknown> {
    try {
      await run;
    } catch (error) {
      return error;
    }
    throw new Error("expected the ceremony to fail");
  }

  it("refuses a browser or connection without passkeys before asking the server", async () => {
    for (const setup of [
      () => vi.stubGlobal("isSecureContext", false),
      () => vi.mocked(browserSupportsWebAuthn).mockReturnValue(false),
    ]) {
      vi.stubGlobal("isSecureContext", true);
      vi.mocked(browserSupportsWebAuthn).mockReturnValue(true);
      setup();
      const { begin, complete } = ceremony();
      const error = await failure(registerWithPasskey(begin, complete));
      expect(error).toBeInstanceOf(ApiError);
      expect((error as ApiError).code).toBe("passkey_unsupported");
      expect(begin).not.toHaveBeenCalled();
      expect(complete).not.toHaveBeenCalled();
    }
  });

  it("turns a dismissed or aborted prompt into a cancellation", async () => {
    const aborted = new WebAuthnError({
      message: "aborted",
      code: "ERROR_CEREMONY_ABORTED",
    } as never);
    Object.assign(aborted, { code: "ERROR_CEREMONY_ABORTED" });
    const dismissed = new Error("dismissed");
    dismissed.name = "NotAllowedError";
    for (const thrown of [dismissed, aborted]) {
      vi.mocked(startRegistration).mockRejectedValueOnce(thrown);
      const { begin, complete } = ceremony();
      const error = await failure(registerWithPasskey(begin, complete));
      expect(isCancellation(error)).toBe(true);
      expect(begin).toHaveBeenCalledOnce();
      expect(complete).not.toHaveBeenCalled();
    }
  });

  it("reports any other failure of the prompt as registration_failed", async () => {
    vi.mocked(startRegistration).mockRejectedValueOnce(
      new Error("InvalidStateError"),
    );
    const { begin, complete } = ceremony();
    const error = await failure(registerWithPasskey(begin, complete));
    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).code).toBe("registration_failed");
    expect(describeError(error).id).toBe("error.registration_failed");
    expect(complete).not.toHaveBeenCalled();
  });

  it("hands the browser's attestation to complete and returns its answer", async () => {
    vi.mocked(startRegistration).mockResolvedValueOnce(attestation as never);
    const { begin, complete } = ceremony();
    await expect(registerWithPasskey(begin, complete)).resolves.toEqual({
      created: attestation,
    });
    expect(vi.mocked(startRegistration).mock.calls[0]?.[0]).toEqual({
      optionsJSON: { challenge: "c" },
    });
    expect(complete).toHaveBeenCalledWith(attestation);
  });
});

describe("passkey autofill", () => {
  const picked = {
    id: "credential-id",
    rawId: "credential-id",
    type: "public-key",
    response: {
      authenticatorData: "a",
      clientDataJSON: "c",
      signature: "s",
      userHandle: "u",
    },
    clientExtensionResults: {},
  } as const;

  function options(challenge: string, extra: object = {}) {
    return Response.json({
      challenge,
      rpId: "id.example",
      timeout: 300_000,
      ...extra,
    });
  }

  function requestUrl(call: number): URL {
    const request = fetchBoundary.mock.calls[call]?.[0];
    if (!request) throw new Error(`no request ${call}`);
    return new URL(request.url);
  }

  function aborted() {
    const error = new WebAuthnError({} as never);
    Object.assign(error, { code: "ERROR_CEREMONY_ABORTED" });
    return error;
  }

  function refused() {
    return new DOMException("private browser details", "NotAllowedError");
  }

  /** A browser request that stays open until the ceremony is cancelled. */
  function pendingUntilCancelled() {
    return new Promise<never>((_, reject) => {
      vi.mocked(WebAuthnAbortService.cancelCeremony).mockImplementationOnce(
        () => reject(aborted()),
      );
    });
  }

  afterEach(() => {
    vi.useRealTimers();
  });

  it("is available only in a secure context whose browser supports it", async () => {
    vi.mocked(browserSupportsWebAuthnAutofill).mockResolvedValue(true);
    await expect(isPasskeyAutofillAvailable()).resolves.toBe(true);
    vi.mocked(browserSupportsWebAuthnAutofill).mockResolvedValue(false);
    await expect(isPasskeyAutofillAvailable()).resolves.toBe(false);
    vi.mocked(browserSupportsWebAuthnAutofill).mockResolvedValue(true);
    vi.stubGlobal("isSecureContext", false);
    await expect(isPasskeyAutofillAvailable()).resolves.toBe(false);
  });

  it("begins a conditional ceremony and returns the passkey picked from autofill", async () => {
    fetchBoundary.mockResolvedValueOnce(options("autofill-challenge"));
    vi.mocked(startAuthentication).mockResolvedValueOnce(picked as never);
    await expect(
      waitForPasskeyAutofill(new AbortController().signal),
    ).resolves.toEqual({ rpId: "id.example", response: picked });
    expect(requestUrl(0).pathname).toBe("/api/prohibitorum/auth/login/begin");
    expect(requestUrl(0).search).toBe("?mediation=conditional");
    expect(vi.mocked(startAuthentication)).toHaveBeenCalledExactlyOnceWith({
      optionsJSON: expect.objectContaining({
        challenge: "autofill-challenge",
      }) as never,
      useBrowserAutofill: true,
    });
  });

  it("refuses begin options without an RP ID or a timeout past the renewal margin", async () => {
    for (const extra of [
      { rpId: undefined },
      { rpId: "" },
      { timeout: undefined },
      { timeout: 60_000 },
      { timeout: 90_000.5 },
      { timeout: "300000" },
    ]) {
      fetchBoundary.mockResolvedValueOnce(options("c", extra));
      await expect(
        waitForPasskeyAutofill(new AbortController().signal),
      ).rejects.toMatchObject({ kind: "invalid-response" });
    }
    expect(startAuthentication).not.toHaveBeenCalled();
  });

  it("renews the ceremony a minute before the server lets it expire", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    fetchBoundary
      .mockResolvedValueOnce(options("first", { timeout: 120_000 }))
      .mockResolvedValueOnce(options("second"));
    vi.mocked(startAuthentication)
      .mockImplementationOnce(pendingUntilCancelled)
      .mockResolvedValueOnce(picked as never);
    const waiting = waitForPasskeyAutofill(new AbortController().signal);
    await vi.advanceTimersByTimeAsync(59_999);
    expect(WebAuthnAbortService.cancelCeremony).not.toHaveBeenCalled();
    expect(fetchBoundary).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    await expect(waiting).resolves.toEqual({
      rpId: "id.example",
      response: picked,
    });
    expect(WebAuthnAbortService.cancelCeremony).toHaveBeenCalledOnce();
    expect(fetchBoundary).toHaveBeenCalledTimes(2);
    expect(requestUrl(1).search).toBe("?mediation=conditional");
    expect(
      vi.mocked(startAuthentication).mock.calls[1]?.[0].optionsJSON.challenge,
    ).toBe("second");
  });

  it("offers autofill again after a refusal and gives up after three in a row", async () => {
    fetchBoundary.mockImplementation(async () => options("c"));
    vi.mocked(startAuthentication)
      .mockRejectedValueOnce(refused())
      .mockRejectedValueOnce(refused())
      .mockResolvedValueOnce(picked as never);
    await expect(
      waitForPasskeyAutofill(new AbortController().signal),
    ).resolves.toMatchObject({ response: picked });
    expect(fetchBoundary).toHaveBeenCalledTimes(3);

    fetchBoundary.mockClear();
    vi.mocked(startAuthentication).mockRejectedValue(refused());
    await expect(
      waitForPasskeyAutofill(new AbortController().signal),
    ).rejects.toBeInstanceOf(ApiError);
    expect(fetchBoundary).toHaveBeenCalledTimes(3);
  });

  it("becomes a cancellation when its signal aborts or another ceremony takes over", async () => {
    fetchBoundary.mockImplementation(async () => options("c"));
    vi.mocked(startAuthentication).mockRejectedValueOnce(aborted());
    const takenOver = await waitForPasskeyAutofill(
      new AbortController().signal,
    ).catch((error: unknown) => error);
    expect(isCancellation(takenOver)).toBe(true);
    expect(fetchBoundary).toHaveBeenCalledTimes(1);

    vi.mocked(startAuthentication).mockImplementationOnce(
      pendingUntilCancelled,
    );
    const controller = new AbortController();
    const waiting = waitForPasskeyAutofill(controller.signal).catch(
      (error: unknown) => error,
    );
    await waitFor(() => expect(startAuthentication).toHaveBeenCalledTimes(2));
    controller.abort();
    expect(isCancellation(await waiting)).toBe(true);
    expect(WebAuthnAbortService.cancelCeremony).toHaveBeenCalledOnce();
    expect(fetchBoundary).toHaveBeenCalledTimes(2);
  });

  it("rethrows a failed begin and any other browser error", async () => {
    fetchBoundary.mockResolvedValueOnce(
      Response.json({ code: "mock_unmocked", requestId: "r" }, { status: 501 }),
    );
    await expect(
      waitForPasskeyAutofill(new AbortController().signal),
    ).rejects.toMatchObject({ code: "mock_unmocked" });
    fetchBoundary.mockResolvedValueOnce(options("c"));
    const failure = new Error("Browser does not support WebAuthn autofill");
    vi.mocked(startAuthentication).mockRejectedValueOnce(failure);
    await expect(
      waitForPasskeyAutofill(new AbortController().signal),
    ).rejects.toBe(failure);
  });
});

describe("completing a passkey sign-in", () => {
  const assertion: PasskeyAssertion = {
    rpId: "id.example",
    response: {
      id: "credential-id",
      rawId: "credential-id",
      type: "public-key",
      response: {
        authenticatorData: "a",
        clientDataJSON: "c",
        signature: "s",
      },
      clientExtensionResults: {},
    },
  };

  function refusedWith(code: string) {
    return Response.json({ code, requestId: "r" }, { status: 401 });
  }

  function stubSignal(result: Promise<void> = Promise.resolve()) {
    const signalUnknownCredential = vi.fn(() => result);
    vi.stubGlobal("PublicKeyCredential", { signalUnknownCredential });
    return signalUnknownCredential;
  }

  it("names the conditional mediation and return_to on the request", async () => {
    fetchBoundary.mockResolvedValueOnce(Response.json({ redirect: "/apps" }));
    await expect(
      completePasskeyLogin({
        assertion,
        mediation: "conditional",
        returnTo: "/apps",
      }),
    ).resolves.toEqual({ redirect: "/apps" });
    const request = fetchBoundary.mock.calls[0]?.[0];
    const url = new URL(request?.url ?? "");
    expect(url.pathname).toBe("/api/prohibitorum/auth/login/complete");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      mediation: "conditional",
      return_to: "/apps",
    });
    expect(await request?.json()).toEqual(assertion.response);

    fetchBoundary.mockResolvedValueOnce(Response.json({ redirect: "/" }));
    await completePasskeyLogin({ assertion });
    expect(new URL(fetchBoundary.mock.calls[1]?.[0].url ?? "").search).toBe("");
  });

  it("tells the password manager about a passkey the server does not hold, then fails", async () => {
    const signalUnknownCredential = stubSignal();
    fetchBoundary.mockResolvedValueOnce(
      refusedWith("login_credential_unknown"),
    );
    const error = await completePasskeyLogin({ assertion }).catch(
      (failure: unknown) => failure,
    );
    expect(error).toMatchObject({ code: "login_credential_unknown" });
    expect(describeError(error).id).toBe("error.login_credential_unknown");
    expect(signalUnknownCredential).toHaveBeenCalledExactlyOnceWith({
      rpId: "id.example",
      credentialId: "credential-id",
    });
  });

  it("fails with the server's error where the Signal API is missing or fails", async () => {
    for (const setup of [
      () => vi.stubGlobal("PublicKeyCredential", class {}),
      () => vi.stubGlobal("PublicKeyCredential", undefined),
      () => stubSignal(Promise.reject(new Error("signal failed"))),
    ]) {
      setup();
      fetchBoundary.mockResolvedValueOnce(
        refusedWith("login_credential_unknown"),
      );
      await expect(completePasskeyLogin({ assertion })).rejects.toMatchObject({
        code: "login_credential_unknown",
      });
    }
  });

  it("leaves the password manager alone for any other failure", async () => {
    const signalUnknownCredential = stubSignal();
    fetchBoundary.mockResolvedValueOnce(
      refusedWith("login_verification_failed"),
    );
    await expect(completePasskeyLogin({ assertion })).rejects.toMatchObject({
      code: "login_verification_failed",
    });
    expect(signalUnknownCredential).not.toHaveBeenCalled();
  });

  it("signals from the passkey button too", async () => {
    const signalUnknownCredential = stubSignal();
    fetchBoundary
      .mockResolvedValueOnce(
        Response.json({ challenge: "c", rpId: "id.example", timeout: 60_000 }),
      )
      .mockResolvedValueOnce(refusedWith("login_credential_unknown"));
    vi.mocked(startAuthentication).mockResolvedValueOnce(
      assertion.response as never,
    );
    await expect(authenticateWithPasskey()).rejects.toMatchObject({
      code: "login_credential_unknown",
    });
    expect(signalUnknownCredential).toHaveBeenCalledExactlyOnceWith({
      rpId: "id.example",
      credentialId: "credential-id",
    });
    expect(new URL(fetchBoundary.mock.calls[0]?.[0].url ?? "").search).toBe("");
  });

  it("refuses begin options without an RP ID before opening the browser prompt", async () => {
    fetchBoundary.mockResolvedValueOnce(Response.json({ challenge: "c" }));
    await expect(authenticateWithPasskey()).rejects.toMatchObject({
      kind: "invalid-response",
    });
    expect(startAuthentication).not.toHaveBeenCalled();
  });
});
