import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { type UseMutationResult, useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { isValidTotpCode } from "@/api/auth";
import { isCancellation } from "@/api/errors";
import { publicConfigQueryOptions } from "@/api/queries";
import type { RecoveryCodesResult } from "@/api/raw-paths";
import { Button } from "@/components/custom/Button";
import { PasswordTotpStepHeader } from "@/components/custom/PasswordTotpStepHeader";
import { RecoveryCodes } from "@/components/custom/RecoveryCodes";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { TotpSetup } from "@/components/custom/TotpSetup";
import { getTopLayerContainer } from "@/components/custom/top-layer";
import {
  Dialog,
  DialogFooter,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  checkPassword,
  passwordMismatch,
  totpCodeInvalid,
} from "@/forms/password-totp-rules";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { useTotpSetup } from "@/pages/security/use-totp-setup";

/** What the setup last produced: the codes, and which wording fits them. */
export type SetupResult = { codes: string[]; firstIssue: boolean };

type SetupSubmit = UseMutationResult<
  RecoveryCodesResult,
  Error,
  { password: string; secret_base32: string; code: string }
>;

/**
 * Setting up both factors in one dialog: a password, then an authenticator,
 * then the recovery codes the write returns. One form runs through the first
 * two steps and the request goes out once, from the second, through the
 * identity check.
 *
 * The codes are the panel's, which also owns the write: both outlive the
 * button that opened this dialog. While the codes are on screen the dialog
 * cannot be dismissed — the saved confirmation unlocks Continue, which is the
 * only way out, because the server will never show them again. A request in
 * flight holds the dialog open too, so a refusal lands on the field it
 * belongs to.
 */
export function SetupDialog({
  isOpen,
  attempt,
  replacing,
  result,
  submit,
  onClose,
  onResult,
  onDone,
}: {
  isOpen: boolean;
  /** Changes on every opening, so the form starts over with a new secret. */
  attempt: number;
  /** Whether a password or an authenticator exists that this will replace. */
  replacing: boolean;
  result: SetupResult | null;
  submit: SetupSubmit;
  onClose: () => void;
  onResult: (codes: string[]) => void;
  onDone: () => Promise<void>;
}) {
  const revealing = result !== null;
  return (
    <Dialog
      open={isOpen}
      onOpenChange={(open) => {
        if (!open && !submit.isPending && !revealing) onClose();
      }}
    >
      <DialogPopup
        className="sm:max-w-xl"
        showCloseButton={!revealing}
        portalProps={{ container: getTopLayerContainer() }}
      >
        {result !== null ? (
          <RecoveryCodes
            progress={<Steps current={2} />}
            codes={result.codes}
            firstIssue={result.firstIssue}
            onContinue={onDone}
          />
        ) : (
          <SetupForm
            key={attempt}
            submit={submit}
            replacing={replacing}
            onCancel={onClose}
            onCodes={onResult}
          />
        )}
      </DialogPopup>
    </Dialog>
  );
}

/** Clear of the dialog's close button, which sits in the header's corner. */
function Steps({ current }: { current: number }) {
  return (
    <div className="pe-8">
      <PasswordTotpStepHeader current={current} />
    </div>
  );
}

type SetupStep = "password" | "authenticator";

function SetupForm({
  submit,
  replacing,
  onCancel,
  onCodes,
}: {
  submit: SetupSubmit;
  replacing: boolean;
  onCancel: () => void;
  onCodes: (codes: string[]) => void;
}) {
  const { t } = useLingui();
  const config = useQuery(publicConfigQueryOptions());
  const setup = useTotpSetup();
  const [step, setStep] = useState<SetupStep>("password");
  // The validators and the submit handler read the step through a ref, so a
  // handler that was created before the step changed still sees the new one.
  const stepRef = useRef<SetupStep>(step);
  const moved = useRef(false);
  const passwordPane = useRef<HTMLDivElement>(null);
  const authenticatorPane = useRef<HTMLDivElement>(null);

  const go = (next: SetupStep) => {
    stepRef.current = next;
    moved.current = true;
    setStep(next);
  };

  const form = useAppForm({
    defaultValues: { password: "", confirm: "", code: "" },
    // "Next" is this submit on the first step, so Enter in a password field
    // moves on too; the code is checked only on the second.
    onSubmit: async ({ value }) => {
      if (stepRef.current === "password") {
        go("authenticator");
        return;
      }
      try {
        const result = await submit.mutateAsync({
          password: value.password,
          secret_base32: setup.secret,
          code: value.code,
        });
        onCodes(result.recovery_codes);
      } catch (error) {
        // Dismissing the identity check is a decision, not a failure.
        if (isCancellation(error)) return;
        applyServerError(form, error, {
          locations: { password: "password", code: "code" },
          codes: {},
        });
        if ((form.getFieldMeta("password")?.errors.length ?? 0) > 0) {
          go("password");
        }
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

  return (
    <form.AppForm>
      <DialogHeader>
        <Steps current={step === "password" ? 0 : 1} />
        <DialogTitle>
          {step === "password" ? (
            <Trans id="security.setup.step.password">Set a password</Trans>
          ) : (
            <Trans id="security.setup.step.authenticator">
              Set up an authenticator
            </Trans>
          )}
        </DialogTitle>
      </DialogHeader>
      <form.Form
        label={t({
          id: "security.setup.dialog",
          message: "Set up password and authenticator",
        })}
        className="flex min-h-0 flex-1 flex-col"
      >
        <DialogPanel className="flex flex-col gap-4">
          <form.FormError />
          {/* Both steps stay mounted, so the form keeps every value and
              validator; the one not in view is out of layout and focus. */}
          <div
            ref={passwordPane}
            hidden={step !== "password"}
            className="flex flex-col gap-4"
          >
            {replacing && (
              <SurfaceAlert status="warning">
                <SurfaceAlert.Indicator />
                <SurfaceAlert.Content>
                  <SurfaceAlert.Title>
                    <Trans id="security.setup.replacing">
                      Once this is done, your current password, authenticator
                      and recovery codes are all replaced.
                    </Trans>
                  </SurfaceAlert.Title>
                </SurfaceAlert.Content>
              </SurfaceAlert>
            )}
            <form.AppField
              name="password"
              validators={{ onChange: ({ value }) => checkPassword(value) }}
            >
              {(field) => (
                <field.FormField
                  label={<Trans id="security.password.new">New password</Trans>}
                  type="password"
                  autoComplete="new-password"
                />
              )}
            </form.AppField>
            <form.AppField
              name="confirm"
              validators={{
                onChangeListenTo: ["password"],
                onChange: ({ value, fieldApi }) =>
                  value === fieldApi.form.getFieldValue("password")
                    ? undefined
                    : passwordMismatch,
              }}
            >
              {(field) => (
                <field.FormField
                  label={
                    <Trans id="security.password.confirm">
                      Repeat new password
                    </Trans>
                  }
                  type="password"
                  autoComplete="new-password"
                />
              )}
            </form.AppField>
          </div>
          <div
            ref={authenticatorPane}
            hidden={step !== "authenticator"}
            className="flex flex-col gap-4"
          >
            <TotpSetup secret={setup.secret} uri={setup.uri} split />
            <form.AppField
              name="code"
              validators={{
                onChange: ({ value }) =>
                  stepRef.current === "password" ||
                  (config.data &&
                    isValidTotpCode(value, config.data.totp.digits))
                    ? undefined
                    : totpCodeInvalid,
              }}
            >
              {(field) => (
                <field.OtpField
                  label={
                    <Trans id="security.setup.code">
                      Code from your authenticator
                    </Trans>
                  }
                  digits={config.data?.totp.digits ?? 6}
                  size="lg"
                />
              )}
            </form.AppField>
          </div>
        </DialogPanel>
        <DialogFooter>
          {step === "password" ? (
            <>
              <Button variant="ghost" onClick={onCancel}>
                <Trans id="security.cancel">Cancel</Trans>
              </Button>
              <form.SubmitButton fullWidth>
                <Trans id="security.setup.next">Next</Trans>
              </form.SubmitButton>
            </>
          ) : (
            <>
              <span className="me-auto self-center text-muted-foreground text-xs max-sm:hidden">
                <Trans id="security.setup.key_note">
                  The key was made in this browser and is sent only when you
                  confirm.
                </Trans>
              </span>
              <Button
                variant="ghost"
                disabled={submit.isPending}
                onClick={() => go("password")}
              >
                <Trans id="security.setup.back">Back</Trans>
              </Button>
              <form.SubmitButton fullWidth>
                <Trans id="security.setup.turn_on">Turn on</Trans>
              </form.SubmitButton>
            </>
          )}
        </DialogFooter>
      </form.Form>
    </form.AppForm>
  );
}
