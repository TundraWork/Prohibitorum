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
  createIdentityProvider: msg({
    id: "success.identity-provider.created",
    message: "Provider created",
  }),
  saveIdentityProvider: msg({
    id: "success.identity-provider.saved",
    message: "Provider saved",
  }),
  setIdentityProviderSecret: msg({
    id: "success.identity-provider.secret-set",
    message: "Secret saved",
  }),
  enableIdentityProvider: msg({
    id: "success.identity-provider.enabled",
    message: "Provider enabled",
  }),
  disableIdentityProvider: msg({
    id: "success.identity-provider.disabled",
    message: "Provider disabled",
  }),
  deleteIdentityProvider: msg({
    id: "success.identity-provider.deleted",
    message: "Provider deleted",
  }),
  updateEntityIcon: msg({
    id: "success.entity-icon.updated",
    message: "Icon updated",
  }),
  removeEntityIcon: msg({
    id: "success.entity-icon.removed",
    message: "Icon removed",
  }),
  createOidcApp: msg({
    id: "success.oidc-app.created",
    message: "Application created",
  }),
  saveOidcApp: msg({
    id: "success.oidc-app.saved",
    message: "Application saved",
  }),
  saveIdentityProjection: msg({
    id: "success.identity-projection.saved",
    message: "Identity projection saved",
  }),
  enableApp: msg({ id: "success.app.enabled", message: "Application enabled" }),
  disableApp: msg({
    id: "success.app.disabled",
    message: "Application disabled",
  }),
  deleteApp: msg({ id: "success.app.deleted", message: "Application deleted" }),
  reingestMetadata: msg({
    id: "success.saml-app.metadata-reingested",
    message: "Metadata re-imported",
  }),
  restrictAppAccess: msg({
    id: "success.app.access-restricted",
    message: "Access restricted to the selected user groups",
  }),
  openAppAccess: msg({
    id: "success.app.access-opened",
    message: "Access opened to every account",
  }),
  saveAppGroups: msg({
    id: "success.app.groups-saved",
    message: "User groups saved",
  }),
  assignAppManager: msg({
    id: "success.app.manager-assigned",
    message: "Manager assigned",
  }),
  removeAppManager: msg({
    id: "success.app.manager-removed",
    message: "Manager removed",
  }),
  validateOperatorSession: msg({
    id: "success.operator-session.validated",
    message: "Session is still valid",
  }),
};
