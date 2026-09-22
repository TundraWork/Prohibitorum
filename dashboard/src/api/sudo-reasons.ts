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

  /* Management writes. These act on someone else's account or on the instance's
     shared policy, so each names the target rather than saying "this". */
  updateAccount: msg({
    id: "sudo.reason.update-account",
    message: "Confirm it is you to change this account.",
  }),
  setAccountDisabled: msg({
    id: "sudo.reason.set-account-disabled",
    message: "Confirm it is you to change whether this account can sign in.",
  }),
  deleteAccount: msg({
    id: "sudo.reason.delete-account",
    message: "Confirm it is you to delete this account. This cannot be undone.",
  }),
  reissueEnrollment: msg({
    id: "sudo.reason.reissue-enrollment",
    message: "Confirm it is you to issue a new registration link.",
  }),
  revokeAccountCredential: msg({
    id: "sudo.reason.revoke-account-credential",
    message: "Confirm it is you to revoke this passkey.",
  }),
  revokeAccountToken: msg({
    id: "sudo.reason.revoke-account-token",
    message: "Confirm it is you to revoke this access token.",
  }),
  revokeAccountSessions: msg({
    id: "sudo.reason.revoke-account-sessions",
    message: "Confirm it is you to sign this account out everywhere.",
  }),
  createGroup: msg({
    id: "sudo.reason.create-group",
    message: "Confirm it is you to create this group.",
  }),
  updateGroup: msg({
    id: "sudo.reason.update-group",
    message: "Confirm it is you to change this group.",
  }),
  deleteGroup: msg({
    id: "sudo.reason.delete-group",
    message: "Confirm it is you to delete this group. This cannot be undone.",
  }),
  groupDecision: msg({
    id: "sudo.reason.group-decision",
    message: "Confirm it is you to change who this group allows.",
  }),
  createInvitation: msg({
    id: "sudo.reason.create-invitation",
    message: "Confirm it is you to create this invitation.",
  }),
  revokeInvitation: msg({
    id: "sudo.reason.revoke-invitation",
    message: "Confirm it is you to revoke this invitation.",
  }),
} as const;
