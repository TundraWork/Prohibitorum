import { msg } from "@lingui/core/macro";

/**
 * What the console says once a write has gone through.
 *
 * Each mutation that changes something without showing a result of its own
 * carries the line that matches it as `meta.success`, and the query client
 * turns it into a toast. A write that answers with something to read — new
 * recovery codes, a token, a registration link — says nothing here: what it
 * reveals is the confirmation.
 */
export const successMessage = {
  renamePasskey: msg({
    id: "success.passkey.renamed",
    message: "Passkey renamed",
  }),
  addPasskey: msg({ id: "success.passkey.added", message: "Passkey added" }),
  removePasskey: msg({
    id: "success.passkey.removed",
    message: "Passkey removed",
  }),
  saveDisplayName: msg({
    id: "success.display-name.saved",
    message: "Display name saved",
  }),
  updateAvatar: msg({
    id: "success.avatar.updated",
    message: "Avatar updated",
  }),
  removeAvatarUpload: msg({
    id: "success.avatar.upload-removed",
    message: "Uploaded picture removed",
  }),
  removeAppAccess: msg({
    id: "success.app.access-removed",
    message: "Access removed",
  }),
  changePassword: msg({
    id: "success.password.changed",
    message: "Password changed",
  }),
  revokePasswordTotp: msg({
    id: "success.password-totp.revoked",
    message: "Password and authenticator turned off",
  }),
  endSession: msg({ id: "success.session.ended", message: "Session ended" }),
  endAllSessions: msg({
    id: "success.sessions.ended",
    message: "Signed out everywhere",
  }),
  unlinkIdentity: msg({
    id: "success.identity.unlinked",
    message: "Identity unlinked",
  }),
  revokeToken: msg({
    id: "success.token.revoked",
    message: "Access token revoked",
  }),
  approveDevice: msg({
    id: "success.device.approved",
    message: "Device approved",
  }),
  declineDevice: msg({
    id: "success.device.declined",
    message: "Pairing declined",
  }),
  saveAccount: msg({ id: "success.account.saved", message: "Account saved" }),
  enableAccount: msg({
    id: "success.account.enabled",
    message: "Account enabled",
  }),
  disableAccount: msg({
    id: "success.account.disabled",
    message: "Account disabled",
  }),
  deleteAccount: msg({
    id: "success.account.deleted",
    message: "Account deleted",
  }),
  createGroup: msg({
    id: "success.group.created",
    message: "User group created",
  }),
  saveGroup: msg({ id: "success.group.saved", message: "User group saved" }),
  deleteGroup: msg({
    id: "success.group.deleted",
    message: "User group deleted",
  }),
  revokeInvitation: msg({
    id: "success.invitation.revoked",
    message: "Invitation revoked",
  }),
  saveInstanceName: msg({
    id: "success.instance-name.saved",
    message: "Instance name saved",
  }),
  maintenanceOn: msg({
    id: "success.maintenance.on",
    message: "Maintenance mode is on",
  }),
  maintenanceSaved: msg({
    id: "success.maintenance.saved",
    message: "Maintenance settings saved",
  }),
  updateInstanceIcon: msg({
    id: "success.instance-icon.updated",
    message: "Icon updated",
  }),
  removeInstanceIcon: msg({
    id: "success.instance-icon.removed",
    message: "Icon removed",
  }),
  updateSignInBackground: msg({
    id: "success.sign-in-background.updated",
    message: "Sign-in background updated",
  }),
  removeSignInBackground: msg({
    id: "success.sign-in-background.removed",
    message: "Sign-in background removed",
  }),
  saveClientIp: msg({
    id: "success.client-ip.saved",
    message: "Client IP settings saved",
  }),
  generateSigningKey: msg({
    id: "success.signing-key.generated",
    message: "Signing key generated",
  }),
  activateSigningKey: msg({
    id: "success.signing-key.activated",
    message: "Signing key activated",
  }),
  retireSigningKey: msg({
    id: "success.signing-key.retired",
    message: "Signing key is being retired",
  }),
};
