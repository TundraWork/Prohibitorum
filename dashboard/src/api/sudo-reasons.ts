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
    message: "Verify your identity to add a passkey to this account.",
  }),
  deletePasskey: msg({
    id: "sudo.reason.delete-passkey",
    message: "Verify your identity to remove this passkey.",
  }),
  changePassword: msg({
    id: "sudo.reason.change-password",
    message: "Verify your identity to change your password.",
  }),
  replaceAuthenticator: msg({
    id: "sudo.reason.replace-authenticator",
    message: "Verify your identity to replace your authenticator.",
  }),
  setPasswordAndAuthenticator: msg({
    id: "sudo.reason.set-password-totp",
    message:
      "Verify your identity to set up a password and authenticator on this account.",
  }),
  regenerateRecoveryCodes: msg({
    id: "sudo.reason.recovery-codes",
    message: "Verify your identity to replace your recovery codes.",
  }),
  revokePasswordTotp: msg({
    id: "sudo.reason.revoke-password-totp",
    message:
      "Verify your identity to turn off your password and authenticator.",
  }),
  unlinkIdentity: msg({
    id: "sudo.reason.unlink-identity",
    message: "Verify your identity to unlink this identity.",
  }),
  linkIdentity: msg({
    id: "sudo.reason.link-identity",
    message: "Verify your identity to link this identity.",
  }),
  approveDevice: msg({
    id: "sudo.reason.approve-device",
    message: "Verify your identity to approve this device.",
  }),
  createToken: msg({
    id: "sudo.reason.create-token",
    message: "Verify your identity to create this access token.",
  }),
  revokeToken: msg({
    id: "sudo.reason.revoke-token",
    message: "Verify your identity to revoke this access token.",
  }),

  /* Management writes. Only the ones that remove something for good or hand out
     a credential ask for sudo: deleting an account, a new registration link, a
     change of role, the signing keys, a new client secret, and deleting an
     application or identity provider. */
  changeAccountRole: msg({
    id: "sudo.reason.change-account-role",
    message: "Verify your identity to change this account's role.",
  }),
  deleteAccount: msg({
    id: "sudo.reason.delete-account",
    message:
      "Verify your identity to delete this account. This cannot be undone.",
  }),
  reissueEnrollment: msg({
    id: "sudo.reason.reissue-enrollment",
    message: "Verify your identity to issue a new registration link.",
  }),
  generateSigningKey: msg({
    id: "sudo.reason.generate-signing-key",
    message: "Verify your identity to generate a signing key.",
  }),
  activateSigningKey: msg({
    id: "sudo.reason.activate-signing-key",
    message: "Verify your identity to start signing with this key.",
  }),
  retireSigningKey: msg({
    id: "sudo.reason.retire-signing-key",
    message: "Verify your identity to retire this signing key.",
  }),
  deleteIdentityProvider: msg({
    id: "sudo.reason.delete-identity-provider",
    message:
      "Verify your identity to delete this provider. Every identity linked through it is deleted too.",
  }),
  rotateClientSecret: msg({
    id: "sudo.reason.rotate-client-secret",
    message:
      "Verify your identity to replace this client secret. The old secret stops working immediately.",
  }),
  deleteApplication: msg({
    id: "sudo.reason.delete-application",
    message: "Verify your identity to delete this application.",
  }),
} as const;
