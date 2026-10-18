import { Plural, Trans, useLingui } from "@lingui/react/macro";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import {
  LifeBuoy,
  Lock,
  PowerOff,
  RectangleEllipsis,
  Smartphone,
} from "lucide-react";
import { type ReactNode, useEffect, useId, useRef, useState } from "react";
import { describeError, isCancellation } from "@/api/errors";
import {
  passwordTotpMutationOptions,
  regenerateRecoveryCodesMutationOptions,
  replaceTotpMutationOptions,
  revokePasswordTotpMutationOptions,
  setPasswordMutationOptions,
} from "@/api/mutations";
import { factorsQueryOptions } from "@/api/queries";
import { withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { RecoveryCodes } from "@/components/custom/RecoveryCodes";
import { AsyncSection, Section } from "@/components/custom/Section";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { Badge } from "@/components/ui/badge";
import { ChangePasswordDialog } from "@/pages/security/ChangePasswordDialog";
import { ReplaceAuthenticatorDialog } from "@/pages/security/ReplaceAuthenticatorDialog";
import { SetupDialog, type SetupResult } from "@/pages/security/SetupDialog";

/** Codes this few or fewer are worth a mark on the row. */
const lowRecoveryCodes = 3;

const plainDescription = (
  <Trans id="security.password.description.both">
    For a device without your passkeys.
  </Trans>
);

/**
 * Password and authenticator.
 *
 * The heading, icon and plain description are the boundary's too, so they stay
 * put while the factors load and when the read fails.
 */
export function PasswordTotpPanel() {
  return (
    <AsyncSection
      resetKey="password-totp"
      icon={<Lock />}
      title={<PanelTitle />}
      description={plainDescription}
    >
      <PasswordTotpFactors />
    </AsyncSection>
  );
}

function PanelTitle() {
  return (
    <Trans id="security.section.password">Password and authenticator</Trans>
  );
}

/**
 * The list has a row per factor — the password, the authenticator, the
 * recovery codes — and each row says whether that factor is there. The two are
 * set up together and turned off together, and changed one at a time: the
 * backend's one call that establishes them writes the password, the
 * authenticator and a batch of recovery codes at once, and a password set on
 * its own would leave the account without a second factor. So while either is
 * missing the rows carry no actions, the missing one recedes, and the
 * section's heading carries the single way forward: setting up both, in a
 * dialog. Having only one of them is a fallback state rather than a flow; the
 * rows keep it legible, the setup repairs it and the last row turns it off.
 *
 * The writes and the recovery codes they return are held here rather than
 * inside a row or a dialog. Every write invalidates the factors query, which
 * re-renders this panel and may unmount the row or button that started it the
 * moment it succeeds — and the codes are the only time the server will ever
 * show them. The same goes for the setup dialog's open state: the button that
 * opens it is gone once both factors exist.
 */
function PasswordTotpFactors() {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const { data } = useSuspenseQuery(factorsQueryOptions());
  const { passwordSet, totpEnrolled, passkeyCount, recoveryCodesRemaining } =
    data;
  const bothSet = passwordSet && totpEnrolled;
  // The plural's placeholder takes its name from this variable.
  const count = recoveryCodesRemaining;

  const [error, setError] = useState<unknown>(null);
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
  const [changing, setChanging] = useState(false);
  const [replacing, setReplacing] = useState(false);
  const [regenerating, setRegenerating] = useState(false);
  const [revoking, setRevoking] = useState(false);
  const [focusChange, setFocusChange] = useState(false);
  const changeButton = useRef<HTMLButtonElement>(null);

  // Field-level refusals are drawn on the field inside the dialog that asked,
  // so those writes keep the default report for anything a field cannot show.
  const setup = useMutation(passwordTotpMutationOptions(queryClient));
  const change = useMutation(setPasswordMutationOptions(queryClient));
  const replace = useMutation(replaceTotpMutationOptions(queryClient));
  // These two are confirmed in a dialog that closes when they settle, so a
  // refusal is drawn in this section instead.
  const regenerate = useMutation(
    withInlineReport(regenerateRecoveryCodesMutationOptions(queryClient), true),
  );
  const revoke = useMutation(
    withInlineReport(revokePasswordTotpMutationOptions(queryClient), true),
  );

  // A finished setup removes the button that opened it, so focus goes to the
  // first action the new rows offer once they have arrived.
  useEffect(() => {
    if (!focusChange || !changeButton.current) return;
    changeButton.current.focus();
    setFocusChange(false);
  });

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

  // A refusal is shown and closes the question; backing out of the identity
  // check returns to it.
  const settle = (close: () => void) => (failure: unknown) => {
    if (isCancellation(failure)) return;
    setError(failure);
    close();
  };

  return (
    <Section
      icon={<Lock />}
      title={<PanelTitle />}
      description={
        bothSet ? (
          plainDescription
        ) : passwordSet || totpEnrolled ? (
          <Trans id="security.password.description.partial">
            Only part of this is set up. Set it up again to use it.
          </Trans>
        ) : (
          <Trans id="security.password.description.none">
            For a device without your passkeys. Not set up yet.
          </Trans>
        )
      }
      action={
        !bothSet && (
          <Button variant="outline" size="sm" onClick={openSetup}>
            <Trans id="security.setup.open">Set up</Trans>
          </Button>
        )
      }
    >
      {error !== null && (
        <SurfaceAlert status="danger" role="alert">
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>{t(describeError(error))}</SurfaceAlert.Title>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      )}
      <ItemList
        label={t({
          id: "security.password.list",
          message: "Password and authenticator",
        })}
        empty={null}
      >
        <FactorRow
          key="password"
          icon={<RectangleEllipsis />}
          title={<Trans id="security.factor.password">Password</Trans>}
          isSet={passwordSet}
          action={
            bothSet ? (
              <Button
                ref={changeButton}
                variant="outline"
                size="sm"
                onClick={() => setChanging(true)}
              >
                <Trans id="security.password.change.open">Change</Trans>
              </Button>
            ) : undefined
          }
        />
        <FactorRow
          key="authenticator"
          icon={<Smartphone />}
          title={
            <Trans id="security.factor.authenticator">Authenticator</Trans>
          }
          isSet={totpEnrolled}
          action={
            bothSet ? (
              <Button
                variant="outline"
                size="sm"
                onClick={() => setReplacing(true)}
              >
                <Trans id="security.totp.replace.open">Replace</Trans>
              </Button>
            ) : undefined
          }
        />
        {totpEnrolled && (
          <ItemListRow
            key="recovery-codes"
            icon={<LifeBuoy />}
            title={<Trans id="security.recovery.title">Recovery codes</Trans>}
            badge={
              recoveryCodesRemaining === 0 ? (
                <Badge variant="destructive">
                  <Trans id="security.recovery.used_up">All used</Trans>
                </Badge>
              ) : recoveryCodesRemaining <= lowRecoveryCodes ? (
                <Badge variant="warning">
                  <Trans id="security.recovery.low">Running low</Trans>
                </Badge>
              ) : undefined
            }
            meta={
              <Plural
                id="security.recovery.remaining"
                value={count}
                one="# code left"
                other="# codes left"
              />
            }
            actions={
              bothSet ? (
                <Button
                  variant="outline"
                  size="sm"
                  pending={regenerate.isPending}
                  onClick={() => setRegenerating(true)}
                >
                  <Trans id="security.recovery.regenerate">Regenerate</Trans>
                </Button>
              ) : undefined
            }
          />
        )}
        {(passwordSet || totpEnrolled) && (
          <TurnOffRow
            key="revoke"
            lastWay={passkeyCount === 0}
            pending={revoke.isPending}
            onTurnOff={() => setRevoking(true)}
          />
        )}
      </ItemList>

      <SetupDialog
        isOpen={setupOpen}
        attempt={setupAttempt}
        replacing={setupBefore.replacing}
        result={setupResult}
        submit={setup}
        onClose={() => setSetupOpen(false)}
        onResult={(codes) =>
          setSetupResult({ codes, firstIssue: setupBefore.firstIssue })
        }
        onDone={async () => {
          setSetupOpen(false);
          setFocusChange(true);
        }}
      />
      <ChangePasswordDialog
        open={changing}
        change={change}
        onClose={() => setChanging(false)}
      />
      <ReplaceAuthenticatorDialog
        open={replacing}
        replace={replace}
        onClose={() => setReplacing(false)}
        onCodes={setDialogCodes}
      />
      {dialogCodes !== null && (
        <RecoveryCodes
          dialog
          codes={dialogCodes}
          onContinue={async () => setDialogCodes(null)}
        />
      )}

      <ConfirmDialog
        isOpen={regenerating}
        onOpenChange={(open) => !open && setRegenerating(false)}
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
          setError(null);
          regenerate.mutate(undefined, {
            onSuccess: (result) => {
              setRegenerating(false);
              setDialogCodes(result.recovery_codes);
            },
            onError: settle(() => setRegenerating(false)),
          });
        }}
      />
      <ConfirmDialog
        isOpen={revoking}
        onOpenChange={(open) => !open && setRevoking(false)}
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
        onConfirm={() => {
          setError(null);
          revoke.mutate(undefined, {
            onSuccess: () => setRevoking(false),
            onError: settle(() => setRevoking(false)),
          });
        }}
      />
    </Section>
  );
}

/**
 * One factor the account may or may not have. A factor that is missing recedes
 * by colour and says "Not set" in words as well.
 */
function FactorRow({
  icon,
  title,
  isSet,
  action,
}: {
  icon: ReactNode;
  title: ReactNode;
  isSet: boolean;
  action?: ReactNode;
}) {
  return (
    <ItemListRow
      icon={icon}
      title={title}
      dimmed={!isSet}
      badge={
        isSet ? (
          <Badge variant="success">
            <Trans id="security.factor.set">Set</Trans>
          </Badge>
        ) : (
          <Badge variant="outline">
            <Trans id="security.factor.unset">Not set</Trans>
          </Badge>
        )
      }
      actions={action}
    />
  );
}

/**
 * Turning both off, for as long as either exists. The server refuses it when
 * it would remove the last way in, which is exactly the case where there is
 * no passkey to fall back on, so the button is disabled then and the row says
 * why in words rather than in a tooltip a keyboard or a touch screen never sees.
 */
function TurnOffRow({
  lastWay,
  pending,
  onTurnOff,
}: {
  lastWay: boolean;
  pending: boolean;
  onTurnOff: () => void;
}) {
  const reason = useId();
  return (
    <ItemListRow
      icon={<PowerOff className="text-destructive-foreground" />}
      title={
        <span className="text-destructive-foreground">
          <Trans id="security.revoke.title">
            Turn off password and authenticator
          </Trans>
        </span>
      }
      details={
        lastWay
          ? [
              <span key="reason" id={reason}>
                <Trans id="security.revoke.last_reason">
                  This is your only way to sign in. Add a passkey first.
                </Trans>
              </span>,
            ]
          : undefined
      }
      actions={
        <Button
          variant="destructive-outline"
          size="sm"
          disabled={lastWay}
          pending={pending}
          aria-describedby={lastWay ? reason : undefined}
          onClick={onTurnOff}
        >
          <Trans id="security.revoke.action">Turn off</Trans>
        </Button>
      }
    />
  );
}
