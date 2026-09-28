import { Card, Chip, Modal, Tooltip } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import {
  type UseMutationResult,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  LifeBuoy,
  PowerOff,
  RectangleEllipsis,
  Smartphone,
} from "lucide-react";
import { type Ref, useEffect, useRef, useState } from "react";
import {
  buildTotpUri,
  generateTotpSecret,
  isValidLoginPassword,
  isValidTotpCode,
} from "@/api/auth";
import { describeError, isCancellation } from "@/api/errors";
import {
  passwordTotpMutationOptions,
  regenerateRecoveryCodesMutationOptions,
  replaceTotpMutationOptions,
  revokePasswordTotpMutationOptions,
  setPasswordMutationOptions,
} from "@/api/mutations";
import {
  factorsQueryOptions,
  publicConfigQueryOptions,
  sessionQueryOptions,
} from "@/api/queries";
import type { RecoveryCodesResult } from "@/api/raw-paths";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { ListSkeleton } from "@/components/custom/ListSkeleton";
import { RecoveryCodes } from "@/components/custom/RecoveryCodes";
import { Section } from "@/components/custom/Section";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import {
  firstRecoveryCodesCopy,
  recoveryCodesCopy,
} from "@/components/custom/secret-reveal-copy";
import { TotpSetup } from "@/components/custom/TotpSetup";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

const passwordInvalid = msg({
  id: "security.password.invalid",
  message: "Use a password of at least 8 characters.",
});
const passwordMismatch = msg({
  id: "security.password.mismatch",
  message: "The two passwords do not match.",
});
const codeInvalid = msg({
  id: "security.totp.invalid",
  message: "Enter the code your authenticator shows, using only 0–9.",
});

/** The password rule is the server's: 8 to 1024 bytes, without trimming. */
function checkPassword(value: string) {
  return isValidLoginPassword(value) && value.length >= 8
    ? undefined
    : passwordInvalid;
}

/** Codes this few or fewer are worth a mark on the row. */
const lowRecoveryCodes = 3;

/** What the setup dialog last produced: the codes, and which wording fits them. */
type SetupResult = { codes: string[]; firstIssue: boolean };

/**
 * Password and authenticator.
 *
 * The list has a row per factor — the password, the authenticator, the
 * recovery codes — and each row says in its details whether that factor is
 * there. The two are set up together and turned off together, and changed one
 * at a time: the backend's one call that establishes them writes the password,
 * the authenticator and a batch of recovery codes at once, and a password set
 * on its own would leave the account without a second factor. So while either
 * is missing the rows carry no actions, the missing one recedes, and the
 * section's heading carries the single way forward: setting up both, in a
 * dialog. Having only one of them is a fallback state rather than a flow; the
 * rows keep it legible, the setup repairs it and the last row turns it off.
 *
 * The recovery codes a write returns are held here rather than inside the row
 * or the button that produced them. Every one of those writes invalidates the
 * factors query, which re-renders this panel and may unmount that row or
 * button the moment it succeeds — so a local copy would be thrown away before
 * the user could read it, and those codes are the only time the server will
 * ever show them. The setup dialog's open state is held here for the same
 * reason: the button that opens it is gone once both factors exist.
 */
export function PasswordTotpPanel() {
  const { t } = useLingui();
  const factors = useQuery(factorsQueryOptions());
  const [dialogCodes, setDialogCodes] = useState<string[] | null>(null);
  const [setupOpen, setSetupOpen] = useState(false);
  const [setupAttempt, setSetupAttempt] = useState(0);
  const [setupResult, setSetupResult] = useState<SetupResult | null>(null);
  // Read when the dialog opens: the write refetches the factors before its
  // codes arrive, so by then they no longer say what the setup replaced.
  const [setupBefore, setSetupBefore] = useState({
    replacing: false,
    firstIssue: true,
  });
  const [focusChange, setFocusChange] = useState(false);
  const changeButton = useRef<HTMLButtonElement>(null);

  // A finished setup removes the button that opened it, so focus goes to the
  // first action the new rows offer once they have arrived.
  useEffect(() => {
    if (!focusChange || !changeButton.current) return;
    changeButton.current.focus();
    setFocusChange(false);
  });

  const title = (
    <Trans id="security.section.password">Password and authenticator</Trans>
  );

  if (factors.isPending) {
    // The block's own shape while its read is in flight: the heading is
    // already on screen, and the card below it is the one the rows will
    // arrive in, so the page does not jump when they do.
    return (
      <Section title={title}>
        <Card className="gap-0 p-0">
          <ListSkeleton />
        </Card>
      </Section>
    );
  }

  if (!factors.data) {
    return (
      <Section title={title}>
        <SurfaceAlert status="danger" role="alert">
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>
              {t(describeError(factors.error))}
            </SurfaceAlert.Title>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      </Section>
    );
  }

  const { passwordSet, totpEnrolled, passkeyCount, recoveryCodesRemaining } =
    factors.data;
  const bothSet = passwordSet && totpEnrolled;

  const openSetup = () => {
    // Each opening starts over with a new secret; the last result is dropped
    // only now, so the dialog still had it to draw while it closed.
    setSetupResult(null);
    setSetupBefore({
      replacing: passwordSet || totpEnrolled,
      firstIssue: !totpEnrolled,
    });
    setSetupAttempt((count) => count + 1);
    setSetupOpen(true);
  };

  return (
    <Section
      title={title}
      action={
        !bothSet && (
          <Button onPress={openSetup}>
            <Trans id="security.setup.open">
              Set up password and authenticator
            </Trans>
          </Button>
        )
      }
    >
      <ItemList
        label={t({
          id: "security.password.list",
          message: "Password and authenticator",
        })}
        empty={null}
      >
        <PasswordRow
          key="password"
          isSet={passwordSet}
          canChange={bothSet}
          buttonRef={changeButton}
        />
        <AuthenticatorRow
          key="authenticator"
          isSet={totpEnrolled}
          canReplace={bothSet}
          onCodes={setDialogCodes}
        />
        {totpEnrolled && (
          <RecoveryCodesRow
            key="recovery-codes"
            remaining={recoveryCodesRemaining}
            canRegenerate={bothSet}
            onCodes={setDialogCodes}
          />
        )}
        {(passwordSet || totpEnrolled) && (
          <RevokeRow key="revoke" passkeyCount={passkeyCount} />
        )}
      </ItemList>
      <SetupDialog
        isOpen={setupOpen}
        attempt={setupAttempt}
        replacing={setupBefore.replacing}
        firstIssue={setupBefore.firstIssue}
        result={setupResult}
        onClose={() => setSetupOpen(false)}
        onResult={setSetupResult}
        onDone={async () => {
          setSetupOpen(false);
          setFocusChange(true);
        }}
      />
      {dialogCodes !== null && (
        <RecoveryCodesDialog
          codes={dialogCodes}
          onContinue={async () => setDialogCodes(null)}
        />
      )}
    </Section>
  );
}

/** "Set" or "Not set", the state a factor row reports in its details. */
function FactorState({ isSet }: { isSet: boolean }) {
  return isSet ? (
    <Trans id="security.factor.set">Set</Trans>
  ) : (
    <Trans id="security.factor.unset">Not set</Trans>
  );
}

/**
 * Setting up both factors: a password, then an authenticator, then the
 * recovery codes the write returns. One form runs through the first two
 * steps — each has its own heading and footer — and the request goes out
 * once, from the second, through the identity check.
 *
 * "Next" is the form's submit on the first step, so Enter in a password field
 * moves on as well; the code is checked only on the second step, which is
 * what keeps the first from refusing an empty code the reader has not reached.
 *
 * The codes are the panel's (see there). While they are on screen the dialog
 * cannot be dismissed: the saved confirmation unlocks "Continue", which is the
 * only way out, because the server will never show them again.
 */
function SetupDialog({
  isOpen,
  attempt,
  replacing,
  firstIssue,
  result,
  onClose,
  onResult,
  onDone,
}: {
  isOpen: boolean;
  /** Changes on every opening, so the form starts over with a new secret. */
  attempt: number;
  /** Whether a password or an authenticator exists that this will replace. */
  replacing: boolean;
  /** Whether the codes it returns are the account's first. */
  firstIssue: boolean;
  result: SetupResult | null;
  onClose: () => void;
  onResult: (result: SetupResult) => void;
  onDone: () => Promise<void>;
}) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const submit = useMutation(passwordTotpMutationOptions(queryClient));
  const locked = result !== null;

  return (
    <Modal
      isOpen={isOpen}
      onOpenChange={(open) => {
        if (!open && !locked && !submit.isPending) onClose();
      }}
    >
      <Modal.Backdrop
        isDismissable={!locked}
        isKeyboardDismissDisabled={locked}
      >
        <Modal.Container placement="center" size="md" scroll="inside">
          <Modal.Dialog>
            {result === null ? (
              <SetupForm
                key={attempt}
                submit={submit}
                replacing={replacing}
                onCancel={onClose}
                onCodes={(codes) => onResult({ codes, firstIssue })}
              />
            ) : (
              <>
                <Modal.Header>
                  <Modal.Heading>
                    {t(
                      result.firstIssue
                        ? firstRecoveryCodesCopy.title
                        : recoveryCodesCopy.title,
                    )}
                  </Modal.Heading>
                </Modal.Header>
                <RecoveryCodes
                  codes={result.codes}
                  firstIssue={result.firstIssue}
                  onContinue={onDone}
                  inDialog
                />
              </>
            )}
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}

type SetupStep = "password" | "authenticator";

function SetupForm({
  submit,
  replacing,
  onCancel,
  onCodes,
}: {
  submit: UseMutationResult<
    RecoveryCodesResult,
    Error,
    { password: string; secret_base32: string; code: string }
  >;
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
  // the field the new step starts with: the first password, or the code. It
  // waits for a submit to settle, because the fields are disabled until then
  // and a disabled field does not take focus.
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
      <Modal.Header>
        <Modal.Heading>
          {step === "password" ? (
            <Trans id="security.setup.step.password">Set a password</Trans>
          ) : (
            <Trans id="security.setup.step.authenticator">
              Set up an authenticator
            </Trans>
          )}
        </Modal.Heading>
      </Modal.Header>
      <form.Form
        label={t({
          id: "security.setup.dialog",
          message: "Set up password and authenticator",
        })}
        className="flex min-h-0 flex-1 flex-col"
      >
        <Modal.Body>
          <div className="flex flex-col gap-4">
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
                    label={
                      <Trans id="security.password.new">New password</Trans>
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
              <TotpSetup secret={setup.secret} uri={setup.uri} onSurface />
              <form.AppField
                name="code"
                validators={{
                  onChange: ({ value }) =>
                    stepRef.current === "password" ||
                    (config.data &&
                      isValidTotpCode(value, config.data.totp.digits))
                      ? undefined
                      : codeInvalid,
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
                    variant="secondary"
                  />
                )}
              </form.AppField>
            </div>
          </div>
        </Modal.Body>
        <Modal.Footer className="mt-5">
          {step === "password" ? (
            <>
              <Button variant="secondary" onPress={onCancel}>
                <Trans id="security.cancel">Cancel</Trans>
              </Button>
              <form.SubmitButton>
                <Trans id="security.setup.next">Next</Trans>
              </form.SubmitButton>
            </>
          ) : (
            <>
              <Button
                variant="secondary"
                isDisabled={submit.isPending}
                onPress={() => go("password")}
              >
                <Trans id="security.setup.back">Back</Trans>
              </Button>
              <form.SubmitButton>
                <Trans id="security.setup.turn_on">Turn on</Trans>
              </form.SubmitButton>
            </>
          )}
        </Modal.Footer>
      </form.Form>
    </form.AppForm>
  );
}

/**
 * The codes a replace or a regeneration just issued, in a dialog over the panel
 * rather than in place of it. Both writes start from a section the user was
 * reading, so the codes are one step of that section and the page should still
 * be there when the dialog closes. The header carries the title, and the reveal
 * fills the body.
 *
 * The backdrop, Escape and clicks outside all leave the dialog open, because
 * the server will never show these codes again. The continue control is the
 * only way out, and the saved confirmation unlocks it (see `SecretReveal`).
 */
function RecoveryCodesDialog({
  codes,
  onContinue,
}: {
  codes: string[];
  onContinue: () => Promise<void>;
}) {
  const { t } = useLingui();
  return (
    // The dialog is open exactly while the codes are held here, so it cannot
    // outlive them and only `onContinue` can close it.
    <Modal isOpen onOpenChange={() => {}}>
      <Modal.Backdrop isDismissable={false} isKeyboardDismissDisabled>
        <Modal.Container placement="center" size="lg" scroll="inside">
          <Modal.Dialog>
            <Modal.Header>
              <Modal.Heading>{t(recoveryCodesCopy.title)}</Modal.Heading>
            </Modal.Header>
            {/* The reveal draws the body and the footer, so its controls stay
                under the codes however long the list is. */}
            <RecoveryCodes codes={codes} onContinue={onContinue} inDialog />
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}

/**
 * The password, and changing it. Changing is one step, so the row opens it in
 * a dialog; the toast says it is done once the dialog closes.
 */
function PasswordRow({
  isSet,
  canChange,
  buttonRef,
}: {
  isSet: boolean;
  canChange: boolean;
  buttonRef: Ref<HTMLButtonElement>;
}) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const change = useMutation(setPasswordMutationOptions(queryClient));

  const form = useAppForm({
    defaultValues: { password: "", confirm: "" },
    onSubmit: async ({ value }) => {
      try {
        await change.mutateAsync(value.password);
        setOpen(false);
        form.reset();
      } catch (error) {
        applyServerError(form, error, {
          locations: { password: "password" },
          codes: {},
        });
      }
    },
  });

  return (
    <ItemListRow
      icon={<RectangleEllipsis size={18} aria-hidden="true" />}
      title={<Trans id="security.factor.password">Password</Trans>}
      dimmed={!isSet}
      details={[<FactorState key="state" isSet={isSet} />]}
      actions={
        canChange && (
          <Button
            ref={buttonRef}
            size="sm"
            variant="secondary"
            onPress={() => setOpen(true)}
          >
            <Trans id="security.password.change.open">Change</Trans>
          </Button>
        )
      }
    >
      <Modal isOpen={open} onOpenChange={setOpen}>
        <Modal.Backdrop>
          <Modal.Container placement="center" size="md">
            <Modal.Dialog>
              <Modal.Header>
                <Modal.Heading>
                  <Trans id="security.password.change.title">
                    Change password
                  </Trans>
                </Modal.Heading>
              </Modal.Header>
              <form.AppForm>
                <form.Form
                  label={t({
                    id: "security.password.change.form",
                    message: "Change password",
                  })}
                  className="flex min-h-0 flex-1 flex-col"
                >
                  <Modal.Body>
                    <div className="flex flex-col gap-4">
                      <form.FormError />
                      <form.AppField
                        name="password"
                        validators={{
                          onChange: ({ value }) => checkPassword(value),
                        }}
                      >
                        {(field) => (
                          <field.FormField
                            label={
                              <Trans id="security.password.new">
                                New password
                              </Trans>
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
                            variant="secondary"
                          />
                        )}
                      </form.AppField>
                    </div>
                  </Modal.Body>
                  <Modal.Footer className="mt-5">
                    <Button
                      variant="secondary"
                      isDisabled={change.isPending}
                      onPress={() => setOpen(false)}
                    >
                      <Trans id="security.cancel">Cancel</Trans>
                    </Button>
                    <form.SubmitButton>
                      <Trans id="security.password.change.action">
                        Change password
                      </Trans>
                    </form.SubmitButton>
                  </Modal.Footer>
                </form.Form>
              </form.AppForm>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>
    </ItemListRow>
  );
}

/**
 * The authenticator, and replacing it. Replacing needs a new secret scanned
 * and a code from it, so the dialog carries the whole setup. The codes it
 * returns go up to the panel, which shows them once this dialog has closed.
 */
function AuthenticatorRow({
  isSet,
  canReplace,
  onCodes,
}: {
  isSet: boolean;
  canReplace: boolean;
  onCodes: (codes: string[]) => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <ItemListRow
      icon={<Smartphone size={18} aria-hidden="true" />}
      title={<Trans id="security.factor.authenticator">Authenticator</Trans>}
      dimmed={!isSet}
      details={[<FactorState key="state" isSet={isSet} />]}
      actions={
        canReplace && (
          <Button size="sm" variant="secondary" onPress={() => setOpen(true)}>
            <Trans id="security.totp.replace.open">Replace</Trans>
          </Button>
        )
      }
    >
      <Modal isOpen={open} onOpenChange={setOpen}>
        <Modal.Backdrop>
          <Modal.Container placement="center" size="md" scroll="inside">
            <Modal.Dialog>
              <Modal.Header>
                <Modal.Heading>
                  <Trans id="security.totp.replace.title">
                    Replace authenticator
                  </Trans>
                </Modal.Heading>
              </Modal.Header>
              {/* Mounted only while open, so every opening draws a fresh
                  secret rather than one the user may already have scanned. */}
              {open && (
                <ReplaceTotpForm
                  onCancel={() => setOpen(false)}
                  onCodes={(codes) => {
                    setOpen(false);
                    onCodes(codes);
                  }}
                />
              )}
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>
    </ItemListRow>
  );
}

function ReplaceTotpForm({
  onCancel,
  onCodes,
}: {
  onCancel: () => void;
  onCodes: (codes: string[]) => void;
}) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const config = useQuery(publicConfigQueryOptions());
  const replace = useMutation(replaceTotpMutationOptions(queryClient));
  const setup = useTotpSetup();
  const form = useAppForm({
    defaultValues: { code: "" },
    onSubmit: async ({ value }) => {
      if (!setup.secret || !config.data) return;
      try {
        const result = await replace.mutateAsync({
          secret_base32: setup.secret,
          code: value.code,
        });
        onCodes(result.recovery_codes);
      } catch (error) {
        applyServerError(form, error, {
          locations: { code: "code" },
          codes: {},
        });
      }
    },
  });

  return (
    <form.AppForm>
      <form.Form
        label={t({
          id: "security.totp.replace.form",
          message: "Replace authenticator",
        })}
        className="flex min-h-0 flex-1 flex-col"
      >
        <Modal.Body>
          <div className="flex flex-col gap-4">
            <form.FormError />
            <SurfaceAlert status="warning">
              <SurfaceAlert.Indicator />
              <SurfaceAlert.Content>
                <SurfaceAlert.Title>
                  <Trans id="security.totp.replace.note">
                    Your current authenticator and every existing recovery code
                    stop working as soon as this succeeds.
                  </Trans>
                </SurfaceAlert.Title>
              </SurfaceAlert.Content>
            </SurfaceAlert>
            <TotpSetup secret={setup.secret} uri={setup.uri} onSurface />
            <form.AppField
              name="code"
              validators={{
                onChange: ({ value }) =>
                  config.data && isValidTotpCode(value, config.data.totp.digits)
                    ? undefined
                    : codeInvalid,
              }}
            >
              {(field) => (
                <field.OtpField
                  label={<Trans id="security.totp.code">Current code</Trans>}
                  digits={config.data?.totp.digits ?? 6}
                  variant="secondary"
                />
              )}
            </form.AppField>
          </div>
        </Modal.Body>
        <Modal.Footer className="mt-5">
          <Button
            variant="secondary"
            isDisabled={replace.isPending}
            onPress={onCancel}
          >
            <Trans id="security.cancel">Cancel</Trans>
          </Button>
          <form.SubmitButton>
            <Trans id="security.totp.replace.action">
              Replace authenticator
            </Trans>
          </form.SubmitButton>
        </Modal.Footer>
      </form.Form>
    </form.AppForm>
  );
}

/**
 * How many recovery codes are left, marked once they run low. New codes void
 * every existing one, including any the user printed or saved, so the request
 * waits for a confirmation; the codes it returns open in the panel's dialog.
 */
function RecoveryCodesRow({
  remaining,
  canRegenerate,
  onCodes,
}: {
  remaining: number;
  canRegenerate: boolean;
  onCodes: (codes: string[]) => void;
}) {
  const queryClient = useQueryClient();
  const regenerate = useMutation(
    regenerateRecoveryCodesMutationOptions(queryClient),
  );
  const [confirming, setConfirming] = useState(false);
  const count = remaining;

  return (
    <ItemListRow
      icon={<LifeBuoy size={18} aria-hidden="true" />}
      title={<Trans id="security.recovery.title">Recovery codes</Trans>}
      badges={
        remaining === 0 ? (
          <Chip color="danger" size="sm" variant="soft">
            <Trans id="security.recovery.used_up">All used</Trans>
          </Chip>
        ) : remaining <= lowRecoveryCodes ? (
          <Chip color="warning" size="sm" variant="soft">
            <Trans id="security.recovery.low">Running low</Trans>
          </Chip>
        ) : undefined
      }
      details={[
        <Plural
          key="remaining"
          id="security.recovery.remaining"
          value={count}
          one="# code left"
          other="# codes left"
        />,
      ]}
      actions={
        canRegenerate && (
          <Button
            size="sm"
            variant="secondary"
            isPending={regenerate.isPending}
            onPress={() => setConfirming(true)}
          >
            <Trans id="security.recovery.regenerate">Regenerate</Trans>
          </Button>
        )
      }
    >
      <ConfirmDialog
        isOpen={confirming}
        onOpenChange={setConfirming}
        status="warning"
        title={
          <Trans id="security.recovery.confirm.title">
            Replace your recovery codes?
          </Trans>
        }
        body={
          <p>
            <Trans id="security.recovery.confirm.body">
              Every recovery code you have now stops working.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="security.recovery.confirm.action">Replace codes</Trans>
        }
        isPending={regenerate.isPending}
        onConfirm={() => {
          regenerate.mutate(undefined, {
            onSuccess: (result) => onCodes(result.recovery_codes),
            onSettled: () => setConfirming(false),
          });
        }}
      />
    </ItemListRow>
  );
}

/**
 * Turning both off, for as long as either exists. The server refuses it when
 * it would remove the last way in, which is exactly the case where there is
 * no passkey to fall back on, so the button is disabled then and says why.
 */
function RevokeRow({ passkeyCount }: { passkeyCount: number }) {
  const queryClient = useQueryClient();
  const revoke = useMutation(revokePasswordTotpMutationOptions(queryClient));
  const [confirming, setConfirming] = useState(false);
  const lastWay = passkeyCount === 0;

  const button = (
    <Button
      size="sm"
      variant="danger-soft"
      isDisabled={lastWay}
      isPending={revoke.isPending}
      onPress={() => setConfirming(true)}
    >
      <Trans id="security.revoke.action">Turn off</Trans>
    </Button>
  );

  return (
    <ItemListRow
      icon={<PowerOff size={18} aria-hidden="true" />}
      title={
        <Trans id="security.revoke.title">
          Turn off password and authenticator
        </Trans>
      }
      actions={
        // A disabled button emits no hover or focus, so the tooltip listens
        // on the trigger wrapper instead.
        lastWay ? (
          <Tooltip delay={0}>
            <Tooltip.Trigger>{button}</Tooltip.Trigger>
            <Tooltip.Content>
              <Trans id="security.revoke.last_tooltip">
                This is your only way to sign in. Add a passkey first.
              </Trans>
            </Tooltip.Content>
          </Tooltip>
        ) : (
          button
        )
      }
    >
      <ConfirmDialog
        isOpen={confirming}
        onOpenChange={(next) => !next && setConfirming(false)}
        status="danger"
        title={
          <Trans id="security.revoke.confirm.title">
            Turn off password and authenticator?
          </Trans>
        }
        body={
          <p>
            <Trans id="security.revoke.confirm.body">
              Your password, your authenticator and all recovery codes stop
              working immediately.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="security.revoke.confirm.action">Turn off</Trans>
        }
        isPending={revoke.isPending}
        onConfirm={() =>
          revoke.mutate(undefined, { onSettled: () => setConfirming(false) })
        }
      />
    </ItemListRow>
  );
}

/**
 * The locally generated authenticator secret and its otpauth URI.
 *
 * The secret never leaves the browser except as `secret_base32` on submit, and
 * a new one is drawn for every dialog that mounts this, so abandoning a
 * half-filled setup does not leave a stale secret to reappear.
 */
function useTotpSetup() {
  const config = useQuery(publicConfigQueryOptions());
  const session = useQuery({
    ...sessionQueryOptions(),
    refetchOnMount: false,
  });
  const [secret] = useState(() => generateTotpSecret());
  const username = session.data?.username;
  const totp = config.data?.totp;
  // buildTotpUri re-validates every field and throws on a malformed config, so
  // the two queries must both have landed before it is called.
  const uri =
    username !== undefined && totp !== undefined
      ? buildTotpUri(secret, username, totp)
      : "";
  return { secret, uri };
}
