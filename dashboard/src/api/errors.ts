import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import { isCancelledError } from "@tanstack/react-query";
import type { RequestExchange } from "@/api/exchange";

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
  /** The failed request and its response, for the details dialog only. */
  exchange?: RequestExchange;
}

export class ApiError extends Error {
  readonly kind: ApiErrorOptions["kind"];
  readonly status?: number;
  readonly code?: string;
  readonly details?: Record<string, unknown>;
  readonly requestId?: string;
  readonly exchange?: RequestExchange;

  constructor(options: ApiErrorOptions, cause?: ErrorOptions) {
    super(options.kind, cause);
    this.name = "ApiError";
    this.kind = options.kind;
    this.status = options.status;
    this.code = options.code;
    this.details = options.details;
    this.requestId = options.requestId;
    this.exchange = options.exchange;
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

/**
 * Whether a failure is the user or the app backing out rather than something
 * going wrong: an aborted request, a cancelled query, or a dismissed identity
 * check (`SudoCancelled`, matched by name because the sudo module builds on
 * this one). None of these is reported.
 */
export function isCancellation(error: unknown): boolean {
  return (
    isCancelledError(error) ||
    (typeof error === "object" &&
      error !== null &&
      "name" in error &&
      (error.name === "AbortError" || error.name === "SudoCancelled"))
  );
}

const genericFailure = msg({
  id: "error.request_failed",
  message: "The request failed.",
});

const errorMessages: Readonly<Record<string, MessageDescriptor>> = {
  validation_failed: msg({
    id: "error.validation_failed",
    message: "The submitted information is not valid.",
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
    message: "This requires an administrator.",
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
    message: "That passkey is already gone.",
  }),
  session_not_found: msg({
    id: "error.session_not_found",
    message: "That session has already ended.",
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
    message: "The service is undergoing maintenance.",
  }),
  rate_limited: msg({
    id: "error.rate_limited",
    message: "Too many requests. Wait a moment.",
  }),
  partial_session_invalid: msg({
    id: "error.partial_session_invalid",
    message: "This sign-in attempt has expired. Enter your password again.",
  }),
  factor_locked: msg({
    id: "error.factor_locked",
    message: "Too many verification attempts. Wait a moment.",
  }),
  ceremony_missing: msg({
    id: "error.ceremony_missing",
    message: "This passkey sign-in could not be found.",
  }),
  ceremony_expired: msg({
    id: "error.ceremony_expired",
    message: "The passkey sign-in attempt has expired.",
  }),
  ceremony_state_invalid: msg({
    id: "error.ceremony_state_invalid",
    message: "The passkey sign-in attempt could not be verified.",
  }),
  login_failed: msg({
    id: "error.login_failed",
    message: "Passkey sign-in failed. You can use your password instead.",
  }),
  login_verification_failed: msg({
    id: "error.login_verification_failed",
    message:
      "The passkey could not be verified. You can use your password instead.",
  }),
  login_account_not_found: msg({
    id: "error.login_account_not_found",
    message:
      "The passkey could not be verified. You can use your password instead.",
  }),
  not_bootstrapped: msg({
    id: "error.not_bootstrapped",
    message:
      "This instance is not ready for sign-in. Contact its administrator.",
  }),
  passkey_incomplete: msg({
    id: "error.passkey_incomplete",
    message:
      "Verification was not completed. You can use your password instead.",
  }),
  passkey_unsupported: msg({
    id: "error.passkey_unsupported",
    message:
      "Passkeys are unavailable in this browser. Use your password instead.",
  }),
  invalid_login_link: msg({
    id: "error.invalid_login_link",
    message: "This sign-in link is invalid. Open a new sign-in link.",
  }),
  server_error: msg({
    id: "error.server_error",
    message: "The server could not complete the request.",
  }),

  /* Management. These arrive on writes the console's admin pages make, and each
     one has an action the reader can take, so none of them may fall through to
     the generic failure above. */
  last_admin: msg({
    id: "error.last_admin",
    message:
      "This is the only administrator. Make another account an administrator first.",
  }),
  admin_cannot_be_disabled: msg({
    id: "error.admin_cannot_be_disabled",
    message:
      "An administrator cannot be disabled. Change the role to user first.",
  }),
  cannot_delete_self: msg({
    id: "error.cannot_delete_self",
    message: "You cannot delete your own account. Ask another administrator.",
  }),
  invalid_role: msg({
    id: "error.invalid_role",
    message: "That role is not one this instance accepts.",
  }),
  username_immutable: msg({
    id: "error.username_immutable",
    message: "A username cannot be changed once the account exists.",
  }),
  invalid_username: msg({
    id: "error.invalid_username",
    message: "That username is not in a format this instance accepts.",
  }),
  username_taken: msg({
    id: "error.username_taken",
    message: "That username is already taken.",
  }),
  account_not_found: msg({
    id: "error.account_not_found",
    message: "That account no longer exists.",
  }),
  invitation_not_found: msg({
    id: "error.invitation_not_found",
    message: "That invitation has already been revoked or used.",
  }),
  invitation_groups_unavailable: msg({
    id: "error.invitation_groups_unavailable",
    message: "One of the chosen user groups no longer exists. Choose again.",
  }),
  upstream_idp_not_found: msg({
    id: "error.upstream_idp_not_found",
    message:
      "That provider is not available. It may have been deleted or disabled.",
  }),
  group_not_found: msg({
    id: "error.group_not_found",
    message: "That user group no longer exists.",
  }),
  group_in_use: msg({
    id: "error.group_in_use",
    message:
      "One or more applications still use this user group. Remove it from them before deleting it.",
  }),
  invalid_group_rule: msg({
    id: "error.invalid_group_rule",
    message: "The server did not accept this rule.",
  }),
  active_key_no_replacement: msg({
    id: "error.active_key_no_replacement",
    message: "Activate another key first, then retire this one.",
  }),
  upstream_idp_already_exists: msg({
    id: "error.upstream_idp_already_exists",
    message: "That identifier is already taken by another provider.",
  }),
  provider_not_ready: msg({
    id: "error.provider_not_ready",
    message:
      "This provider is not ready yet. Finish setting up its credentials before enabling it.",
  }),
  oidc_client_already_exists: msg({
    id: "error.oidc_client_already_exists",
    message: "That Client ID or host name is already in use.",
  }),
  saml_application_already_exists: msg({
    id: "error.saml_application_already_exists",
    message: "An application with that Entity ID already exists.",
  }),
  client_not_found: msg({
    id: "error.client_not_found",
    message: "That application no longer exists.",
  }),
  invalid_manager_role: msg({
    id: "error.invalid_manager_role",
    message: "A disabled account cannot manage an application.",
  }),
  // VRChat operator sign-in: the code is wrong, or the whole challenge expired.
  vrchat_operator_code_invalid: msg({
    id: "error.vrchat_operator_code_invalid",
    message: "That code is not valid.",
  }),
  vrchat_operator_credentials_invalid: msg({
    id: "error.vrchat_operator_credentials_invalid",
    message: "Those VRChat credentials were not accepted.",
  }),
  vrchat_operator_challenge_invalid: msg({
    id: "error.vrchat_operator_challenge_invalid",
    message: "This sign-in took too long. Start again with your password.",
  }),
  upstream_rate_limited: msg({
    id: "error.upstream_rate_limited",
    message: "The provider is asking us to slow down. Wait a moment.",
  }),
  upstream_temporarily_unavailable: msg({
    id: "error.upstream_temporarily_unavailable",
    message: "Could not reach the provider.",
  }),
  // A diagnostic run is single-use and short-lived. The console does not treat
  // this as a signed-out session: only `no_session` means that.
  federation_state_invalid: msg({
    id: "error.federation_state_invalid",
    message: "This test is no longer valid. Start a new one.",
  }),
};

/**
 * Where a request was made, for the few codes whose meaning depends on it.
 * A mutation names its scope as `meta.errorScope`, and the query client hands
 * it on, so the toast reads the scoped wording before the general one.
 */
export type ErrorScope = "signing-key";

const scopedErrorMessages: Readonly<
  Record<ErrorScope, Readonly<Record<string, MessageDescriptor>>>
> = {
  // The key handlers reuse `credential_not_found` for a key that is gone or no
  // longer pending, and the general wording for that code names a passkey.
  "signing-key": {
    credential_not_found: msg({
      id: "error.signing-key.credential_not_found",
      message: "This key is no longer pending.",
    }),
  },
};

export type ErrorDescription = MessageDescriptor & { requestId?: string };

export function describeError(
  error: unknown,
  scope?: ErrorScope,
): ErrorDescription {
  if (!(error instanceof ApiError)) return genericFailure;
  if (error.kind === "network") {
    return msg({
      id: "error.network",
      message: "Could not connect to the server. Check your connection.",
    });
  }
  if (error.kind === "invalid-response") {
    return msg({
      id: "error.invalid_response",
      message: "The server returned an invalid response.",
    });
  }
  const scoped = scope === undefined ? undefined : scopedErrorMessages[scope];
  const message =
    error.code && scoped && Object.hasOwn(scoped, error.code)
      ? (scoped[error.code] ?? genericFailure)
      : error.code && Object.hasOwn(errorMessages, error.code)
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

/** The one way out a failed page offers, chosen by what went wrong. */
export type RouteRecovery =
  | "retry"
  | "reload"
  | "sign-in"
  | "sign-out"
  | "none";

/** A page that failed to load or render, as the route error views draw it. */
export interface RouteFailure {
  /** What happened; the same line the toast shows. */
  message: MessageDescriptor;
  recovery: RouteRecovery;
  /** Shown under the message as they are; a missing fact is not drawn. */
  facts: {
    request?: string;
    status?: number;
    code?: string;
    requestId?: string;
    exception?: string;
  };
  /** What was sent and received, for the details dialog. */
  exchange?: RequestExchange;
}

const pageCrashed = msg({
  id: "route.error.crashed",
  message: "Something went wrong on this page.",
});

/**
 * Reads a route failure. Only a failure that can clear by itself offers a
 * retry: a lost connection, a server fault or a rate limit. An anonymous
 * session signs in and a disabled account signs out, since every other exit
 * lands on the same refusal; anything else the server refused stays refused.
 * A failure that is not a request is the page itself breaking, which only a
 * fresh load can clear.
 */
export function describeRouteFailure(error: unknown): RouteFailure {
  if (!(error instanceof ApiError)) {
    return {
      message: pageCrashed,
      recovery: "reload",
      facts: { exception: describeException(error) },
    };
  }
  const { requestId, ...message } = describeError(error);
  return {
    message,
    recovery: routeRecovery(error),
    facts: {
      request: error.exchange
        ? `${error.exchange.method} ${error.exchange.path}`
        : undefined,
      status: error.status,
      code: error.code,
      requestId,
    },
    exchange: error.exchange,
  };
}

function routeRecovery(error: ApiError): RouteRecovery {
  if (error.kind === "network" || error.kind === "invalid-response") {
    return "retry";
  }
  const status = error.status;
  if (error.kind === "http" && status !== undefined) {
    if (status >= 500 || status === 429) return "retry";
    if (status === 401 && error.code === "no_session") return "sign-in";
    if (status === 403 && error.code === "account_disabled") return "sign-out";
  }
  return "none";
}

function describeException(error: unknown): string {
  if (error instanceof Error) return `${error.name}: ${error.message}`;
  if (typeof error === "string") return error;
  try {
    return JSON.stringify(error) ?? String(error);
  } catch {
    return String(error);
  }
}
