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
    language: 'Language',
  },

  nav: {
    account: 'Account',
    profile: 'Profile',
    sessions: 'Sessions',
    signOut: 'Sign out',
  },

  profile: {
    title: 'Profile',
    username: 'Username',
    displayName: 'Display name',
    role: 'Role',
  },
  sessions: {
    title: 'Active sessions',
    current: 'This device',
    issued: 'Signed in',
    expires: 'Expires',
    revoke: 'Sign out',
    lastSeen: 'Last seen',
    empty: 'No other active sessions.',
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

  sudo: {
    title: 'Confirm it\'s you',
    prompt: 'For your security, re-verify before making this change.',
    passkeyButton: 'Verify with passkey',
    usePassword: 'Use password and code instead',
    passwordLabel: 'Current password',
    codeLabel: 'One-time code',
    verify: 'Verify',
    cancel: 'Cancel',
    noMethod: 'No verification method is available on this account. Contact an administrator.',
  },

  /**
   * errors.* — map backend error codes to user-facing English messages.
   * Usage: te('errors.'+code) ? t('errors.'+code) : err.message
   *
   * IMPORTANT: the backend AuthError messages are authored in Chinese, so the
   * te()/fallback MUST resolve a key here for every code a user can reach —
   * the err.message fallback is a last resort, not the happy path. Codes below
   * are the real values from pkg/authn/errors.go (verified against the
   * catalogue), plus two client-synthesized codes: `webauthn_error`
   * (useWebauthn) and `server_error` (api.ts / useApi).
   */
  errors: {
    // Session / authorization
    no_session: 'Please sign in to continue.',
    not_admin: 'This action requires administrator access.',
    permission_denied: 'You do not have permission to do that.',
    account_disabled: 'This account has been disabled. Contact an administrator.',
    rate_limited: 'Too many attempts. Please wait a moment and try again.',
    factor_locked: 'Too many failed attempts — this sign-in method is temporarily locked.',
    sudo_method_unavailable: 'That verification method isn\'t available on your account.',

    // Login (passkey + password/TOTP)
    not_bootstrapped:
      'No account has been set up yet. Run `prohibitorum enroll-admin` to create the first administrator.',
    bad_credentials: 'Incorrect username, password, or code.',
    partial_session_invalid: 'Your sign-in session expired. Please start again.',
    ceremony_missing: 'The sign-in attempt expired. Please try again.',
    ceremony_expired: 'The sign-in attempt expired. Please try again.',
    ceremony_state_invalid: 'The sign-in attempt could not be verified. Please try again.',
    login_account_not_found: 'No matching account was found for that passkey.',
    login_failed: 'Sign-in could not be completed. Please try again.',
    login_verification_failed: 'Your passkey could not be verified. Please try again.',
    webauthn_error: 'The passkey step did not complete. Please try again.',

    // Consent
    invalid_consent_ticket:
      'This authorization request has expired. Please start again from the application.',
    bad_request: 'The request was invalid.',

    // Enrollment
    enrollment_expired: 'This invitation link has expired.',
    enrollment_consumed: 'This invitation link has already been used.',
    enrollment_federation_required:
      'This invitation must be completed through your identity provider.',
    invite_required: 'An invitation is required to create an account.',
    username_taken: 'That username is already taken.',
    username_collision: 'That username is already taken.',
    invalid_username:
      'Usernames must be 2–32 lowercase letters, numbers, underscores, or hyphens.',
    invalid_display_name: 'Please enter a valid display name.',
    credential_already_registered: 'That passkey is already registered.',
    registration_failed: 'Passkey setup could not be completed. Please try again.',

    // Federation (upstream IdP)
    upstream_error: 'Your identity provider returned an error. Please try again.',
    email_not_verified: 'Your identity provider has not verified your email address.',
    federation_state_invalid: 'The sign-in attempt expired. Please try again.',
    invalid_return_to: 'The return address was invalid.',

    // Generic / client-synthesized
    server_error: 'Something went wrong on our end. Please try again.',
    not_found: 'The requested page could not be found.',
  },
} as const
