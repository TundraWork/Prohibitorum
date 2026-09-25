import { Alert, Description, Modal, Spinner } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type ReactNode, useState } from "react";
import {
  buildTotpUri,
  generateTotpSecret,
  isValidLoginPassword,
  isValidTotpCode,
} from "@/api/auth";
import { describeError } from "@/api/errors";
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
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { RecoveryCodes } from "@/components/custom/RecoveryCodes";
import { recoveryCodesCopy } from "@/components/custom/secret-reveal-copy";
import { TotpSetup } from "@/components/custom/TotpSetup";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

const passwordInvalid = msg({
  id: "security.password.invalid",
  message: "Use a password of at least 8 characters.",
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

/**
 * Password and authenticator.
 *
 * There is no separate "current state" section: the state of each factor
 * decides which rows the list holds and what their buttons say. Every row is
 * on screen at once; a row whose action needs input opens it in a dialog.
 *
 * The setup form is not a layout preference. The backend offers one atomic
 * call that establishes both factors together and replaces the recovery codes
 * as a batch, and a password set on its own would leave the account without a
 * second factor — so there is no interface here that sets only the missing
 * half. Setup is the one thing left to do, so it is drawn in place on a card
 * rather than behind a button.
 *
 * The recovery codes a write returns are held here rather than inside the
 * row that produced them. Every one of those writes invalidates the factors
 * query, which re-renders this panel and may unmount that row the moment it
 * succeeds — so a row-local copy would be thrown away before the user could
 * read it, and those codes are the only time the server will ever show them.
 *
 * The two writes the user asks for from a row they were reading — replacing
 * the authenticator and regenerating the recovery codes — show their codes in a
 * dialog over the panel, so the page is still there when the dialog closes. The
 * enrollment write sets up the account instead of doing one step of a row,
 * so it keeps the page-wide reveal that replaces the panel.
 */
export function PasswordTotpPanel() {
  const { t } = useLingui();
  const factors = useQuery(factorsQueryOptions());
  const [enrollmentCodes, setEnrollmentCodes] = useState<string[] | null>(null);
  const [dialogCodes, setDialogCodes] = useState<string[] | null>(null);
  const [rotate, setRotate] = useState(0);

  if (enrollmentCodes !== null) {
    return (
      <RecoveryCodes
        codes={enrollmentCodes}
        onContinue={async () => {
          setEnrollmentCodes(null);
          // A fresh secret for the next attempt, so the one the user just
          // consumed never reappears as if it were unused.
          setRotate((count) => count + 1);
        }}
      />
    );
  }

  if (factors.isPending) {
    return (
      <div className="flex items-center gap-2 text-sm text-muted">
        <Spinner size="sm" />
        <Trans id="security.loading">Loading…</Trans>
      </div>
    );
  }

  if (!factors.data) {
    return (
      <Alert status="danger" role="alert">
        <Alert.Indicator />
        <Alert.Content>
          <Alert.Title>{t(describeError(factors.error))}</Alert.Title>
        </Alert.Content>
      </Alert>
    );
  }

  const { passwordSet, totpEnrolled, passkeyCount } = factors.data;
  const rotateKey = (count: number) => `${count}`;
  // Both factors present means nothing is pending; a missing one leaves a
  // single section, which opens on arrival rather than waiting for a click.
  const bothSet = passwordSet && totpEnrolled;

  const rows: ReactNode[] = [];
  if (bothSet) {
    rows.push(
      <ChangePasswordRow key="change-password" />,
      <ReplaceTotpRow key="replace-totp" onCodes={setDialogCodes} />,
    );
  }
  if (totpEnrolled) {
    rows.push(
      <RecoveryCodesRow key="recovery-codes" onCodes={setDialogCodes} />,
    );
  }
  rows.push(<RevokeRow key="revoke" passkeyCount={passkeyCount} />);

  return (
    <div className="flex flex-col gap-4">
      {!bothSet && (
        // The one thing left to do is on screen as it is, not behind a click.
        <ConsoleCard
          title={
            passwordSet ? (
              <Trans id="security.setup.title.totp">
                Set up an authenticator with a new password
              </Trans>
            ) : (
              <Trans id="security.setup.title.both">
                Set up a password and authenticator
              </Trans>
            )
          }
        >
          <SetupForm
            key={rotateKey(rotate)}
            passwordSet={passwordSet}
            onCodes={setEnrollmentCodes}
          />
        </ConsoleCard>
      )}
      <ItemList
        label={t({
          id: "security.password.list",
          message: "Password and authenticator",
        })}
        empty={null}
      >
        {rows}
      </ItemList>
      {dialogCodes !== null && (
        <RecoveryCodesDialog
          codes={dialogCodes}
          onContinue={async () => setDialogCodes(null)}
        />
      )}
    </div>
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
 * Changing the password is one step, so the row opens it in a dialog; the
 * toast says it is done once the dialog closes.
 */
function ChangePasswordRow() {
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
      title={<Trans id="security.password.change.title">Change password</Trans>}
      details={[
        <Trans key="note" id="security.password.change.note">
          Choose a new password for signing in.
        </Trans>,
      ]}
      actions={
        <Button size="sm" variant="secondary" onPress={() => setOpen(true)}>
          <Trans id="security.password.change.open">Change</Trans>
        </Button>
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
                              : msg({
                                  id: "security.password.mismatch",
                                  message: "The two passwords do not match.",
                                }),
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
 * Replacing the authenticator needs a new secret scanned and a code from it,
 * so the dialog carries the whole setup. The codes it returns go up to the
 * panel, which shows them once this dialog has closed.
 */
function ReplaceTotpRow({ onCodes }: { onCodes: (codes: string[]) => void }) {
  const [open, setOpen] = useState(false);
  return (
    <ItemListRow
      title={
        <Trans id="security.totp.replace.title">Replace authenticator</Trans>
      }
      details={[
        <Trans key="note" id="security.totp.replace.note">
          Your current authenticator and every existing recovery code stop
          working as soon as this succeeds.
        </Trans>,
      ]}
      actions={
        <Button size="sm" variant="secondary" onPress={() => setOpen(true)}>
          <Trans id="security.totp.replace.open">Replace</Trans>
        </Button>
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
 * The one section that can establish both factors. The title states which half
 * is missing, but both cases submit the same atomic endpoint.
 */
function SetupForm({
  passwordSet,
  onCodes,
}: {
  passwordSet: boolean;
  onCodes: (codes: string[]) => void;
}) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const config = useQuery(publicConfigQueryOptions());
  const setup = useTotpSetup();
  const submit = useMutation(passwordTotpMutationOptions(queryClient));

  const form = useAppForm({
    defaultValues: { password: "", code: "" },
    onSubmit: async ({ value }) => {
      if (!setup.secret) return;
      try {
        const result = await submit.mutateAsync({
          password: value.password,
          secret_base32: setup.secret,
          code: value.code,
        });
        onCodes(result.recovery_codes);
      } catch (error) {
        applyServerError(form, error, {
          locations: { password: "password", code: "code" },
          codes: {},
        });
      }
    },
  });

  return (
    <form.AppForm>
      <form.Form
        label={t({
          id: "security.setup.form",
          message: "Set up a password and authenticator",
        })}
      >
        <form.FormError />
        <form.AppField
          name="password"
          validators={{ onChange: ({ value }) => checkPassword(value) }}
        >
          {(field) => (
            <field.FormField
              label={
                passwordSet ? (
                  <Trans id="security.setup.new_password">New password</Trans>
                ) : (
                  <Trans id="security.setup.password">Choose a password</Trans>
                )
              }
              type="password"
              autoComplete="new-password"
              variant="secondary"
            />
          )}
        </form.AppField>
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
        <Description>
          <Trans id="security.setup.note">
            Signing in will then ask for your password and a code from this
            authenticator. Anything you used before that stops working.
          </Trans>
        </Description>
        <form.SubmitButton>
          <Trans id="security.setup.action">Turn on both</Trans>
        </form.SubmitButton>
      </form.Form>
    </form.AppForm>
  );
}

/**
 * New codes void every existing one, including any the user printed or saved,
 * so the request waits for a confirmation. The codes it returns still open in
 * the panel's dialog.
 */
function RecoveryCodesRow({ onCodes }: { onCodes: (codes: string[]) => void }) {
  const queryClient = useQueryClient();
  const regenerate = useMutation(
    regenerateRecoveryCodesMutationOptions(queryClient),
  );
  const [confirming, setConfirming] = useState(false);

  return (
    <ItemListRow
      title={<Trans id="security.recovery.title">Recovery codes</Trans>}
      details={[
        <Trans key="note" id="security.recovery.note">
          Use a recovery code to sign in when you cannot use your password or
          authenticator. Generating new ones replaces every existing code.
        </Trans>,
      ]}
      actions={
        <Button
          size="sm"
          variant="secondary"
          isPending={regenerate.isPending}
          onPress={() => setConfirming(true)}
        >
          <Trans id="security.recovery.generate">
            Generate new recovery codes
          </Trans>
        </Button>
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

function RevokeRow({ passkeyCount }: { passkeyCount: number }) {
  const queryClient = useQueryClient();
  const revoke = useMutation(revokePasswordTotpMutationOptions(queryClient));
  const [confirming, setConfirming] = useState(false);
  // The server refuses this when it would remove the last way in, which is
  // exactly the case where there is no passkey to fall back on.
  const lastWay = passkeyCount === 0;

  return (
    <ItemListRow
      title={
        <Trans id="security.revoke.title">
          Turn off password and authenticator
        </Trans>
      }
      details={[
        lastWay ? (
          <Trans key="note" id="security.revoke.last">
            This is your only way to sign in, so it cannot be turned off. Add a
            passkey first.
          </Trans>
        ) : (
          <Trans key="note" id="security.revoke.note">
            Removes your password and authenticator from this account. Your
            passkeys keep working.
          </Trans>
        ),
      ]}
      actions={
        <Button
          size="sm"
          variant="danger-soft"
          isDisabled={lastWay}
          isPending={revoke.isPending}
          onPress={() => setConfirming(true)}
        >
          <Trans id="security.revoke.action">Turn off</Trans>
        </Button>
      }
    >
      <RevokeConfirm
        open={confirming}
        pending={revoke.isPending}
        onClose={() => setConfirming(false)}
        onConfirm={() =>
          revoke.mutate(undefined, { onSettled: () => setConfirming(false) })
        }
      />
    </ItemListRow>
  );
}

function RevokeConfirm({
  open,
  pending,
  onClose,
  onConfirm,
}: {
  open: boolean;
  pending: boolean;
  onClose: () => void;
  onConfirm: () => void;
}) {
  return (
    <ConfirmDialog
      isOpen={open}
      onOpenChange={(next) => !next && onClose()}
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
      confirmLabel={<Trans id="security.revoke.confirm.action">Turn off</Trans>}
      isPending={pending}
      onConfirm={onConfirm}
    />
  );
}

/**
 * The locally generated authenticator secret and its otpauth URI.
 *
 * The secret never leaves the browser except as `secret_base32` on submit, and
 * it is rotated on request so abandoning a half-filled card does not leave a
 * stale secret sitting in state.
 */
function useTotpSetup() {
  const config = useQuery(publicConfigQueryOptions());
  const session = useQuery({
    ...sessionQueryOptions(),
    refetchOnMount: false,
  });
  const [secret, setSecret] = useState(() => generateTotpSecret());
  const username = session.data?.username;
  const totp = config.data?.totp;
  // buildTotpUri re-validates every field and throws on a malformed config, so
  // the two queries must both have landed before it is called.
  const uri =
    username !== undefined && totp !== undefined
      ? buildTotpUri(secret, username, totp)
      : "";
  return { secret, uri, rotate: () => setSecret(generateTotpSecret()) };
}
