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
  /** How the secret is used, for one whose use is not obvious from its name. */
  usage?: MessageDescriptor;
  copy: MessageDescriptor;
  copyFailed: MessageDescriptor;
  /**
   * Offered only for a secret kept offline (recovery codes); the others are
   * pasted somewhere at once, and a downloaded file would be one more copy.
   */
  download?: MessageDescriptor;
  downloadFailed?: MessageDescriptor;
  continueLabel: MessageDescriptor;
  /**
   * What is lost by leaving before the secret was copied or downloaded: the
   * body of the confirmation that leaving asks for.
   */
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
      "These codes are shown only once. Save them somewhere safe before you continue. Your old recovery codes no longer work.",
  }),
  label: msg({
    id: "login.codes.label",
    message: "New recovery codes",
  }),
  copy: msg({ id: "login.codes.copy", message: "Copy codes" }),
  copyFailed: msg({
    id: "login.codes.copy_failed",
    message:
      "Could not copy the codes. Select the codes above and copy them manually, or download them.",
  }),
  download: msg({ id: "login.codes.download", message: "Download codes" }),
  downloadFailed: msg({
    id: "login.codes.download_failed",
    message: "Could not download the codes. Copy them or save them manually.",
  }),
  continueLabel: msg({ id: "login.codes.continue", message: "Continue" }),
  leave: msg({
    id: "login.codes.leave",
    message: "These recovery codes cannot be shown again after this is closed.",
  }),
};

/**
 * The same reveal for the first codes an account is given, when there are no
 * earlier codes for the wording to say were replaced.
 */
export const firstRecoveryCodesCopy: SecretRevealCopy = {
  ...recoveryCodesCopy,
  title: msg({
    id: "security.codes.first.title",
    message: "Save your recovery codes",
  }),
  once: msg({
    id: "security.codes.first.once",
    message:
      "When you cannot use your authenticator, sign in with a recovery code. Each code works once. These codes are shown only once; store them somewhere safe before continuing.",
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
      "This link is shown only once. Anyone holding it can sign in as the invited account, so send it to the invitee only over a trusted channel.",
  }),
  label: msg({
    id: "admin.invitations.reveal.label",
    message: "Registration link",
  }),
  copy: msg({
    id: "admin.invitations.reveal.copy",
    message: "Copy link",
  }),
  copyFailed: msg({
    id: "admin.invitations.reveal.copy_failed",
    message: "Could not copy the link. Select it above and copy it manually.",
  }),
  continueLabel: msg({
    id: "admin.invitations.reveal.continue",
    message: "Done",
  }),
  leave: msg({
    id: "admin.invitations.reveal.leave",
    message: "This link cannot be shown again after this is closed.",
  }),
};

/**
 * The invitation link's reveal that also says when the link stops working.
 * `expiry` is the date already written in the reader's language.
 */
export function invitationLinkCopyWithExpiry(expiry: string): SecretRevealCopy {
  return {
    ...invitationLinkCopy,
    usage: msg({
      id: "admin.user.enrollment.expires",
      message: `The link expires on ${expiry}.`,
    }),
  };
}

/**
 * The reveal for a registration link issued to an account that already exists:
 * the holder sets up how that account signs in, rather than being invited, and
 * the dialog says when the link stops working.
 */
export function registrationLinkCopyWithExpiry(
  expiry: string,
): SecretRevealCopy {
  return {
    ...invitationLinkCopyWithExpiry(expiry),
    once: msg({
      id: "admin.user.enrollment.reveal.once",
      message:
        "This link is shown only once. Anyone holding it can set up how this account signs in, so send it to the account holder only over a trusted channel.",
    }),
  };
}

export const accessTokenCopy: SecretRevealCopy = {
  title: msg({
    id: "security.tokens.reveal.title",
    message: "Copy your new access token",
  }),
  once: msg({
    id: "security.tokens.reveal.once",
    message:
      "This token is shown only once. Store it somewhere safe before continuing. The token list shows only prohibitorum_pat_… and the token's last 4 characters.",
  }),
  label: msg({
    id: "security.tokens.reveal.label",
    message: "New access token",
  }),
  usage: msg({
    id: "security.tokens.reveal.usage",
    message: "Send the token in the X-Prohibitorum-PAT request header.",
  }),
  copy: msg({ id: "security.tokens.reveal.copy", message: "Copy token" }),
  copyFailed: msg({
    id: "security.tokens.reveal.copy_failed",
    message:
      "Could not copy the token. Select the token above and copy it manually.",
  }),
  continueLabel: msg({
    id: "security.tokens.reveal.continue",
    message: "Done",
  }),
  leave: msg({
    id: "security.tokens.reveal.leave",
    message: "This token cannot be shown again after this is closed.",
  }),
};
