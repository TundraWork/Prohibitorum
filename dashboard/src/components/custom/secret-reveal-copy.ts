import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";

/**
 * Wording for a one-time secret disclosure. Kept as message descriptors so a
 * caller can supply its own catalog ids while `SecretReveal` keeps one
 * implementation and one leaving guard.
 */
export interface SecretRevealCopy {
  title: MessageDescriptor;
  once: MessageDescriptor;
  label: MessageDescriptor;
  copy: MessageDescriptor;
  download: MessageDescriptor;
  copyFailed: MessageDescriptor;
  downloadFailed: MessageDescriptor;
  saved: MessageDescriptor;
  continueLabel: MessageDescriptor;
  leave: MessageDescriptor;
}

export const recoveryCodesCopy: SecretRevealCopy = {
  title: msg({
    id: "login.codes.title",
    message: "Save your new recovery codes",
  }),
  once: msg({
    id: "login.codes.once",
    message:
      "These codes are shown only once. Keep them somewhere safe before continuing. Your old recovery codes no longer work.",
  }),
  label: msg({
    id: "login.codes.label",
    message: "New recovery codes",
  }),
  copy: msg({ id: "login.codes.copy", message: "Copy codes" }),
  download: msg({ id: "login.codes.download", message: "Download codes" }),
  copyFailed: msg({
    id: "login.codes.copy_failed",
    message:
      "Could not copy the codes. Select the codes above and copy them manually, or download them.",
  }),
  downloadFailed: msg({
    id: "login.codes.download_failed",
    message: "Could not download the codes. Copy them or save them manually.",
  }),
  saved: msg({
    id: "login.codes.saved",
    message: "I have saved my recovery codes",
  }),
  continueLabel: msg({ id: "login.codes.continue", message: "Continue" }),
  leave: msg({
    id: "login.codes.leave",
    message:
      "Leave without saving? These recovery codes cannot be shown again.",
  }),
};

export const invitationLinkCopy: SecretRevealCopy = {
  title: msg({
    id: "admin.invitations.reveal.title",
    message: "Copy the registration link",
  }),
  once: msg({
    id: "admin.invitations.reveal.once",
    message:
      "This link is shown only once and it signs the holder in as the invited account. Send it to the person yourself, and only over a channel you trust.",
  }),
  label: msg({
    id: "admin.invitations.reveal.label",
    message: "Registration link",
  }),
  copy: msg({
    id: "admin.invitations.reveal.copy",
    message: "Copy link",
  }),
  download: msg({
    id: "admin.invitations.reveal.download",
    message: "Download link",
  }),
  copyFailed: msg({
    id: "admin.invitations.reveal.copy_failed",
    message:
      "Could not copy the link. Select it above and copy it manually, or download it.",
  }),
  downloadFailed: msg({
    id: "admin.invitations.reveal.download_failed",
    message: "Could not download the link. Copy it or save it manually.",
  }),
  saved: msg({
    id: "admin.invitations.reveal.saved",
    message: "I have saved the registration link",
  }),
  continueLabel: msg({
    id: "admin.invitations.reveal.continue",
    message: "Done",
  }),
  leave: msg({
    id: "admin.invitations.reveal.leave",
    message: "Leave without saving? This link cannot be shown again.",
  }),
};

export const accessTokenCopy: SecretRevealCopy = {
  title: msg({
    id: "security.tokens.reveal.title",
    message: "Copy your new access token",
  }),
  once: msg({
    id: "security.tokens.reveal.once",
    message:
      "This token is shown only once. Store it somewhere safe before continuing. The list will only ever show its hint.",
  }),
  label: msg({
    id: "security.tokens.reveal.label",
    message: "New access token",
  }),
  copy: msg({ id: "security.tokens.reveal.copy", message: "Copy token" }),
  download: msg({
    id: "security.tokens.reveal.download",
    message: "Download token",
  }),
  copyFailed: msg({
    id: "security.tokens.reveal.copy_failed",
    message:
      "Could not copy the token. Select the token above and copy it manually, or download it.",
  }),
  downloadFailed: msg({
    id: "security.tokens.reveal.download_failed",
    message: "Could not download the token. Copy it or save it manually.",
  }),
  saved: msg({
    id: "security.tokens.reveal.saved",
    message: "I have saved my access token",
  }),
  continueLabel: msg({
    id: "security.tokens.reveal.continue",
    message: "Done",
  }),
  leave: msg({
    id: "security.tokens.reveal.leave",
    message: "Leave without saving? This token cannot be shown again.",
  }),
};
