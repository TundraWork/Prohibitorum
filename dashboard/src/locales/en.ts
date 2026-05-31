export default {
  app: { name: 'Prohibitorum' },
  common: { continue: 'Continue', cancel: 'Cancel', signOut: 'Sign out', copy: 'Copy', copied: 'Copied', confirm: 'Confirm', delete: 'Delete', rename: 'Rename', save: 'Save', create: 'Create', revoke: 'Revoke', disable: 'Disable', enable: 'Enable', loading: 'Loading…', empty: 'Nothing here yet' },
  login: { title: 'Sign in', passkey: 'Sign in with a passkey', password: 'Sign in with password', or: 'or', totp: 'Enter your authenticator code', signInWith: 'Sign in with {name}', username: 'Username', passwordLabel: 'Password', submit: 'Submit', errorFallback: 'Sign-in failed, please try again' },
  consent: { title: 'Authorization request', requests: '"{app}" requests the following permissions:', continueAs: 'Continue as {account}', approve: 'Allow', deny: 'Deny', errorFallback: 'Could not complete the request, please try again' },
  logout: { done: 'You have signed out', returnTo: 'Return to {app}' },
  error: { title: 'Something went wrong', generic: 'An unknown error occurred' },
  scopes: { openid: 'Basic identity', profile: 'Your profile (name, nickname)', email: 'Your email address', offline_access: 'Offline access while you are away', address: 'Your address', phone: 'Your phone number' },
  errors: { no_session: 'Please sign in first', invalid_consent_ticket: 'This authorization request has expired; please start over', bad_credentials: 'Invalid credentials', factor_locked: 'Too many attempts, try again later', server_error: 'Server error, please try again later' },
}
