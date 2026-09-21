import { AlertDialog, Button, Card, Description, Spinner } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
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
import { RecoveryCodes } from "@/components/custom/RecoveryCodes";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
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
 * There is no separate "current state" card: the state of each factor decides
 * which card appears and what its button says.
 *
 * The single-card shape is not a layout preference. The backend offers one
 * atomic call that establishes both factors together and replaces the recovery
 * codes as a batch, and a password set on its own would leave the account
 * without a second factor — so there is no interface here that sets only the
 * missing half.
 *
 * The recovery codes a write returns are held here rather than inside the card
 * that produced them. Every one of those writes invalidates the factors query,
 * which re-renders this panel and unmounts that card the moment it succeeds —
 * so a card-local copy would be thrown away before the user could read it, and
 * those codes are the only time the server will ever show them.
 */
export function PasswordTotpPanel() {
  const { t } = useLingui();
  const factors = useQuery(factorsQueryOptions());
  const [codes, setCodes] = useState<string[] | null>(null);
  const [rotate, setRotate] = useState(0);

  if (codes !== null) {
    return (
      <RecoveryCodes
        codes={codes}
        onContinue={async () => {
          setCodes(null);
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
      <SurfaceAlert status="danger" role="alert">
        <SurfaceAlert.Indicator />
        <SurfaceAlert.Content>
          <SurfaceAlert.Title>
            {t(describeError(factors.error))}
          </SurfaceAlert.Title>
        </SurfaceAlert.Content>
      </SurfaceAlert>
    );
  }

  const { passwordSet, totpEnrolled, passkeyCount } = factors.data;
  const rotateKey = (count: number) => `${count}`;

  return (
    <div className="flex flex-col gap-6">
      {passwordSet && totpEnrolled ? (
        // One column at every width. The form inside each card keeps its own
        // measure, so a password field does not span the console column.
        <div className="grid gap-6 [&_[data-slot=card]_form]:max-w-lg">
          <ChangePasswordCard />
          <ReplaceTotpCard onCodes={setCodes} />
        </div>
      ) : (
        <SetupCard
          key={rotateKey(rotate)}
          passwordSet={passwordSet}
          onCodes={setCodes}
        />
      )}
      <RecoveryCodesCard enrolled={totpEnrolled} onCodes={setCodes} />
      <RevokeCard passkeyCount={passkeyCount} />
    </div>
  );
}

function ChangePasswordCard() {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const [saved, setSaved] = useState(false);
  const change = useMutation(setPasswordMutationOptions(queryClient));

  const form = useAppForm({
    defaultValues: { password: "", confirm: "" },
    onSubmit: async ({ value }) => {
      setSaved(false);
      try {
        await change.mutateAsync(value.password);
        setSaved(true);
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
    <Card>
      <Card.Header>
        <Card.Title render={(props) => <h2 {...props} />}>
          <Trans id="security.password.change.title">Change password</Trans>
        </Card.Title>
      </Card.Header>
      <Card.Content>
        <form.AppForm>
          <form.Form
            label={t({
              id: "security.password.change.form",
              message: "Change password",
            })}
          >
            <form.FormError />
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
                />
              )}
            </form.AppField>
            {saved && (
              <p role="status" className="text-sm">
                <Trans id="security.password.changed">
                  Your password is updated. Which sudo prompt you see next may
                  change.
                </Trans>
              </p>
            )}
            <form.SubmitButton>
              <Trans id="security.password.change.action">
                Change password
              </Trans>
            </form.SubmitButton>
          </form.Form>
        </form.AppForm>
      </Card.Content>
    </Card>
  );
}

function ReplaceTotpCard({ onCodes }: { onCodes: (codes: string[]) => void }) {
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
    <Card>
      <Card.Header>
        <Card.Title render={(props) => <h2 {...props} />}>
          <Trans id="security.totp.replace.title">Replace authenticator</Trans>
        </Card.Title>
      </Card.Header>
      <Card.Content>
        <form.AppForm>
          <form.Form
            label={t({
              id: "security.totp.replace.form",
              message: "Replace authenticator",
            })}
          >
            <form.FormError />
            <TotpSetup secret={setup.secret} uri={setup.uri} />
            <Description>
              <Trans id="security.totp.replace.note">
                Your current authenticator and every existing recovery code stop
                working as soon as this succeeds.
              </Trans>
            </Description>
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
                />
              )}
            </form.AppField>
            <form.SubmitButton>
              <Trans id="security.totp.replace.action">
                Replace authenticator
              </Trans>
            </form.SubmitButton>
          </form.Form>
        </form.AppForm>
      </Card.Content>
    </Card>
  );
}

/**
 * The one card that can establish both factors. The title states which half is
 * missing, but both cases submit the same atomic endpoint.
 */
function SetupCard({
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
    <Card>
      <Card.Header>
        <Card.Title render={(props) => <h2 {...props} />}>
          {passwordSet ? (
            <Trans id="security.setup.title.totp">
              Set up an authenticator with a new password
            </Trans>
          ) : (
            <Trans id="security.setup.title.both">
              Set up a password and authenticator
            </Trans>
          )}
        </Card.Title>
      </Card.Header>
      <Card.Content>
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
                      <Trans id="security.setup.new_password">
                        New password
                      </Trans>
                    ) : (
                      <Trans id="security.setup.password">
                        Choose a password
                      </Trans>
                    )
                  }
                  type="password"
                  autoComplete="new-password"
                />
              )}
            </form.AppField>
            <TotpSetup secret={setup.secret} uri={setup.uri} />
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
      </Card.Content>
    </Card>
  );
}

function RecoveryCodesCard({
  enrolled,
  onCodes,
}: {
  enrolled: boolean;
  onCodes: (codes: string[]) => void;
}) {
  const queryClient = useQueryClient();
  const regenerate = useMutation(
    regenerateRecoveryCodesMutationOptions(queryClient),
  );

  if (!enrolled) return null;

  return (
    <Card>
      <Card.Header>
        <Card.Title render={(props) => <h2 {...props} />}>
          <Trans id="security.recovery.title">Recovery codes</Trans>
        </Card.Title>
      </Card.Header>
      <Card.Content className="flex flex-col gap-4">
        <Description>
          <Trans id="security.recovery.note">
            Use a recovery code to sign in when you cannot use your password or
            authenticator. Generating new ones replaces every existing code.
          </Trans>
        </Description>
        <div>
          <Button
            variant="secondary"
            isPending={regenerate.isPending}
            onPress={() => {
              regenerate.mutate(undefined, {
                onSuccess: (result) => onCodes(result.recovery_codes),
              });
            }}
          >
            <Trans id="security.recovery.generate">
              Generate new recovery codes
            </Trans>
          </Button>
        </div>
      </Card.Content>
    </Card>
  );
}

function RevokeCard({ passkeyCount }: { passkeyCount: number }) {
  const queryClient = useQueryClient();
  const revoke = useMutation(revokePasswordTotpMutationOptions(queryClient));
  const [confirming, setConfirming] = useState(false);
  // The server refuses this when it would remove the last way in, which is
  // exactly the case where there is no passkey to fall back on.
  const lastWay = passkeyCount === 0;

  return (
    <Card>
      <Card.Header>
        <Card.Title render={(props) => <h2 {...props} />}>
          <Trans id="security.revoke.title">
            Turn off password and authenticator
          </Trans>
        </Card.Title>
      </Card.Header>
      <Card.Content className="flex flex-col gap-4">
        <Description>
          {lastWay ? (
            <Trans id="security.revoke.last">
              This is your only way to sign in, so it cannot be turned off. Add
              a passkey first.
            </Trans>
          ) : (
            <Trans id="security.revoke.note">
              Removes your password and authenticator from this account. Your
              passkeys keep working.
            </Trans>
          )}
        </Description>
        <div>
          <Button
            variant="danger-soft"
            isDisabled={lastWay}
            isPending={revoke.isPending}
            onPress={() => setConfirming(true)}
          >
            <Trans id="security.revoke.action">Turn off</Trans>
          </Button>
        </div>
      </Card.Content>
      <RevokeConfirm
        open={confirming}
        pending={revoke.isPending}
        onClose={() => setConfirming(false)}
        onConfirm={() =>
          revoke.mutate(undefined, { onSettled: () => setConfirming(false) })
        }
      />
    </Card>
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
    <AlertDialog isOpen={open} onOpenChange={(next) => !next && onClose()}>
      <AlertDialog.Backdrop>
        <AlertDialog.Container placement="center" size="md">
          <AlertDialog.Dialog>
            <AlertDialog.Header>
              <AlertDialog.Heading>
                <Trans id="security.revoke.confirm.title">
                  Turn off password and authenticator?
                </Trans>
              </AlertDialog.Heading>
            </AlertDialog.Header>
            <AlertDialog.Body>
              <p>
                <Trans id="security.revoke.confirm.body">
                  Your password, your authenticator and all recovery codes stop
                  working immediately.
                </Trans>
              </p>
            </AlertDialog.Body>
            <AlertDialog.Footer>
              <Button
                variant="secondary"
                onPress={onClose}
                isDisabled={pending}
              >
                <Trans id="security.cancel">Cancel</Trans>
              </Button>
              <Button variant="danger" isPending={pending} onPress={onConfirm}>
                <Trans id="security.revoke.confirm.action">Turn off</Trans>
              </Button>
            </AlertDialog.Footer>
          </AlertDialog.Dialog>
        </AlertDialog.Container>
      </AlertDialog.Backdrop>
    </AlertDialog>
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
