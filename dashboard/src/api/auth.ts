import { base32 } from "@scure/base";
import {
  type AuthenticationResponseJSON,
  browserSupportsWebAuthn,
  type PublicKeyCredentialRequestOptionsJSON,
  startAuthentication,
  WebAuthnAbortService,
  WebAuthnError,
} from "@simplewebauthn/browser";
import { client, requireJsonData } from "@/api/client";
import { ApiError, isCancellation } from "@/api/errors";
import type { LoginResult, PublicConfig } from "@/api/raw-paths";

/**
 * Reads the single `return_to` parameter of a sign-in link. The value itself
 * needs no client-side validation: the server validates it before returning
 * the final redirect target. Only a duplicated parameter makes the link
 * malformed.
 */
export function parseReturnTo(search: string): string | undefined {
  const values = new URLSearchParams(search).getAll("return_to");
  if (values.length === 0) return undefined;
  if (values.length !== 1) {
    throw new ApiError({ kind: "local", code: "invalid_login_link" });
  }
  return values[0] ?? "";
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
    let body: AuthenticationResponseJSON;
    try {
      body = await startAuthentication({
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
        throw new DOMException("The request was aborted.", "AbortError");
      }
      throw new ApiError({ kind: "local", code: "passkey_incomplete" });
    }
    signal.throwIfAborted();
    const result = await requireJsonData(
      client.POST("/api/prohibitorum/auth/login/complete", {
        body,
        signal,
        ...(returnTo === undefined
          ? {}
          : { params: { query: { return_to: returnTo } } }),
      }),
    );
    signal.throwIfAborted();
    return validateLoginResult(result);
  } finally {
    if (passkeyController === controller) passkeyController = undefined;
  }
}
