import { msg } from "@lingui/core/macro";

/**
 * Why the console is asking for a fresh verification, in the user's terms.
 *
 * Each sudo-guarded write carries the one that matches it, so the prompt says
 * what is about to happen rather than showing the same sentence every time. They
 * live together here because the dialog renders them and the mutation factories
 * supply them, and neither should own the wording alone.
 */
export const sudoReason = {
  addPasskey: msg({
    id: "sudo.reason.add-passkey",
    message: "Confirm it is you to add a passkey to this account.",
  }),
  deletePasskey: msg({
    id: "sudo.reason.delete-passkey",
    message: "Confirm it is you to remove this passkey.",
  }),
  changePassword: msg({
    id: "sudo.reason.change-password",
    message: "Confirm it is you to change your password.",
  }),
  replaceAuthenticator: msg({
    id: "sudo.reason.replace-authenticator",
    message: "Confirm it is you to replace your authenticator.",
  }),
  setPasswordAndAuthenticator: msg({
    id: "sudo.reason.set-password-totp",
    message:
      "Confirm it is you to set up a password and authenticator on this account.",
  }),
  regenerateRecoveryCodes: msg({
    id: "sudo.reason.recovery-codes",
    message: "Confirm it is you to replace your recovery codes.",
  }),
  revokePasswordTotp: msg({
    id: "sudo.reason.revoke-password-totp",
    message: "Confirm it is you to turn off your password and authenticator.",
  }),
  unlinkIdentity: msg({
    id: "sudo.reason.unlink-identity",
    message: "Confirm it is you to unlink this identity.",
  }),
  linkIdentity: msg({
    id: "sudo.reason.link-identity",
    message: "Confirm it is you to link this identity.",
  }),
  approveDevice: msg({
    id: "sudo.reason.approve-device",
    message: "Confirm it is you to approve this device.",
  }),
  createToken: msg({
    id: "sudo.reason.create-token",
    message: "Confirm it is you to create this access token.",
  }),
  revokeToken: msg({
    id: "sudo.reason.revoke-token",
    message: "Confirm it is you to revoke this access token.",
  }),

  /* Management writes. Only the ones that remove something for good or hand out
     a credential ask for sudo: deleting an account, a new registration link, a
     change of role, the signing keys, a new client secret, and deleting an
     application or identity provider. */
  changeAccountRole: msg({
    id: "sudo.reason.change-account-role",
    message: "Confirm it is you to change this account's role.",
  }),
  deleteAccount: msg({
    id: "sudo.reason.delete-account",
    message: "Confirm it is you to delete this account. This cannot be undone.",
  }),
  reissueEnrollment: msg({
    id: "sudo.reason.reissue-enrollment",
    message: "Confirm it is you to issue a new registration link.",
  }),
  generateSigningKey: msg({
    id: "sudo.reason.generate-signing-key",
    message: "Confirm it is you to generate a signing key.",
  }),
  activateSigningKey: msg({
    id: "sudo.reason.activate-signing-key",
    message: "Confirm it is you to start signing with this key.",
  }),
  retireSigningKey: msg({
    id: "sudo.reason.retire-signing-key",
    message: "Confirm it is you to retire this signing key.",
  }),
  deleteIdentityProvider: msg({
    id: "sudo.reason.delete-identity-provider",
    message:
      "Confirm it is you to delete this provider and every identity linked through it.",
  }),
  rotateClientSecret: msg({
    id: "sudo.reason.rotate-client-secret",
    message:
      "Confirm it is you to replace this client secret. The old one stops working immediately.",
  }),
  deleteApplication: msg({
    id: "sudo.reason.delete-application",
    message: "Confirm it is you to delete this application.",
  }),
} as const;
