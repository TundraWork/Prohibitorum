import {
  Button,
  Label,
  Modal,
  Radio,
  RadioGroup,
  Skeleton,
  useOverlayState,
} from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAtomValue, useSetAtom, useStore } from "jotai";
import { ShieldCheck } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { describeError, isCancellation } from "@/api/errors";
import {
  completeSudoWithPasskey,
  completeSudoWithPasswordTotp,
} from "@/api/mutations";
import type { SudoMethod } from "@/api/raw-paths";
import type { SudoRequest } from "@/api/sudo";
import {
  configureSudo,
  resetSudo,
  SudoCancelled,
  sudoDialogAtom,
  sudoFreshAtom,
  sudoMethodsQueryOptions,
} from "@/api/sudo";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

const noFields = { locations: {}, codes: {} };

/**
 * The console-wide step-up prompt.
 *
 * Mounted once on the console layout and driven by `sudoDialogAtom`:
 * `runWithSudo` parks the operation the backend refused here, and this dialog
 * settles that promise with the replayed result — or with `SudoCancelled` when
 * the user backs out.
 *
 * The wiring contract is installed here as well. `runWithSudo` is a plain
 * function rather than a hook, so it needs its dependencies handed over once,
 * from a component that is mounted whenever any console page can call it.
 */
export function SudoDialog() {
  const queryClient = useQueryClient();
  const store = useStore();
  const setDialog = useSetAtom(sudoDialogAtom);
  const setFresh = useSetAtom(sudoFreshAtom);
  const request = useAtomValue(sudoDialogAtom);
  const state = useOverlayState();

  useEffect(() => {
    configureSudo({
      queryClient,
      set: (next) => setDialog(next),
      setFresh: (value) => setFresh(value),
      getFresh: () => store.get(sudoFreshAtom),
    });
    return () => resetSudo();
  }, [queryClient, setDialog, setFresh, store]);

  const open = request !== null;
  const { open: openOverlay, close: closeOverlay } = state;
  useEffect(() => {
    if (open) openOverlay();
    else closeOverlay();
  }, [open, openOverlay, closeOverlay]);

  function dismiss() {
    const pending = request;
    setDialog(null);
    pending?.reject(new SudoCancelled());
  }

  function verified(result: unknown) {
    const pending = request;
    setFresh(true);
    setDialog(null);
    pending?.resolve(result);
  }

  return (
    <Modal state={state}>
      <Modal.Backdrop isDismissable={false}>
        <Modal.Container placement="center" size="md">
          <Modal.Dialog>
            {request && (
              <SudoStep
                request={request}
                onDismiss={dismiss}
                onVerified={verified}
              />
            )}
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}

/**
 * One verification attempt. Keyed on the request so a second interception never
 * inherits the first one's typed-in password or selected method.
 */
function SudoStep({
  request,
  onDismiss,
  onVerified,
}: {
  request: SudoRequest;
  onDismiss: () => void;
  onVerified: (result: unknown) => void;
}) {
  const { t } = useLingui();
  const methods = useQuery(sudoMethodsQueryOptions());
  const [choice, setChoice] = useState<SudoMethod | null>(null);
  const [failure, setFailure] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const settled = useRef(false);

  const available = methods.data?.methods ?? [];
  const selected = choice ?? available[0] ?? null;

  async function passkey() {
    if (busy || settled.current) return;
    setBusy(true);
    setFailure(null);
    try {
      await completeSudoWithPasskey();
      settled.current = true;
      onVerified(await request.perform());
    } catch (error) {
      // The operation itself can fail here too, and it is the more useful
      // message: the caller's own error mapping never ran.
      setFailure(error);
      if (!settled.current) {
        // A second begin is required after an expired ceremony, so the method
        // list is re-read rather than the old options being reused.
        void methods.refetch();
      }
    } finally {
      setBusy(false);
    }
  }

  const form = useAppForm({
    defaultValues: { password: "", totp_code: "" },
    onSubmit: async ({ value }) => {
      if (settled.current) return;
      try {
        await completeSudoWithPasswordTotp({
          current_password: value.password,
          totp_code: value.totp_code,
        });
        settled.current = true;
        onVerified(await request.perform());
      } catch (error) {
        // A retry after a failure re-runs the operation, not just the form.
        settled.current = false;
        applyServerError(form, error, noFields);
        throw error;
      }
    },
  });

  const message = methods.isPending ? null : describeError(methods.error);

  // A passkey is verified straight from the footer, so only the password path
  // needs the fields wrapped in a form element.
  const usesForm = available.length > 0 && selected !== "webauthn";

  const cancel = (
    <Button variant="secondary" onPress={onDismiss} isDisabled={busy}>
      <Trans id="sudo.cancel">Cancel</Trans>
    </Button>
  );

  const content = (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-muted">
        {request.reason ? (
          t(request.reason)
        ) : (
          <Trans id="sudo.intro">
            For your security, verify your identity again to continue.
          </Trans>
        )}
      </p>

      {methods.isPending && <Skeleton className="h-24 rounded-lg" />}

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
                This account has no way to confirm your identity from here. Set
                up a passkey, or a password and authenticator, before trying
                again.
              </Trans>
            </SurfaceAlert.Title>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      )}

      {available.length > 0 && (
        <>
          {available.length > 1 && (
            <RadioGroup
              aria-label={t({
                id: "sudo.methods",
                message: "Verification method",
              })}
              value={selected}
              onChange={(value) => setChoice(value as SudoMethod)}
              isDisabled={busy}
            >
              <Label>
                <Trans id="sudo.method.label">Method</Trans>
              </Label>
              {available.map((method) => (
                <Radio key={method} value={method}>
                  <Radio.Content>
                    <Radio.Control>
                      <Radio.Indicator />
                    </Radio.Control>
                    {method === "webauthn" ? (
                      <Trans id="sudo.method.passkey">Use a passkey</Trans>
                    ) : (
                      <Trans id="sudo.method.password">
                        Use your password and authenticator
                      </Trans>
                    )}
                  </Radio.Content>
                </Radio>
              ))}
            </RadioGroup>
          )}

          {failure !== null && !isCancellation(failure) && (
            <SurfaceAlert status="danger" role="alert">
              <SurfaceAlert.Indicator />
              <SurfaceAlert.Content>
                <SurfaceAlert.Title>
                  {t(describeError(failure))}
                </SurfaceAlert.Title>
              </SurfaceAlert.Content>
            </SurfaceAlert>
          )}

          {usesForm && (
            <>
              <form.FormError />
              <form.AppField name="password">
                {(field) => (
                  <field.FormField
                    label={<Trans id="sudo.password">Current password</Trans>}
                    type="password"
                    autoComplete="current-password"
                    variant="secondary"
                  />
                )}
              </form.AppField>
              <form.AppField name="totp_code">
                {(field) => (
                  <field.OtpField
                    label={<Trans id="sudo.code">Authenticator code</Trans>}
                    digits={6}
                    variant="secondary"
                  />
                )}
              </form.AppField>
            </>
          )}
        </>
      )}
    </div>
  );

  return (
    <>
      <Modal.Header>
        <Modal.Icon className="bg-default text-foreground">
          <ShieldCheck size={20} strokeWidth={1.75} aria-hidden="true" />
        </Modal.Icon>
        <Modal.Heading>
          <Trans id="sudo.title">Confirm it is you</Trans>
        </Modal.Heading>
      </Modal.Header>
      {usesForm ? (
        <form.AppForm>
          <form.Form
            className="flex min-h-0 flex-1 flex-col"
            label={t({
              id: "sudo.form.label",
              message: "Identity verification",
            })}
          >
            <Modal.Body>{content}</Modal.Body>
            <Modal.Footer>
              {cancel}
              <form.SubmitButton>
                <Trans id="sudo.submit">Verify and continue</Trans>
              </form.SubmitButton>
            </Modal.Footer>
          </form.Form>
        </form.AppForm>
      ) : (
        <>
          <Modal.Body>{content}</Modal.Body>
          <Modal.Footer>
            {cancel}
            {available.length > 0 && (
              <Button
                isPending={busy}
                isDisabled={busy}
                onPress={() => {
                  void passkey();
                }}
              >
                <Trans id="sudo.use-passkey">Verify with a passkey</Trans>
              </Button>
            )}
          </Modal.Footer>
        </>
      )}
    </>
  );
}
