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
  pat_invalid: msg({
    id: "error.pat_invalid",
    message:
      "This access token is not valid. It may have expired or been revoked.",
  }),
  pat_api_not_allowed: msg({
    id: "error.pat_api_not_allowed",
    message:
      "This access token can only be used for applications, not for the account.",
  }),
  pat_browser_session_required: msg({
    id: "error.pat_browser_session_required",
    message: "Sign in with a browser to do this. An access token cannot.",
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
  login_credential_unknown: msg({
    id: "error.login_credential_unknown",
    message:
      "This passkey is no longer registered here. Use another passkey or your password.",
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
  invalid_consent_ticket: msg({
    id: "error.invalid_consent_ticket",
    message:
      "This authorization request has expired or was already used. Go back to the app and sign in again.",
  }),
  invalid_return_to: msg({
    id: "error.invalid_return_to",
    message: "Invalid return address.",
  }),

  /* Enrollment and the public federation pages. */
  enrollment_consumed: msg({
    id: "error.enrollment_consumed",
    message:
      "This link was already used or has expired. Ask your administrator for a new one.",
  }),
  enrollment_expired: msg({
    id: "error.enrollment_expired",
    message:
      "This link was already used or has expired. Ask your administrator for a new one.",
  }),
  enrollment_method_not_allowed: msg({
    id: "error.enrollment_method_not_allowed",
    message: "This link can't be used to set up that sign-in method.",
  }),
  invalid_display_name: msg({
    id: "error.invalid_display_name",
    message:
      "Enter a display name of 1–128 characters, without control characters.",
  }),
  credential_already_registered: msg({
    id: "error.credential_already_registered",
    message: "This passkey is already registered.",
  }),
  registration_failed: msg({
    id: "error.registration_failed",
    message: "The passkey couldn't be created.",
  }),
  federation_action_invalid: msg({
    id: "error.federation_action_invalid",
    message: "This page is no longer valid.",
  }),
  username_collision: msg({
    id: "error.username_collision",
    message:
      "An account with this username already exists, so one can't be created automatically. Contact your administrator.",
  }),
  vrchat_identity_invalid: msg({
    id: "error.vrchat_identity_invalid",
    message: "Enter a VRChat profile address or a user ID beginning with usr_.",
  }),
  vrchat_proof_missing: msg({
    id: "error.vrchat_proof_missing",
    message:
      "The verification link isn't in your bio yet. Save your profile, wait a moment, then verify.",
  }),
  local_username_required: msg({
    id: "error.local_username_required",
    message: "Choose a username for your new account.",
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
  // A sign-in through a provider keeps its state for a short while in the
  // browser's cookie. This is not a signed-out session: only `no_session`
  // means that. A connection test reuses the code, in the `diagnostic` scope.
  federation_state_invalid: msg({
    id: "error.federation_state_invalid.sign_in",
    message: "This sign-in has expired. Sign in again.",
  }),
};

/**
 * Where a request was made, for the few codes whose meaning depends on it.
 * A mutation names its scope as `meta.errorScope`, and the query client hands
 * it on, so the toast reads the scoped wording before the general one.
 *
 * `federation` is a sign-in or a link that went through an upstream provider:
 * its wording names the provider, which the failure carries as
 * `details.federationName`. `setup-signin` is a public page that adds a
 * sign-in of the account's own right after a first one — `/setup-signin`, and
 * `/pair` once the device is signed in; `diagnostic` is an administrator's connection test
 * of a provider.
 */
export type ErrorScope =
  | "signing-key"
  | "federation"
  | "setup-signin"
  | "diagnostic";

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
  // The page that adds a sign-in after a first federated one does not ask
  // who is signed in again; once its window has passed, the step is left for
  // the Security page.
  "setup-signin": {
    sudo_required: msg({
      id: "error.setup-signin.sudo_required",
      message: "This step has timed out. Add it later from Security.",
    }),
  },
  // A connection test is single-use and short-lived.
  diagnostic: {
    federation_state_invalid: msg({
      id: "error.federation_state_invalid",
      message: "This test is no longer valid. Start a new one.",
    }),
  },
  // Every one of these names the provider. Without a name the sentence would
  // have to say "the provider", which tells the reader nothing they can act
  // on, so a failure without one falls back to `signInFailure`.
  federation: {
    enrollment_federation_required: msg({
      id: "error.federation.enrollment_federation_required",
      message:
        "This invitation needs the account to be created through {provider}.",
    }),
    invite_required: msg({
      id: "error.federation.invite_required",
      message: "Creating an account through {provider} requires an invitation.",
    }),
    link_required: msg({
      id: "error.federation.link_required",
      message: "Sign in first, then link {provider} from Security.",
    }),
    federation_identity_conflict: msg({
      id: "error.federation.federation_identity_conflict",
      message: "This {provider} identity is already linked to another account.",
    }),
    federation_invite_provider_mismatch: msg({
      id: "error.federation.federation_invite_provider_mismatch",
      message: "This invitation can't be accepted with a {provider} account.",
    }),
    email_not_verified: msg({
      id: "error.federation.email_not_verified",
      message: "Verify your email address with {provider} first.",
    }),
    upstream_error: msg({
      id: "error.federation.upstream_error",
      message: "{provider} refused this sign-in.",
    }),
    upstream_rate_limited: msg({
      id: "error.federation.upstream_rate_limited",
      message: "{provider} is asking us to slow down. Wait a moment.",
    }),
    upstream_temporarily_unavailable: msg({
      id: "error.federation.upstream_temporarily_unavailable",
      message: "Could not reach {provider}.",
    }),
    provider_not_ready: msg({
      id: "error.federation.provider_not_ready",
      message: "{provider} isn't set up yet.",
    }),
  },
};

/** `upstream_rate_limited` when the response says how long to wait. */
const upstreamRateLimitedFor = msg({
  id: "error.federation.upstream_rate_limited.seconds",
  message:
    "{provider} is asking us to slow down. Wait {seconds, plural, one {# second} other {# seconds}}.",
});

/** A sign-in that failed with nothing more specific to say. */
const signInFailure = msg({
  id: "error.sign_in_failed",
  message: "Something went wrong while signing in.",
});

/**
 * The federation wording for a code, with the provider's name filled in, or
 * `signInFailure` when the name is missing. `undefined` for a code the
 * federation table does not hold. `retryAfter` is the response's
 * `Retry-After` in seconds, which a rate limit reports when it has one.
 */
function federationMessage(
  code: string,
  name: unknown,
  retryAfter?: number,
): MessageDescriptor | undefined {
  const table = scopedErrorMessages.federation;
  if (!Object.hasOwn(table, code)) return undefined;
  const message = table[code];
  if (message === undefined) return undefined;
  if (typeof name !== "string" || name === "") return signInFailure;
  if (code === "upstream_rate_limited" && retryAfter !== undefined) {
    return {
      ...upstreamRateLimitedFor,
      values: { provider: name, seconds: retryAfter },
    };
  }
  return { ...message, values: { provider: name } };
}

/**
 * The whole seconds a failed response asked to wait, from its `Retry-After`.
 * The date form is not read: the server sends seconds.
 */
function retryAfterSeconds(error: ApiError): number | undefined {
  const header = error.exchange?.response?.headers.find(
    ([name]) => name.toLowerCase() === "retry-after",
  )?.[1];
  if (header === undefined || !/^\d+$/.test(header.trim())) return undefined;
  const seconds = Number(header.trim());
  return seconds > 0 && Number.isSafeInteger(seconds) ? seconds : undefined;
}

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
  const federation =
    scope === "federation" && error.code
      ? federationMessage(
          error.code,
          error.details?.federationName,
          retryAfterSeconds(error),
        )
      : undefined;
  const message =
    federation ??
    (error.code && scoped && Object.hasOwn(scoped, error.code)
      ? (scoped[error.code] ?? genericFailure)
      : error.code && Object.hasOwn(errorMessages, error.code)
        ? (errorMessages[error.code] ?? genericFailure)
        : genericFailure);
  return {
    ...message,
    requestId:
      error.requestId && /^[A-Za-z0-9_-]{1,128}$/.test(error.requestId)
        ? error.requestId
        : undefined,
  };
}

/**
 * Codes that only ever reach the browser as a redirect to `/error`, from the
 * OIDC and SAML endpoints, rather than in a response the console reads.
 */
const landingMessages: Readonly<Record<string, MessageDescriptor>> = {
  invalid_request: msg({
    id: "error.landing.invalid_request",
    message: "The app sent a request that can't be used.",
  }),
  saml_request_invalid: msg({
    id: "error.landing.saml_request_invalid",
    message: "The app sent a sign-in request that can't be used.",
  }),
  saml_sp_unknown: msg({
    id: "error.landing.saml_sp_unknown",
    message: "This app isn't registered with {instance}.",
  }),
  saml_replayed: msg({
    id: "error.landing.saml_replayed",
    message:
      "This sign-in request was already used. Go back to the app and sign in again.",
  }),
  saml_idp_init_disabled: msg({
    id: "error.landing.saml_idp_init_disabled",
    message: "This app can't be opened directly from {instance}.",
  }),
};

const appAccessDenied = msg({
  id: "error.landing.app_access_denied",
  message: "An administrator hasn't given your account access to this app.",
});

/**
 * What the `/error` page says about a flow the server stopped. Its inputs are
 * the page's search values, which anyone can write, so only a code one of the
 * tables knows is described and everything else reads as `signInFailure`.
 *
 * The order: a denied application, then the codes only a redirect carries,
 * then the federation codes (which need the provider's name), then the
 * general table.
 */
export function describeErrorLanding({
  code,
  reason,
  federationName,
  instance,
}: {
  code?: string;
  reason?: string;
  federationName?: string;
  instance: string;
}): MessageDescriptor {
  if (reason === "app_access_denied") return appAccessDenied;
  if (code === undefined || code === "") return signInFailure;
  if (Object.hasOwn(landingMessages, code)) {
    const message = landingMessages[code];
    if (message) return { ...message, values: { instance } };
  }
  const federation = federationMessage(code, federationName);
  if (federation) return federation;
  if (Object.hasOwn(errorMessages, code)) {
    const message = errorMessages[code];
    if (message) return message;
  }
  return signInFailure;
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
