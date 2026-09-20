import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import { isCancelledError } from "@tanstack/react-query";

export interface PublicError {
  code: string;
  details?: Record<string, unknown>;
  requestId: string;
}

export interface ApiErrorOptions {
  kind: "http" | "network" | "invalid-response" | "local";
  status?: number;
  code?: string;
  details?: Record<string, unknown>;
  requestId?: string;
}

export class ApiError extends Error {
  readonly kind: ApiErrorOptions["kind"];
  readonly status?: number;
  readonly code?: string;
  readonly details?: Record<string, unknown>;
  readonly requestId?: string;

  constructor(options: ApiErrorOptions, cause?: ErrorOptions) {
    super(options.kind, cause);
    this.name = "ApiError";
    this.kind = options.kind;
    this.status = options.status;
    this.code = options.code;
    this.details = options.details;
    this.requestId = options.requestId;
  }
}

export function isPublicError(value: unknown): value is PublicError {
  return (
    typeof value === "object" &&
    value !== null &&
    !Array.isArray(value) &&
    "code" in value &&
    typeof value.code === "string" &&
    value.code.length > 0 &&
    "requestId" in value &&
    typeof value.requestId === "string" &&
    (!("details" in value) ||
      value.details === undefined ||
      (typeof value.details === "object" &&
        value.details !== null &&
        !Array.isArray(value.details)))
  );
}

export function isCancellation(error: unknown): boolean {
  return (
    isCancelledError(error) ||
    (typeof error === "object" &&
      error !== null &&
      "name" in error &&
      error.name === "AbortError")
  );
}

const genericFailure = msg({
  id: "error.request_failed",
  message: "The request failed. Please try again.",
});

const errorMessages: Readonly<Record<string, MessageDescriptor>> = {
  validation_failed: msg({
    id: "error.validation_failed",
    message: "Check the submitted information and try again.",
  }),
  invalid_nickname: msg({
    id: "error.invalid_nickname",
    message: "Enter a valid nickname without control characters.",
  }),
  bad_request: msg({
    id: "error.bad_request",
    message: "The request could not be accepted.",
  }),
  request_too_large: msg({
    id: "error.request_too_large",
    message: "The submitted data is too large.",
  }),
  unsupported_media_type: msg({
    id: "error.unsupported_media_type",
    message: "This data format is not supported.",
  }),
  no_session: msg({
    id: "error.no_session",
    message: "Sign in to continue.",
  }),
  not_admin: msg({
    id: "error.not_admin",
    message: "You do not have permission to perform this action.",
  }),
  account_disabled: msg({
    id: "error.account_disabled",
    message: "This account is disabled.",
  }),
  sudo_required: msg({
    id: "error.sudo_required",
    message: "Verify your identity again to continue.",
  }),
  sudo_method_unavailable: msg({
    id: "error.sudo_method_unavailable",
    message: "That verification method is not available. Choose another one.",
  }),
  last_passkey: msg({
    id: "error.last_passkey",
    message:
      "Keep at least one passkey on the account. Add another one before removing this one.",
  }),
  last_sign_in_method: msg({
    id: "error.last_sign_in_method",
    message:
      "This is your only way to sign in, so it cannot be removed. Add another method first.",
  }),
  credential_not_found: msg({
    id: "error.credential_not_found",
    message: "That passkey is already gone. The list has been refreshed.",
  }),
  session_not_found: msg({
    id: "error.session_not_found",
    message: "That session has already ended. The list has been refreshed.",
  }),
  cannot_revoke_current_session: msg({
    id: "error.cannot_revoke_current_session",
    message: "This is the session you are using. Sign out instead.",
  }),
  pairing_not_found: msg({
    id: "error.pairing_not_found",
    message:
      "That pairing code is not valid. It may have been used already, or it may have expired.",
  }),
  pairing_expired: msg({
    id: "error.pairing_expired",
    message: "That pairing code has expired. Generate a new one on the device.",
  }),
  pairing_not_approved: msg({
    id: "error.pairing_not_approved",
    message: "This pairing is not ready to be approved yet.",
  }),
  pairing_state: msg({
    id: "error.pairing_state",
    message: "This pairing is no longer in a state that allows that action.",
  }),
  avatar_too_large: msg({
    id: "error.avatar_too_large",
    message: "That picture is larger than 5 MiB. Choose a smaller one.",
  }),
  avatar_invalid_image: msg({
    id: "error.avatar_invalid_image",
    message: "That file is not an image this instance can use.",
  }),
  avatar_source_unavailable: msg({
    id: "error.avatar_source_unavailable",
    message: "That picture is no longer available. Choose another source.",
  }),
  would_remove_last_factor: msg({
    id: "error.would_remove_last_factor",
    message:
      "This is your only way to sign in, so it cannot be removed. Add another method first.",
  }),
  bad_credentials: msg({
    id: "error.bad_credentials",
    message: "The credentials could not be verified.",
  }),
  maintenance_mode: msg({
    id: "error.maintenance_mode",
    message: "The service is undergoing maintenance. Please try again later.",
  }),
  rate_limited: msg({
    id: "error.rate_limited",
    message: "Too many requests. Please wait before trying again.",
  }),
  partial_session_invalid: msg({
    id: "error.partial_session_invalid",
    message: "This sign-in attempt has expired. Enter your password again.",
  }),
  factor_locked: msg({
    id: "error.factor_locked",
    message: "Too many verification attempts. Please wait before trying again.",
  }),
  ceremony_missing: msg({
    id: "error.ceremony_missing",
    message: "Start passkey sign-in again to continue.",
  }),
  ceremony_expired: msg({
    id: "error.ceremony_expired",
    message: "The passkey sign-in attempt has expired. Please start again.",
  }),
  ceremony_state_invalid: msg({
    id: "error.ceremony_state_invalid",
    message:
      "The passkey sign-in attempt could not be verified. Please start again.",
  }),
  login_failed: msg({
    id: "error.login_failed",
    message: "Passkey sign-in failed. Try again or use your password.",
  }),
  login_verification_failed: msg({
    id: "error.login_verification_failed",
    message:
      "The passkey could not be verified. Try again or use your password.",
  }),
  login_account_not_found: msg({
    id: "error.login_account_not_found",
    message:
      "The passkey could not be verified. Try again or use your password.",
  }),
  not_bootstrapped: msg({
    id: "error.not_bootstrapped",
    message:
      "This instance is not ready for sign-in. Contact its administrator.",
  }),
  passkey_incomplete: msg({
    id: "error.passkey_incomplete",
    message: "Verification was not completed. Try again or use your password.",
  }),
  passkey_unsupported: msg({
    id: "error.passkey_unsupported",
    message:
      "Passkeys are unavailable in this browser. Use your password instead.",
  }),
  invalid_login_link: msg({
    id: "error.invalid_login_link",
    message:
      "This sign-in link is invalid. Open a new sign-in link and try again.",
  }),
  server_error: msg({
    id: "error.server_error",
    message: "The server could not complete the request. Please try again.",
  }),
};

export type ErrorDescription = MessageDescriptor & { requestId?: string };

export function describeError(error: unknown): ErrorDescription {
  if (!(error instanceof ApiError)) return genericFailure;
  if (error.kind === "network") {
    return msg({
      id: "error.network",
      message:
        "Could not connect to the server. Check your connection and try again.",
    });
  }
  if (error.kind === "invalid-response") {
    return msg({
      id: "error.invalid_response",
      message: "The server returned an invalid response. Please try again.",
    });
  }
  const message =
    error.code && Object.hasOwn(errorMessages, error.code)
      ? (errorMessages[error.code] ?? genericFailure)
      : genericFailure;
  return {
    ...message,
    requestId:
      error.requestId && /^[A-Za-z0-9_-]{1,128}$/.test(error.requestId)
        ? error.requestId
        : undefined,
  };
}
