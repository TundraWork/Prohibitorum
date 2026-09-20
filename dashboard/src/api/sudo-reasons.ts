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
} as const;
