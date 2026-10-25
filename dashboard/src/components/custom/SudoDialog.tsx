import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { browserSupportsWebAuthn } from "@simplewebauthn/browser";
import { useStore } from "@tanstack/react-form";
import {
  useQuery,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { useAtomValue, useStore as useJotaiStore, useSetAtom } from "jotai";
import { Fingerprint, ShieldCheck } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { isValidLoginPassword, isValidTotpCode } from "@/api/auth";
import { describeError, isCancellation } from "@/api/errors";
import {
  completeSudoWithPasskey,
  completeSudoWithPasswordTotp,
} from "@/api/mutations";
import { publicConfigQueryOptions } from "@/api/queries";
import type { SudoRequest } from "@/api/sudo";
import {
  configureSudo,
  resetSudo,
  SudoCancelled,
  sudoDialogAtom,
  sudoFreshAtom,
  sudoMethodsQueryOptions,
} from "@/api/sudo";
import { Button } from "@/components/custom/Button";
import { OrSeparator } from "@/components/custom/OrSeparator";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import {
  Dialog,
  DialogDescription,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { applyServerError, clearServerErrors } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

const noFields = { locations: {}, codes: {} };

const passwordInvalid = msg({
  id: "sudo.password.invalid",
  message: "Enter your current password, up to 1024 UTF-8 bytes.",
});
const codeInvalid = msg({
  id: "sudo.code.invalid",
  message: "Enter the code shown in your authenticator, using digits 0–9 only.",
});

/**
 * The console-wide step-up prompt.
 *
 * Mounted once on the console layout and driven by `sudoDialogAtom`:
 * `runWithSudo` parks the operation the backend refused here, and this dialog
 * verifies the user and replays that operation once. The replay's own result
 * settles the promise either way — a failed replay closes the dialog and
 * reaches the caller, whose error mapping handles it just as it would had the
 * window already been open. Closing the dialog settles it with `SudoCancelled`.
 *
 * While a verification or the replay is in flight the dialog cannot be closed:
 * once the replay is sent the write is under way, and "nothing was changed"
 * would no longer be true.
 *
 * The wiring contract is installed here as well. `runWithSudo` is a plain
 * function rather than a hook, so it needs its dependencies handed over once,
 * from a component that is mounted whenever any console page can call it.
 */
export function SudoDialog() {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const store = useJotaiStore();
  const setDialog = useSetAtom(sudoDialogAtom);
  const setFresh = useSetAtom(sudoFreshAtom);
  const request = useAtomValue(sudoDialogAtom);
  const [locked, setLocked] = useState(false);

  useEffect(() => {
    configureSudo({
      queryClient,
      set: (next) => setDialog(next),
      setFresh: (value) => setFresh(value),
      getFresh: () => store.get(sudoFreshAtom),
    });
    return () => resetSudo();
  }, [queryClient, setDialog, setFresh, store]);

  function dismiss() {
    const pending = request;
    setDialog(null);
    pending?.reject(new SudoCancelled());
  }

  // Both outcomes of a replay follow a verification that succeeded, so the
  // window is open either way.
  function verified(result: unknown) {
    const pending = request;
    setFresh(true);
    setDialog(null);
    pending?.resolve(result);
  }

  function failed(error: unknown) {
    const pending = request;
    setFresh(true);
    setDialog(null);
    pending?.reject(error);
  }

  return (
    // Open while a request is parked. Closing it any way the dialog allows —
    // the close button, or Escape — settles the request as cancelled, so the
    // operation that asked never waits on a prompt that is gone.
    <Dialog
      open={request !== null}
      disablePointerDismissal
      onOpenChange={(isOpen) => {
        if (!isOpen && !locked) dismiss();
      }}
    >
      <DialogPopup
        className="sm:max-w-md"
        closeProps={{
          disabled: locked,
          "aria-label": t({ id: "sudo.close", message: "Close" }),
        }}
      >
        {request && (
          <SudoStep
            request={request}
            onLockChange={setLocked}
            onVerified={verified}
            onFailed={failed}
          />
        )}
      </DialogPopup>
    </Dialog>
  );
}

/**
 * One verification attempt, for as long as its request is parked. The two
 * methods sit side by side, each with its own control: a passkey in one press,
 * or the password and authenticator form. Either one finishes the step.
 */
function SudoStep({
  request,
  onLockChange,
  onVerified,
  onFailed,
}: {
  request: SudoRequest;
  onLockChange: (locked: boolean) => void;
  onVerified: (result: unknown) => void;
  onFailed: (error: unknown) => void;
}) {
  const { t } = useLingui();
  const methods = useQuery(sudoMethodsQueryOptions());
  const { data: config } = useSuspenseQuery(publicConfigQueryOptions());
  const [failure, setFailure] = useState<unknown>(null);
  const [passkeyBusy, setPasskeyBusy] = useState(false);
  const settled = useRef(false);

  const available = methods.data?.methods ?? [];
  const hasPasskey = available.includes("webauthn");
  const hasPassword = available.includes("password_totp");
  const passkeySupported = window.isSecureContext && browserSupportsWebAuthn();

  /** Replays the parked operation, once, after a verification succeeded. */
  async function replay() {
    settled.current = true;
    let result: unknown;
    try {
      result = await request.perform();
    } catch (error) {
      onFailed(error);
      return;
    }
    onVerified(result);
  }

  const form = useAppForm({
    defaultValues: { password: "", totp_code: "" },
    onSubmit: async ({ value }) => {
      if (settled.current) return;
      setFailure(null);
      try {
        await completeSudoWithPasswordTotp({
          current_password: value.password,
          totp_code: value.totp_code,
        });
      } catch (error) {
        applyServerError(form, error, noFields);
        return;
      }
      await replay();
    },
  });
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const busy = passkeyBusy || submitting;

  useEffect(() => {
    onLockChange(busy);
  }, [busy, onLockChange]);
  // The lock belongs to this attempt; a closed dialog leaves none behind.
  useEffect(() => () => onLockChange(false), [onLockChange]);

  async function passkey() {
    if (busy || settled.current) return;
    setPasskeyBusy(true);
    setFailure(null);
    clearServerErrors(form);
    try {
      await completeSudoWithPasskey();
    } catch (error) {
      setFailure(error);
      // A second begin is required after an expired ceremony, so the method
      // list is re-read rather than the old options being reused.
      void methods.refetch();
      setPasskeyBusy(false);
      return;
    }
    await replay();
  }

  const message = methods.isPending ? null : describeError(methods.error);
  // One method leads: the passkey when this browser can use one, the password
  // otherwise. It takes the primary style and the focus the step opens on, so
  // the dialog never shows two primary buttons or a primary one disabled.
  const passkeyLeads = hasPasskey && passkeySupported;

  const passkeyButton = (
    <Button
      autoFocus={passkeyLeads}
      pending={passkeyBusy}
      disabled={!passkeySupported || busy}
      onClick={() => {
        void passkey();
      }}
      variant={passkeyLeads ? undefined : "outline"}
      className="w-full"
    >
      <Fingerprint size={16} aria-hidden="true" />
      <Trans id="sudo.use-passkey">Verify with a passkey</Trans>
    </Button>
  );

  const content = (
    <div className="flex flex-col gap-4">
      {methods.isPending && <MethodsSkeleton />}

      {methods.isError && message && (
        <SurfaceAlert status="danger" role="alert">
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>{t(message)}</SurfaceAlert.Title>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      )}

      {methods.isSuccess && available.length === 0 && (
        <SurfaceAlert status="warning">
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>
              <Trans id="sudo.no-methods">
                This account has no way to verify your identity. Set up a
                passkey, or a password and authenticator, first.
              </Trans>
            </SurfaceAlert.Title>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      )}

      {failure !== null && !isCancellation(failure) && (
        <SurfaceAlert status="danger" role="alert">
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>{t(describeError(failure))}</SurfaceAlert.Title>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      )}
      {hasPassword && <form.FormError />}

      {hasPasskey &&
        (passkeySupported ? (
          passkeyButton
        ) : (
          // A disabled button emits no hover or focus for a tooltip to answer,
          // so the tooltip listens on the trigger wrapper instead.
          <Tooltip>
            <TooltipTrigger delay={0} render={<span className="w-full" />}>
              {passkeyButton}
            </TooltipTrigger>
            <TooltipPopup>
              <Trans id="sudo.passkey.unsupported">
                This browser or connection cannot use passkeys.
              </Trans>
            </TooltipPopup>
          </Tooltip>
        ))}

      {hasPasskey && hasPassword && <OrSeparator />}

      {hasPassword && (
        <>
          <form.AppField
            name="password"
            validators={{
              onBlur: ({ value }) =>
                isValidLoginPassword(value) ? undefined : passwordInvalid,
            }}
          >
            {(field) => (
              <field.FormField
                label={<Trans id="sudo.password">Current password</Trans>}
                type="password"
                autoComplete="current-password"
                autoFocus={!passkeyLeads}
                isDisabled={passkeyBusy}
                variant="secondary"
              />
            )}
          </form.AppField>
          <form.AppField
            name="totp_code"
            validators={{
              onBlur: ({ value }) =>
                isValidTotpCode(value, config.totp.digits)
                  ? undefined
                  : codeInvalid,
            }}
          >
            {(field) => (
              <field.OtpField
                label={<Trans id="sudo.code">Authenticator code</Trans>}
                digits={config.totp.digits}
                isDisabled={passkeyBusy}
                variant="secondary"
              />
            )}
          </form.AppField>
          <form.SubmitButton
            fullWidth
            disabled={passkeyBusy}
            variant={passkeyLeads ? "outline" : "default"}
          >
            <Trans id="sudo.submit">Verify and continue</Trans>
          </form.SubmitButton>
        </>
      )}
    </div>
  );

  return (
    <>
      <DialogHeader>
        <div className="flex items-center gap-3">
          <span className="flex size-10 shrink-0 items-center justify-center rounded-full bg-muted text-foreground">
            <ShieldCheck size={20} strokeWidth={1.75} aria-hidden="true" />
          </span>
          <DialogTitle>
            <Trans id="sudo.title">Verify your identity</Trans>
          </DialogTitle>
        </div>
        <DialogDescription>
          {request.reason ? (
            t(request.reason)
          ) : (
            <Trans id="sudo.intro">
              Verify your identity again to continue.
            </Trans>
          )}
        </DialogDescription>
      </DialogHeader>
      {hasPassword ? (
        // The form spans the whole body, passkey included, so Enter submits
        // the password path and a failed submit can focus its summary.
        <form.AppForm>
          <form.Form
            className="flex min-h-0 flex-1 flex-col"
            label={t({
              id: "sudo.form.label",
              message: "Identity verification",
            })}
          >
            <DialogPanel>{content}</DialogPanel>
          </form.Form>
        </form.AppForm>
      ) : (
        <DialogPanel>{content}</DialogPanel>
      )}
    </>
  );
}

/**
 * The step's shape while the method list is re-read: the passkey button, the
 * rule between the methods, the two fields and the submit button.
 */
function MethodsSkeleton() {
  return (
    <div aria-hidden="true" className="flex flex-col gap-4">
      <Skeleton className="h-10 w-full" />
      <Skeleton className="h-px w-full" />
      <Skeleton className="h-16 w-full" />
      <Skeleton className="h-16 w-full" />
      <Skeleton className="h-10 w-full" />
    </div>
  );
}
