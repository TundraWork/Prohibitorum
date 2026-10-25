import { msg } from "@lingui/core/macro";
import { isValidLoginPassword } from "@/api/auth";

export const passwordInvalid = msg({
  id: "security.password.invalid",
  message: "The password must be at least 8 characters.",
});

export const passwordMismatch = msg({
  id: "security.password.mismatch",
  message: "The two passwords do not match.",
});

/**
 * A new password, by the server's rule: 8 to 1024 bytes, without trimming.
 * The Security page and the first local sign-in both check it here, and
 * share the wording for a code the authenticator did not show.
 */
export function checkPassword(value: string) {
  return isValidLoginPassword(value) && value.length >= 8
    ? undefined
    : passwordInvalid;
}

export const totpCodeInvalid = msg({
  id: "security.totp.invalid",
  message: "Enter the code shown in your authenticator, using digits 0–9 only.",
});
