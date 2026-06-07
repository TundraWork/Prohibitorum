/**
 * English locale — source of truth for all copy.
 * zh added in a later pass (i18n infra is ready; zh strings are deferred).
 *
 * `errors.*` keys map known backend error codes to plain-language messages
 * (per PRODUCT.md: no jargon, no stack traces). Fallback: err.message.
 *
 * Scope descriptions (consent.scopes.*) follow the OpenID Connect spec names.
 */
export default {
  common: {
    loading: 'Loading…',
    error: 'Something went wrong.',
    tryAgain: 'Try again',
    backToLogin: 'Back to sign in',
    signIn: 'Sign in',
    signOut: 'Sign out',
    cancel: 'Cancel',
    continue: 'Continue',
    save: 'Save',
    close: 'Close',
  },

  login: {
    title: 'Sign in',
    passkeyButton: 'Sign in with passkey',
    passkeyHint: "Use your device's built-in authenticator.",
    passwordLabel: 'Password',
    usernameLabel: 'Username',
    passwordSubmit: 'Continue',
    totpLabel: 'One-time code',
    totpHint: 'Enter the 6-digit code from your authenticator app.',
    totpSubmit: 'Verify',
    orDivider: 'or',
    federationHeading: 'Sign in with another account',
    noBootstrap: 'No accounts yet — run `prohibitorum enroll-admin` to get started.',
  },

  consent: {
    title: 'Approve access',
    requestingAccess: '{client} is requesting access to your account.',
    yourAccount: 'You are signed in as {displayName}.',
    scopesHeading: 'This application will be able to:',
    approve: 'Allow',
    deny: 'Deny',
    scopes: {
      openid: 'Confirm your identity',
      profile: 'Read your display name',
      email: 'Read your email address',
      offline_access: 'Stay signed in when you are not using the app',
    },
  },

  logout: {
    title: 'Signed out',
    message: 'You have been successfully signed out.',
    signInAgain: 'Sign in again',
  },

  error: {
    title: 'Something went wrong',
    defaultMessage: 'An unexpected error occurred. Please try again.',
    returnToLogin: 'Return to sign in',
  },

  enroll: {
    title: 'Set up your account',
    titleReset: 'Set up a new passkey',
    titleInvite: 'Accept your invitation',
    usernameLabel: 'Choose a username',
    usernamePlaceholder: 'e.g. alex',
    displayNameLabel: 'Your display name',
    displayNamePlaceholder: 'e.g. Alex Smith',
    registerButton: 'Set up passkey',
    targetAccount: 'Setting up passkey for {username}',
    expired: 'This invitation has expired.',
    invalid: 'This invitation link is invalid or has already been used.',
    federationRedirect: 'Redirecting to your identity provider…',
  },

  /**
   * errors.* — map backend error codes to user-facing messages.
   * Usage: te('errors.'+code) ? t('errors.'+code) : err.message
   *
   * These codes come from the Go handlers (see pkg/server for error bodies).
   */
  errors: {
    // Auth
    unauthorized: 'You need to sign in to continue.',
    forbidden: 'You do not have permission to do that.',
    invalid_credentials: 'Incorrect username or password.',
    totp_required: 'A one-time code is required.',
    invalid_totp: 'That code is incorrect or has expired. Try again.',
    passkey_not_found: 'No passkey found for this account.',
    webauthn_error: 'The passkey ceremony failed. Please try again.',
    session_expired: 'Your session has expired. Please sign in again.',

    // Consent
    invalid_ticket: 'This consent request has expired or is invalid. Please sign in again.',
    consent_denied: 'You denied access.',

    // Enrollment
    token_not_found: 'This invitation link is invalid or has already been used.',
    token_expired: 'This invitation link has expired.',
    token_consumed: 'This invitation link has already been used.',

    // Generic
    server_error: 'A server error occurred. Please try again.',
    not_found: 'The requested page could not be found.',
    rate_limited: 'Too many requests. Please wait a moment and try again.',
    invalid_request: 'The request was invalid.',
  },
} as const
