import { Trans, useLingui } from "@lingui/react/macro";
import { type UseMutationResult, useQuery } from "@tanstack/react-query";
import { useRef } from "react";
import { isValidTotpCode } from "@/api/auth";
import { isCancellation } from "@/api/errors";
import { publicConfigQueryOptions } from "@/api/queries";
import type { RecoveryCodesResult } from "@/api/raw-paths";
import { Button } from "@/components/custom/Button";
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
import { totpCodeInvalid } from "@/forms/password-totp-rules";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { useTotpSetup } from "@/pages/security/use-totp-setup";

type ReplaceTotp = UseMutationResult<
  RecoveryCodesResult,
  Error,
  { secret_base32: string; code: string }
>;

/**
 * Replacing the authenticator needs a new secret scanned and a code from it, so
 * the dialog carries the whole setup. The codes the write returns go to the
 * panel, which shows them once this dialog has closed.
 */
export function ReplaceAuthenticatorDialog({
  open,
  replace,
  onClose,
  onCodes,
}: {
  open: boolean;
  replace: ReplaceTotp;
  onClose: () => void;
  onCodes: (codes: string[]) => void;
}) {
  const popup = useRef<HTMLDivElement>(null);
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !next && !replace.isPending && onClose()}
    >
      <DialogPopup
        ref={popup}
        className="sm:max-w-xl"
        // The code is what the reader does next; the setup key is only read.
        initialFocus={() =>
          popup.current?.querySelector<HTMLElement>(
            'input[autocomplete="one-time-code"]',
          ) ?? true
        }
        portalProps={{ container: getTopLayerContainer() }}
      >
        <ReplaceForm replace={replace} onClose={onClose} onCodes={onCodes} />
      </DialogPopup>
    </Dialog>
  );
}

/** Mounted with the popup, so every opening draws a secret nobody has scanned yet. */
function ReplaceForm({
  replace,
  onClose,
  onCodes,
}: {
  replace: ReplaceTotp;
  onClose: () => void;
  onCodes: (codes: string[]) => void;
}) {
  const { t } = useLingui();
  const config = useQuery(publicConfigQueryOptions());
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
        onClose();
        onCodes(result.recovery_codes);
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, {
          locations: { code: "code" },
          codes: {},
        });
      }
    },
  });

  return (
    <form.AppForm>
      <DialogHeader>
        <DialogTitle>
          <Trans id="security.totp.replace.title">Replace authenticator</Trans>
        </DialogTitle>
      </DialogHeader>
      <form.Form
        label={t({
          id: "security.totp.replace.form",
          message: "Replace authenticator",
        })}
        className="flex min-h-0 flex-1 flex-col"
      >
        <DialogPanel className="flex flex-col gap-4">
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
          <TotpSetup secret={setup.secret} uri={setup.uri} split />
          <form.AppField
            name="code"
            validators={{
              onChange: ({ value }) =>
                config.data && isValidTotpCode(value, config.data.totp.digits)
                  ? undefined
                  : totpCodeInvalid,
            }}
          >
            {(field) => (
              <field.OtpField
                label={
                  <Trans id="security.totp.code">
                    Code from the new authenticator
                  </Trans>
                }
                digits={config.data?.totp.digits ?? 6}
                size="lg"
              />
            )}
          </form.AppField>
        </DialogPanel>
        <DialogFooter>
          <Button
            variant="ghost"
            disabled={replace.isPending}
            onClick={onClose}
          >
            <Trans id="security.cancel">Cancel</Trans>
          </Button>
          <form.SubmitButton fullWidth>
            <Trans id="security.totp.replace.action">
              Replace authenticator
            </Trans>
          </form.SubmitButton>
        </DialogFooter>
      </form.Form>
    </form.AppForm>
  );
}
