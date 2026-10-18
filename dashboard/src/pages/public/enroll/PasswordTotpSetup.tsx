import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useSuspenseQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { buildTotpUri, generateTotpSecret, isValidTotpCode } from "@/api/auth";
import { ApiError, isCancellation } from "@/api/errors";
import { publicConfigQueryOptions } from "@/api/queries";
import {
  NewPasswordFields,
  newPasswordFormValues,
} from "@/components/custom/NewPasswordFields";
import { PasswordTotpStepHeader } from "@/components/custom/PasswordTotpStepHeader";
import { PublicStep } from "@/components/custom/PublicStep";
import { RecoveryCodes } from "@/components/custom/RecoveryCodes";
import { firstRecoveryCodesCopy } from "@/components/custom/secret-reveal-copy";
import { TotpSetup } from "@/components/custom/TotpSetup";
import {
  useFocusOnStep,
  useSetupSteps,
} from "@/components/custom/use-setup-steps";
import { totpCodeInvalid } from "@/forms/password-totp-rules";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

export interface PasswordTotpSubmission {
  password: string;
  secretBase32: string;
  code: string;
}

/** The field a failed submit belongs on, when it belongs on one. */
export function failedField(error: unknown): "password" | "code" | undefined {
  if (!(error instanceof ApiError)) return undefined;
  if (error.code === "bad_credentials") return "code";
  if (error.code === "validation_failed") {
    const location = error.details?.location;
    if (location === "password" || location === "code") return location;
  }
  return undefined;
}

/**
 * The first password and authenticator an account gets, on the public card:
 * a password, then the authenticator, then the recovery codes the server
 * answers with. The enrollment page and the page that adds a sign-in after a
 * first federated one share it, each with its own `submit`.
 *
 * One form spans the first two steps, both mounted and one hidden, so going
 * back keeps what was typed and the secret stays the one the authenticator
 * was shown. A new step puts focus in its first field. A failed submit that
 * belongs on a field is marked there — the code, which is cleared, or the
 * password, which takes the reader back to it. `onSubmitError` claims a
 * failure for the caller first, such as a username taken in the meantime,
 * and anything else is left to the error toast. The codes step has no way
 * back: the factors are set once the server has answered.
 */
export function PasswordTotpSetup({
  accountName,
  secret: givenSecret,
  onBack,
  submit,
  onSubmitError,
  onDone,
}: {
  /** The account the authenticator's entry is labelled with. */
  accountName: string;
  /**
   * The authenticator secret, for a caller that must keep it across leaving
   * and coming back to this setup. One is drawn on mount otherwise.
   */
  secret?: string;
  /** The first step's way back, to the caller's choice of method. */
  onBack: () => void;
  submit: (submission: PasswordTotpSubmission) => Promise<string[]>;
  /** Returns whether the caller has handled the failure itself. */
  onSubmitError?: (error: unknown) => boolean;
  onDone: () => Promise<void>;
}) {
  const { t } = useLingui();
  const { data: config } = useSuspenseQuery(publicConfigQueryOptions());
  const [drawnSecret] = useState(() => generateTotpSecret());
  const secret = givenSecret ?? drawnSecret;
  const uri = useMemo(
    () => buildTotpUri(secret, accountName, config.totp),
    [secret, accountName, config.totp],
  );
  const [codes, setCodes] = useState<string[]>();
  const steps = useSetupSteps();
  const { step, stepRef, go, passwordPane, authenticatorPane } = steps;

  const form = useAppForm({
    defaultValues: newPasswordFormValues,
    onSubmit: async ({ value, formApi }) => {
      if (stepRef.current === "password") {
        go("authenticator");
        return;
      }
      try {
        setCodes(
          await submit({
            password: value.password,
            secretBase32: secret,
            code: value.code,
          }),
        );
      } catch (error) {
        if (isCancellation(error) || onSubmitError?.(error)) return;
        const field = failedField(error);
        if (field === undefined) return;
        if (field === "code") {
          formApi.setFieldValue("code", "", { dontValidate: true });
        }
        applyServerError(form, error, {
          locations: { password: "password", code: "code" },
          codes: { bad_credentials: "code" },
        });
        if (field === "password") go("password");
      }
    },
  });

  const submitting = useStore(form.store, (state) => state.isSubmitting);

  useFocusOnStep(steps, submitting);

  const progress = (current: number) => (
    <PasswordTotpStepHeader current={current} />
  );

  if (codes) {
    return (
      <div className="flex flex-col gap-5">
        {progress(2)}
        <PublicStep title={t(firstRecoveryCodesCopy.title)}>
          <RecoveryCodes
            codes={codes}
            onContinue={onDone}
            firstIssue
            titled={false}
          />
        </PublicStep>
      </div>
    );
  }

  return (
    <form.AppForm>
      <form.Form
        className="flex flex-col gap-5"
        label={t({
          id: "enroll.password_totp.form",
          message: "Set up a password and an authenticator",
        })}
      >
        {progress(step === "password" ? 0 : 1)}
        <PublicStep
          // A new title is announced as the step changes; focus then goes on
          // to the step's first field.
          titleKey={step}
          back={{
            label: t({ id: "enroll.password_totp.back", message: "Back" }),
            disabled: submitting,
            onClick: step === "password" ? onBack : () => go("password"),
          }}
          title={
            step === "password" ? (
              <Trans id="enroll.password_totp.password.title">
                Set a password
              </Trans>
            ) : (
              <Trans id="enroll.password_totp.authenticator.title">
                Set up an authenticator
              </Trans>
            )
          }
          actions={
            <PublicStep.Actions>
              <form.SubmitButton fullWidth size="lg">
                {step === "password" ? (
                  <Trans id="enroll.password_totp.next">Continue</Trans>
                ) : (
                  <Trans id="enroll.password_totp.finish">
                    Verify and finish
                  </Trans>
                )}
              </form.SubmitButton>
            </PublicStep.Actions>
          }
        >
          {/* Both steps stay mounted, so the form keeps every value and
              validator; the one not in view is out of layout and focus. */}
          <div
            ref={passwordPane}
            hidden={step !== "password"}
            className="flex flex-col gap-4"
          >
            <NewPasswordFields
              form={form}
              validateOn="blur"
              size="lg"
              passwordLabel={
                <Trans id="enroll.password_totp.password">Password</Trans>
              }
              confirmLabel={
                <Trans id="enroll.password_totp.confirm">
                  Confirm password
                </Trans>
              }
            />
          </div>
          <div
            ref={authenticatorPane}
            hidden={step !== "authenticator"}
            className="flex flex-col gap-4"
          >
            <TotpSetup secret={secret} uri={uri} onSurface />
            <form.AppField
              name="code"
              validators={{
                onBlur: ({ value }) =>
                  stepRef.current === "password" ||
                  isValidTotpCode(value, config.totp.digits)
                    ? undefined
                    : totpCodeInvalid,
              }}
            >
              {(field) => (
                <field.OtpField
                  label={
                    <Trans id="enroll.password_totp.code">
                      Authenticator code
                    </Trans>
                  }
                  digits={config.totp.digits}
                  size="lg"
                />
              )}
            </form.AppField>
          </div>
        </PublicStep>
      </form.Form>
    </form.AppForm>
  );
}
