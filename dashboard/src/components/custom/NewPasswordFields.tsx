import type { ReactNode } from "react";
import { checkPassword, passwordMismatch } from "@/forms/password-totp-rules";
import { withForm } from "@/forms/use-app-form";

/**
 * The shape of every form that holds a new password. `code` is the setup
 * flows' authenticator code: the field pair is typed against the whole form,
 * so a form without one (changing the password alone) carries it empty.
 */
export const newPasswordFormValues = { password: "", confirm: "", code: "" };

/**
 * A new password and its repeat, checked against the server's rule and against
 * each other. The form that holds them is shaped like `newPasswordFormValues`; `validateOn` picks whether it speaks up while typing or on leaving
 * the field, and `size` follows the surface it sits on.
 */
export const NewPasswordFields = withForm({
  defaultValues: newPasswordFormValues,
  props: {} as {
    passwordLabel: ReactNode;
    confirmLabel: ReactNode;
    validateOn?: "change" | "blur";
    size?: "default" | "lg";
  },
  render: function Render({
    form,
    passwordLabel,
    confirmLabel,
    validateOn = "change",
    size,
  }) {
    const onBlur = validateOn === "blur";
    const matches = ({
      value,
      fieldApi,
    }: {
      value: string;
      fieldApi: { form: { getFieldValue: (name: "password") => string } };
    }) =>
      value === fieldApi.form.getFieldValue("password")
        ? undefined
        : passwordMismatch;
    return (
      <>
        <form.AppField
          name="password"
          validators={
            onBlur
              ? { onBlur: ({ value }) => checkPassword(value) }
              : { onChange: ({ value }) => checkPassword(value) }
          }
        >
          {(field) => (
            <field.FormField
              label={passwordLabel}
              type="password"
              autoComplete="new-password"
              size={size}
            />
          )}
        </form.AppField>
        <form.AppField
          name="confirm"
          validators={
            onBlur
              ? { onBlurListenTo: ["password"], onBlur: matches }
              : { onChangeListenTo: ["password"], onChange: matches }
          }
        >
          {(field) => (
            <field.FormField
              label={confirmLabel}
              type="password"
              autoComplete="new-password"
              size={size}
            />
          )}
        </form.AppField>
      </>
    );
  },
});
