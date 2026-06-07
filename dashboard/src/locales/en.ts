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
    copy: 'Copy',
    copied: 'Copied',
  },

  nav: {
    account: 'Account',
    profile: 'Profile',
    security: 'Security',
    sessions: 'Sessions',
    connected: 'Connected',
    devices: 'Devices',
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

  connected: {
    title: 'Connected accounts',
    help: 'Sign in using accounts from other identity providers.',
    empty: 'You haven’t connected any accounts yet.',
    linked: 'Linked',
    unlink: 'Disconnect',
    unlinkConfirmTitle: 'Disconnect this account?',
    unlinkConfirmBody: 'You will no longer be able to sign in using this provider.',
    linkHeading: 'Connect an account',
    linkHelp: 'Add another identity provider you can sign in with.',
    alreadyLinked: 'Connected',
    noProviders: 'No identity providers are available to connect.',
  },

  devices: {
    title: 'Devices',
    help: 'Approve a new device that’s trying to sign in to your account.',
    codeLabel: 'Pairing code',
    codePlaceholder: 'XXXX-XXXX',
    lookup: 'Look up',
    confirmTitle: 'Approve this device?',
    requestedFrom: 'Requested from',
    ipAddress: 'IP address',
    started: 'Started',
    expires: 'Expires',
    approve: 'Approve device',
    cancel: 'Cancel',
    approved: 'Device approved. It will be signed in shortly.',
    alreadyBound: 'You’ve already approved this device.',
  },

  pair: {
    title: 'Pair this device',
    intro: 'On a device where you’re already signed in, open Devices and enter this code.',
    waiting: 'Waiting for approval…',
    expiresIn: 'Expires in {seconds}s',
    expired: 'This code has expired.',
    regenerate: 'Generate a new code',
    success: 'This device is now signed in.',
    addPasskey: 'Add a passkey to this device',
    addPasskeyHelp: 'So you can sign in directly next time, without pairing.',
    skip: 'Continue to dashboard',
  },

  login: {
    title: 'Sign in',
    passkeyButton: 'Sign in with passkey',
    passkeyHint: 'Use your device’s built-in authenticator.',
    passwordLabel: 'Password',
    usernameLabel: 'Username',
    passwordSubmit: 'Continue',
    totpLabel: 'One-time code',
    totpHint: 'Enter the 6-digit code from your authenticator app.',
    totpSubmit: 'Verify',
    orDivider: 'or',
    federationHeading: 'Sign in with another account',
    noBootstrap: 'No accounts yet — run `prohibitorum enroll-admin` to get started.',
    pairDevice: 'New device? Pair it',
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

  confirm: {
    cancel: 'Cancel',
  },

  recoveryCodes: {
    heading: 'Save your recovery codes',
    intro: 'Each code can be used once if you lose access to your authenticator app.',
    regeneratedWarning: 'Your previous recovery codes no longer work.',
    storage: 'Store them in a safe place — a password manager or printed copy. Don’t keep them next to your password, and don’t rely on a single screenshot.',
    copyAll: 'Copy all',
    download: 'Download .txt',
    savedConfirm: 'I’ve saved my recovery codes',
    done: 'Done',
  },

  sudo: {
    title: 'Confirm it’s you',
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
    sudo_method_unavailable: 'That verification method isn’t available on your account.',

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

    // Passkey management
    last_passkey: 'You can’t remove your only passkey. Add another first.',

    // Connected accounts
    last_sign_in_method: 'You can’t remove your last sign-in method. Add another first.',
    credential_not_found: 'That connection no longer exists.',

    // Device pairing
    pairing_not_found: 'That code is invalid, used, or expired.',
    pairing_expired: 'That code has expired. Ask the device to generate a new one.',
    pairing_not_approved: 'This device hasn’t been approved yet.',
    pairing_state: 'That pairing can’t be changed right now.',
  },

  security: {
    title: 'Security',
    passkeys: {
      title: 'Passkeys',
      add: 'Add passkey',
      rename: 'Rename',
      save: 'Save',
      remove: 'Remove',
      removeTitle: 'Remove this passkey?',
      removeBody: 'You’ll sign in with your other passkeys. This passkey will stop working on its device.',
      created: 'Added',
      lastUsed: 'Last used',
      synced: 'Synced',
      deviceBound: 'This device',
      defaultName: 'Passkey',
    },
    password: {
      title: 'Password',
      help: 'Set a password you can use with a one-time code if a passkey isn’t available.',
      newLabel: 'New password',
      confirmLabel: 'Confirm password',
      submit: 'Save password',
      tooShort: 'Use at least 8 characters.',
      mismatch: 'Passwords don’t match.',
      saved: 'Password updated.',
    },
    totp: {
      title: 'Authenticator app',
      help: 'Use a TOTP app (Google Authenticator, 1Password, …) for one-time codes.',
      setup: 'Set up authenticator',
      scan: 'Scan this QR code with your authenticator app',
      secretLabel: 'Or enter this key manually',
      codeLabel: 'Enter the 6-digit code to confirm',
      verify: 'Verify & enable',
      enabled: 'Authenticator enabled.',
    },
    recovery: {
      title: 'Recovery codes',
      help: 'One-time codes to sign in if you lose your authenticator. Requires an authenticator app.',
      regenerate: 'Regenerate codes',
      needTotp: 'Set up an authenticator app first.',
    },
    revoke: {
      title: 'Password & authenticator',
      help: 'Remove your password, authenticator app, and recovery codes. You’ll sign in with passkeys only.',
      button: 'Remove password & authenticator',
      confirmTitle: 'Remove password & authenticator?',
      confirmBody: 'This deletes your password, authenticator app, and recovery codes. You’ll be able to sign in with your passkeys only. You can set them up again later.',
      done: 'Removed. You now sign in with passkeys only.',
    },
  },
} as const
