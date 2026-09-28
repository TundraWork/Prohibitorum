import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useSuspenseQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { buildTotpUri, generateTotpSecret, isValidTotpCode } from "@/api/auth";
import { ApiError, isCancellation } from "@/api/errors";
import { publicConfigQueryOptions } from "@/api/queries";
import { PublicStep } from "@/components/custom/PublicStep";
import { RecoveryCodes } from "@/components/custom/RecoveryCodes";
import { firstRecoveryCodesCopy } from "@/components/custom/secret-reveal-copy";
import { TotpSetup } from "@/components/custom/TotpSetup";
import {
  checkPassword,
  passwordMismatch,
  totpCodeInvalid,
} from "@/forms/password-totp-rules";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

export interface PasswordTotpSubmission {
  password: string;
  secretBase32: string;
  code: string;
}

type Step = "password" | "authenticator";

/** The field a failed submit belongs on, when it belongs on one. */
function failedField(error: unknown): "password" | "code" | undefined {
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
  const [step, setStep] = useState<Step>("password");
  const [codes, setCodes] = useState<string[]>();
  // The validators and the submit handler read the step through a ref, so a
  // handler created before the step changed still sees the new one.
  const stepRef = useRef<Step>(step);
  const moved = useRef(false);
  const passwordPane = useRef<HTMLDivElement>(null);
  const authenticatorPane = useRef<HTMLDivElement>(null);

  const go = (next: Step) => {
    stepRef.current = next;
    moved.current = true;
    setStep(next);
  };

  const form = useAppForm({
    defaultValues: { password: "", confirm: "", code: "" },
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

  // A step change takes the control that caused it away, so focus moves to
  // the field the new step starts with. It waits for a submit to settle,
  // because the fields are disabled until then and a disabled field does not
  // take focus.
  useEffect(() => {
    if (!moved.current || submitting) return;
    moved.current = false;
    const target =
      step === "password"
        ? passwordPane.current?.querySelector("input")
        : authenticatorPane.current?.querySelector<HTMLInputElement>(
            'input[autocomplete="one-time-code"]',
          );
    target?.focus();
  }, [step, submitting]);

  if (codes) {
    return (
      <PublicStep title={t(firstRecoveryCodesCopy.title)}>
        <RecoveryCodes
          codes={codes}
          onContinue={onDone}
          onSurface
          firstIssue
          titled={false}
        />
      </PublicStep>
    );
  }

  return (
    <form.AppForm>
      <form.Form
        label={t({
          id: "enroll.password_totp.form",
          message: "Set up a password and an authenticator",
        })}
      >
        <PublicStep
          // A new title is announced as the step changes; focus then goes on
          // to the step's first field.
          titleKey={step}
          back={{
            label: t({ id: "enroll.password_totp.back", message: "Back" }),
            isDisabled: submitting,
            onPress: step === "password" ? onBack : () => go("password"),
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
              <form.SubmitButton fullWidth>
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
            <form.AppField
              name="password"
              validators={{ onBlur: ({ value }) => checkPassword(value) }}
            >
              {(field) => (
                <field.FormField
                  label={
                    <Trans id="enroll.password_totp.password">Password</Trans>
                  }
                  type="password"
                  autoComplete="new-password"
                  variant="secondary"
                />
              )}
            </form.AppField>
            <form.AppField
              name="confirm"
              validators={{
                onBlurListenTo: ["password"],
                onBlur: ({ value, fieldApi }) =>
                  value === fieldApi.form.getFieldValue("password")
                    ? undefined
                    : passwordMismatch,
              }}
            >
              {(field) => (
                <field.FormField
                  label={
                    <Trans id="enroll.password_totp.confirm">
                      Confirm password
                    </Trans>
                  }
                  type="password"
                  autoComplete="new-password"
                  variant="secondary"
                />
              )}
            </form.AppField>
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
                  variant="secondary"
                />
              )}
            </form.AppField>
          </div>
        </PublicStep>
      </form.Form>
    </form.AppForm>
  );
}
