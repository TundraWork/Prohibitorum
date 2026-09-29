import { base32 } from "@scure/base";
import {
  type AuthenticationResponseJSON,
  browserSupportsWebAuthn,
  browserSupportsWebAuthnAutofill,
  type PublicKeyCredentialCreationOptionsJSON,
  type PublicKeyCredentialRequestOptionsJSON,
  type RegistrationResponseJSON,
  startAuthentication,
  startRegistration,
  WebAuthnAbortService,
  WebAuthnError,
} from "@simplewebauthn/browser";
import { client, requireJsonData } from "@/api/client";
import { ApiError, isCancellation } from "@/api/errors";
import type { LoginResult, PublicConfig } from "@/api/raw-paths";
import type { SearchValue } from "@/app/search-params";

/**
 * The `return_to` of a sign-in link, as the address carried it. The value
 * itself needs no client-side validation: the server validates it before
 * returning the final redirect target. Only a malformed link is refused here:
 * one that names the parameter more than once.
 */
export function readReturnTo(
  value: SearchValue | undefined,
): string | undefined {
  if (Array.isArray(value)) {
    throw new ApiError({ kind: "local", code: "invalid_login_link" });
  }
  return value;
}

export function isValidLoginPassword(password: string): boolean {
  return (
    password.length > 0 && new TextEncoder().encode(password).byteLength <= 1024
  );
}

export function isValidTotpCode(code: string, digits: number): boolean {
  return (
    Number.isSafeInteger(digits) &&
    digits > 0 &&
    code.length === digits &&
    /^[0-9]+$/.test(code)
  );
}

export function isValidRecoveryCode(code: string): boolean {
  return code.length === 19 && /^[A-Z2-7]{4}(?:-[A-Z2-7]{4}){3}$/.test(code);
}

export function generateTotpSecret(): string {
  return base32.encode(crypto.getRandomValues(new Uint8Array(20)));
}

export function buildTotpUri(
  secret: string,
  username: string,
  config: PublicConfig["totp"],
): string {
  if (
    !config ||
    typeof config.issuer !== "string" ||
    config.issuer.length === 0 ||
    config.algorithm !== "SHA1" ||
    !Number.isSafeInteger(config.digits) ||
    config.digits <= 0 ||
    !Number.isSafeInteger(config.period) ||
    config.period <= 0 ||
    secret.length !== 32 ||
    !/^[A-Z2-7]{32}$/.test(secret) ||
    username.length === 0
  ) {
    throw new ApiError({ kind: "invalid-response" });
  }
  const label = `${encodeURIComponent(config.issuer)}:${encodeURIComponent(username)}`;
  const params = new URLSearchParams({
    secret,
    issuer: config.issuer,
    algorithm: config.algorithm,
    digits: String(config.digits),
    period: String(config.period),
  });
  return `otpauth://totp/${label}?${params}`;
}

export function validateLoginResult(result: LoginResult): LoginResult {
  if (
    typeof result !== "object" ||
    result === null ||
    typeof result.redirect !== "string"
  ) {
    throw new ApiError({ kind: "invalid-response" });
  }
  return result;
}

/** A signed passkey assertion and the RP ID the browser signed it for. */
export interface PasskeyAssertion {
  rpId: string;
  response: AuthenticationResponseJSON;
}

// The autofill ceremony is renewed this long before the server expires it, so
// a passkey picked just before renewal still has time for the biometric prompt
// and the round trip.
const autofillRenewMargin = 60_000;
// Consecutive browser refusals after which autofill stays off for this visit.
const autofillRefusalLimit = 3;

function abortError(): DOMException {
  return new DOMException("The request was aborted.", "AbortError");
}

/** The request options' RP ID, which a Signal to the password manager names. */
function requireRpId(options: { rpId?: string }): string {
  if (typeof options.rpId !== "string" || options.rpId.length === 0) {
    throw new ApiError({ kind: "invalid-response" });
  }
  return options.rpId;
}

let passkeyController: AbortController | undefined;

export function cancelPasskeyAuthentication(): void {
  passkeyController?.abort();
  WebAuthnAbortService.cancelCeremony();
}

export async function authenticateWithPasskey(
  returnTo?: string,
): Promise<LoginResult> {
  if (!window.isSecureContext || !browserSupportsWebAuthn()) {
    throw new ApiError({ kind: "local", code: "passkey_unsupported" });
  }
  cancelPasskeyAuthentication();
  const controller = new AbortController();
  passkeyController = controller;
  const { signal } = controller;
  try {
    const optionsJSON = await requireJsonData(
      client.POST("/api/prohibitorum/auth/login/begin", { signal }),
    );
    signal.throwIfAborted();
    const rpId = requireRpId(optionsJSON);
    let response: AuthenticationResponseJSON;
    try {
      response = await startAuthentication({
        // openapi-fetch's Readable maps extension BufferSource methods to objects.
        optionsJSON: optionsJSON as PublicKeyCredentialRequestOptionsJSON,
      });
    } catch (error) {
      if (
        signal.aborted ||
        isCancellation(error) ||
        (error instanceof WebAuthnError &&
          error.code === "ERROR_CEREMONY_ABORTED")
      ) {
        throw abortError();
      }
      throw new ApiError({ kind: "local", code: "passkey_incomplete" });
    }
    signal.throwIfAborted();
    const result = await completePasskeyLogin({
      assertion: { rpId, response },
      returnTo,
      signal,
    });
    signal.throwIfAborted();
    return result;
  } finally {
    if (passkeyController === controller) passkeyController = undefined;
  }
}

/**
 * Whether this page can offer passkeys in the username field's autofill: a
 * secure context whose browser supports conditional mediation.
 */
export async function isPasskeyAutofillAvailable(): Promise<boolean> {
  return window.isSecureContext && (await browserSupportsWebAuthnAutofill());
}

/**
 * Waits until the reader picks a passkey from the username field's autofill
 * and returns the signed assertion.
 *
 * The browser request stays pending while the page is open, so it is renewed
 * with a fresh server ceremony before that one expires. A refusal after a
 * passkey was picked (such as a dismissed biometric prompt) offers autofill
 * again, up to `autofillRefusalLimit` times in a row. Aborting `signal`, or
 * another ceremony taking over (the passkey button), rejects with an
 * `AbortError`; a failed begin or any other browser error is rethrown.
 */
export async function waitForPasskeyAutofill(
  signal: AbortSignal,
): Promise<PasskeyAssertion> {
  let refusals = 0;
  for (;;) {
    const optionsJSON = await requireJsonData(
      client.POST("/api/prohibitorum/auth/login/begin", {
        params: { query: { mediation: "conditional" } },
        signal,
      }),
    );
    signal.throwIfAborted();
    const rpId = requireRpId(optionsJSON);
    const { timeout } = optionsJSON;
    if (
      typeof timeout !== "number" ||
      !Number.isSafeInteger(timeout) ||
      timeout <= autofillRenewMargin
    ) {
      throw new ApiError({ kind: "invalid-response" });
    }
    const outcome = await awaitAutofillSelection(
      // openapi-fetch's Readable maps extension BufferSource methods to objects.
      optionsJSON as PublicKeyCredentialRequestOptionsJSON,
      timeout - autofillRenewMargin,
      signal,
    );
    if (outcome === "renew") continue;
    if (outcome === "refused") {
      refusals += 1;
      if (refusals >= autofillRefusalLimit) {
        throw new ApiError({ kind: "local", code: "passkey_incomplete" });
      }
      continue;
    }
    return { rpId, response: outcome };
  }
}

/**
 * One conditional browser request: the picked passkey's response, `"renew"`
 * once `renewAfter` passes without a pick, or `"refused"` when the browser
 * refused after a pick.
 */
async function awaitAutofillSelection(
  optionsJSON: PublicKeyCredentialRequestOptionsJSON,
  renewAfter: number,
  signal: AbortSignal,
): Promise<AuthenticationResponseJSON | "renew" | "refused"> {
  let renewing = false;
  let settled = false;
  const cancel = () => {
    if (!settled) WebAuthnAbortService.cancelCeremony();
  };
  const timer = setTimeout(() => {
    renewing = true;
    cancel();
  }, renewAfter);
  signal.addEventListener("abort", cancel, { once: true });
  try {
    return await startAuthentication({ optionsJSON, useBrowserAutofill: true });
  } catch (error) {
    const aborted =
      error instanceof WebAuthnError && error.code === "ERROR_CEREMONY_ABORTED";
    if (signal.aborted || (aborted && !renewing)) throw abortError();
    if (aborted) return "renew";
    if (
      (error instanceof Error || error instanceof DOMException) &&
      error.name === "NotAllowedError"
    ) {
      return "refused";
    }
    throw error;
  } finally {
    settled = true;
    clearTimeout(timer);
    signal.removeEventListener("abort", cancel);
  }
}

/**
 * Sends a signed assertion to the server to finish signing in. When the server
 * holds no such passkey, the password manager is told to remove it before the
 * error is rethrown.
 */
export async function completePasskeyLogin({
  assertion,
  mediation,
  returnTo,
  signal,
}: {
  assertion: PasskeyAssertion;
  mediation?: "conditional";
  returnTo?: string;
  signal?: AbortSignal;
}): Promise<LoginResult> {
  try {
    const result = await requireJsonData(
      client.POST("/api/prohibitorum/auth/login/complete", {
        body: assertion.response,
        signal,
        params: {
          query: {
            ...(mediation === undefined ? {} : { mediation }),
            ...(returnTo === undefined ? {} : { return_to: returnTo }),
          },
        },
      }),
    );
    return validateLoginResult(result);
  } catch (error) {
    if (
      error instanceof ApiError &&
      error.code === "login_credential_unknown"
    ) {
      await signalUnknownPasskey(assertion.rpId, assertion.response.id);
    }
    throw error;
  }
}

/**
 * Asks the password manager to remove a passkey this site no longer holds,
 * where the browser supports the Signal API. Its own failure is ignored: the
 * reader is shown the server's error either way.
 */
async function signalUnknownPasskey(
  rpId: string,
  credentialId: string,
): Promise<void> {
  // TypeScript's DOM types do not declare the Signal API yet.
  const credential = globalThis.PublicKeyCredential as
    | {
        signalUnknownCredential?: (options: {
          rpId: string;
          credentialId: string;
        }) => Promise<void>;
      }
    | undefined;
  try {
    await credential?.signalUnknownCredential?.({ rpId, credentialId });
  } catch {
    // Nothing to report: the password manager keeps the passkey.
  }
}

/**
 * Whether a failed browser ceremony was the reader backing out: the prompt
 * dismissed, or the ceremony aborted for another one.
 */
function isCeremonyCancelled(error: unknown): boolean {
  return (
    isCancellation(error) ||
    (error instanceof WebAuthnError &&
      error.code === "ERROR_CEREMONY_ABORTED") ||
    (error instanceof Error && error.name === "NotAllowedError")
  );
}

/**
 * Creates a passkey: `begin` asks the server for the challenge, the browser
 * makes the credential, and `complete` sends it back. The two requests are the
 * caller's, since an enrollment and a signed-in account register through
 * different endpoints.
 *
 * It follows the sign-in ceremony's rules: a browser or connection without
 * passkeys fails as `passkey_unsupported`, a dismissed prompt becomes an
 * `AbortError`, which is not reported, and any other failure of the prompt is
 * `registration_failed`.
 */
export async function registerWithPasskey<T>(
  begin: () => Promise<unknown>,
  complete: (attestation: RegistrationResponseJSON) => Promise<T>,
): Promise<T> {
  if (!window.isSecureContext || !browserSupportsWebAuthn()) {
    throw new ApiError({ kind: "local", code: "passkey_unsupported" });
  }
  const optionsJSON = await begin();
  let attestation: RegistrationResponseJSON;
  try {
    attestation = await startRegistration({
      // openapi-fetch's Readable maps extension BufferSource methods to objects.
      optionsJSON: optionsJSON as PublicKeyCredentialCreationOptionsJSON,
    });
  } catch (error) {
    if (isCeremonyCancelled(error)) {
      throw new DOMException("The request was aborted.", "AbortError");
    }
    throw new ApiError({ kind: "local", code: "registration_failed" });
  }
  return complete(attestation);
}
