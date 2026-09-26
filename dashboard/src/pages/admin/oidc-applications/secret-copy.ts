import { msg } from "@lingui/core/macro";
import type { SecretRevealCopy } from "@/components/custom/secret-reveal-copy";

/**
 * Wording for the one-time disclosure of an OIDC client secret.
 *
 * The client secret differs from the console's other one-time secrets in who
 * holds it and what losing it costs: a recovery code is an account's own way
 * back in, while this is a credential an administrator pastes into someone
 * else's configuration. So the warning says where it goes and what happens if
 * it is lost rather than urging the reader to "keep it safe", and the continue
 * control says what it does — it leaves for the application's page.
 *
 * Rotation reuses the same wording: the new secret replaces the old in exactly
 * the same way, and the dialog's own wording for that is the confirmation
 * before it, not a second disclaimer here.
 */
export const clientSecretCopy: SecretRevealCopy = {
  title: msg({
    id: "admin.oidc-apps.secret.title",
    message: "Copy the client secret",
  }),
  once: msg({
    id: "admin.oidc-apps.secret.once",
    message:
      "This secret is shown only once. Put it into the client's configuration now — it cannot be read back later, and a client that loses it stops signing in.",
  }),
  label: msg({
    id: "admin.oidc-apps.secret.label",
    message: "Client secret",
  }),
  copy: msg({ id: "admin.oidc-apps.secret.copy", message: "Copy secret" }),
  download: msg({
    id: "admin.oidc-apps.secret.download",
    message: "Download secret",
  }),
  copyFailed: msg({
    id: "admin.oidc-apps.secret.copy_failed",
    message:
      "Could not copy the secret. Select it above and copy it manually, or download it.",
  }),
  downloadFailed: msg({
    id: "admin.oidc-apps.secret.download_failed",
    message: "Could not download the secret. Copy it or save it manually.",
  }),
  saved: msg({
    id: "admin.oidc-apps.secret.saved",
    message: "I have saved the client secret",
  }),
  continueLabel: msg({
    id: "admin.oidc-apps.secret.continue",
    message: "Go to the application",
  }),
  leave: msg({
    id: "admin.oidc-apps.secret.leave",
    message: "Leave without saving? This secret cannot be shown again.",
  }),
};
