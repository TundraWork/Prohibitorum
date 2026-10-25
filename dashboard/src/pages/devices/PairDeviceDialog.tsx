import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import {
  type UseMutationResult,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import {
  approveDeviceMutationOptions,
  cancelDeviceMutationOptions,
  deviceLookupQueryOptions,
} from "@/api/mutations";
import { sessionsQueryOptions } from "@/api/queries";
import type { DevicePairing } from "@/api/raw-paths";
import { successMessage } from "@/api/success-messages";
import { quietQuery } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { Detail } from "@/components/custom/Detail";
import { DeviceName } from "@/components/custom/DeviceName";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import {
  Dialog,
  DialogFooter,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import {
  pairingCodeLength,
  pairingCodePattern,
} from "@/pages/devices/pairing-code";

const codeIncomplete = msg({
  id: "devices.code.incomplete",
  message: "Enter the full 8-character pairing code.",
});

type PairingRequest = { code: string; pairing: DevicePairing };

/**
 * Signing in a new device, from the device that is already signed in. The new
 * device shows a code and waits; this dialog takes the code, then shows what
 * the server knows about the device that asked and lets the reader approve or
 * decline it.
 *
 * The dialog's content is keyed by `attempt`, which the page changes on every
 * opening: the next opening starts at the code again, while a closing dialog
 * keeps the last request to draw on its way out. `initialCode` fills the code
 * in for an opening that arrived with one.
 */
export function PairDeviceDialog({
  isOpen,
  attempt,
  initialCode,
  onClose,
  onApproved,
}: {
  isOpen: boolean;
  attempt: number;
  initialCode?: string;
  onClose: () => void;
  onApproved: (pairing: DevicePairing) => void;
}) {
  const queryClient = useQueryClient();
  // The writes live here, with the dialog's owner, so a failure is still
  // reported once the step that started it is gone. The global toast reports
  // them; the dialog draws no error of its own.
  const approve = useMutation(approveDeviceMutationOptions(queryClient));
  const decline = useMutation(cancelDeviceMutationOptions(queryClient));
  const revoke = useMutation(
    cancelDeviceMutationOptions(
      queryClient,
      successMessage.cancelDevicePairing,
    ),
  );
  const busy = approve.isPending || decline.isPending || revoke.isPending;
  return (
    <Dialog
      open={isOpen}
      onOpenChange={(open) => {
        if (!open && !busy) onClose();
      }}
    >
      <DialogPopup className="sm:max-w-md">
        <PairingFlow
          key={attempt}
          initialCode={initialCode}
          writes={{ approve, decline, revoke, busy }}
          onClose={onClose}
          onApproved={onApproved}
        />
      </DialogPopup>
    </Dialog>
  );
}

type Writes = {
  approve: UseMutationResult<unknown, Error, string>;
  decline: UseMutationResult<unknown, Error, string>;
  revoke: UseMutationResult<unknown, Error, string>;
  busy: boolean;
};

function PairingFlow({
  initialCode,
  writes,
  onClose,
  onApproved,
}: {
  initialCode?: string;
  writes: Writes;
  onClose: () => void;
  onApproved: (pairing: DevicePairing) => void;
}) {
  const [request, setRequest] = useState<PairingRequest | null>(null);
  return request === null ? (
    <CodeStep
      initialCode={initialCode}
      onCancel={onClose}
      onFound={setRequest}
    />
  ) : (
    <ReviewStep
      request={request}
      writes={writes}
      onClose={onClose}
      onApproved={onApproved}
    />
  );
}

/**
 * The code the new device shows, in slots that take only the characters the
 * server's alphabet contains. It goes out as typed; the server owns the
 * format and ignores case. A code the page arrived with is filled in, and
 * looked up only once the reader continues.
 */
function CodeStep({
  initialCode = "",
  onCancel,
  onFound,
}: {
  initialCode?: string;
  onCancel: () => void;
  onFound: (request: PairingRequest) => void;
}) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const form = useAppForm({
    defaultValues: { code: initialCode },
    onSubmit: async ({ value }) => {
      try {
        // Fetched afresh on every submit: a pairing changes state on the
        // other device, so an earlier answer for the same code may be stale.
        const pairing = await queryClient.fetchQuery({
          ...deviceLookupQueryOptions(value.code),
          staleTime: 0,
        });
        onFound({ code: value.code, pairing });
      } catch (error) {
        applyServerError(form, error, {
          locations: {},
          codes: { pairing_not_found: "code", pairing_expired: "code" },
        });
      }
    },
  });

  return (
    <form.AppForm>
      <DialogHeader>
        <DialogTitle>
          <Trans id="devices.pair.title">Sign in a new device</Trans>
        </DialogTitle>
      </DialogHeader>
      <form.Form
        label={t({ id: "devices.form", message: "Device pairing" })}
        className="flex min-h-0 flex-1 flex-col"
      >
        <DialogPanel className="flex flex-col gap-4">
          <form.FormError />
          <form.AppField
            name="code"
            validators={{
              onSubmit: ({ value }) =>
                value.length === pairingCodeLength ? undefined : codeIncomplete,
            }}
          >
            {(field) => (
              <field.OtpField
                digits={pairingCodeLength}
                pattern={pairingCodePattern}
                inputMode="text"
                size="lg"
                // Eight large slots would overflow a phone dialog.
                className="max-sm:gap-1 max-sm:**:data-[slot=otp-field-input]:size-9 max-sm:**:data-[slot=otp-field-input]:leading-9"
                label={
                  <Trans id="devices.code.label">
                    Pairing code shown on the new device
                  </Trans>
                }
              />
            )}
          </form.AppField>
        </DialogPanel>
        <DialogFooter>
          <Button variant="ghost" onClick={onCancel}>
            <Trans id="devices.cancel">Cancel</Trans>
          </Button>
          <form.SubmitButton fullWidth>
            <Trans id="devices.continue">Continue</Trans>
          </form.SubmitButton>
        </DialogFooter>
      </form.Form>
    </form.AppForm>
  );
}

/**
 * Who is asking, and the decision. The reader checks the device against the
 * one in front of them — its name, where it asked from, and when — and the
 * sentence under the facts says what approving does.
 *
 * A pairing runs out: at `expiresAt` the approval is disabled and a notice
 * says to start again. A pairing this account already approved offers to take
 * that approval back instead.
 */
function ReviewStep({
  request,
  writes,
  onClose,
  onApproved,
}: {
  request: PairingRequest;
  writes: Writes;
  onClose: () => void;
  onApproved: (pairing: DevicePairing) => void;
}) {
  const { approve, decline, revoke, busy } = writes;
  const sessions = useQuery(quietQuery(sessionsQueryOptions()));
  const { code, pairing } = request;
  const expiresAt = Date.parse(pairing.expiresAt);
  const [expired, setExpired] = useState(() => Date.now() >= expiresAt);

  // Marked at the moment it happens rather than when the reader next acts,
  // so the approval is never offered for a code the server would refuse.
  useEffect(() => {
    const wait = expiresAt - Date.now();
    if (wait <= 0) {
      setExpired(true);
      return;
    }
    const timer = window.setTimeout(() => setExpired(true), wait);
    return () => window.clearTimeout(timer);
  }, [expiresAt]);

  // The code step's field is gone, and left to itself focus would land on the
  // first control here — the device name, whose tooltip then covers the
  // heading. The dialog takes it instead, as it does when it opens, so the
  // new heading is what is announced and the decision is not pre-selected.
  const facts = useRef<HTMLDListElement>(null);
  useEffect(() => {
    facts.current?.closest<HTMLElement>('[role="dialog"]')?.focus();
  }, []);

  const thisDevice = sessions.data?.find((session) => session.isCurrent);
  const sameNetwork =
    pairing.initiatorIp !== "" &&
    pairing.initiatorIp === thisDevice?.lastSeenIp;
  const ip = pairing.initiatorIp;

  return (
    <>
      <DialogHeader>
        <DialogTitle>
          {pairing.alreadyBound ? (
            <Trans id="devices.review.approved_title">
              Device already approved
            </Trans>
          ) : expired ? (
            <Trans id="devices.review.expired_title">
              This pairing code has expired
            </Trans>
          ) : (
            <Trans id="devices.review.title">Let this device sign in?</Trans>
          )}
        </DialogTitle>
      </DialogHeader>
      <DialogPanel className="flex flex-col gap-4">
        {pairing.alreadyBound ? (
          <SurfaceAlert status="accent">
            <SurfaceAlert.Indicator />
            <SurfaceAlert.Content>
              <SurfaceAlert.Title>
                <Trans id="devices.review.already_bound">
                  You have already approved this pairing. The new device is
                  finishing signing in.
                </Trans>
              </SurfaceAlert.Title>
            </SurfaceAlert.Content>
          </SurfaceAlert>
        ) : (
          expired && (
            <SurfaceAlert status="warning">
              <SurfaceAlert.Indicator />
              <SurfaceAlert.Content>
                <SurfaceAlert.Title>
                  <Trans id="devices.review.expired">
                    Start pairing again on the new device.
                  </Trans>
                </SurfaceAlert.Title>
              </SurfaceAlert.Content>
            </SurfaceAlert>
          )
        )}
        <dl
          ref={facts}
          className="grid min-w-0 gap-x-6 gap-y-3 rounded-xl bg-muted/60 p-4 ring-1 ring-border/60 sm:grid-cols-[max-content_minmax(0,1fr)] sm:gap-x-10"
        >
          <Detail label={<Trans id="devices.field.device">Device</Trans>}>
            <DeviceName userAgent={pairing.initiatorUa} />
          </Detail>
          <Detail label={<Trans id="devices.field.ip">IP address</Trans>}>
            {ip === "" ? (
              "—"
            ) : sameNetwork ? (
              <>
                <span className="font-mono">{ip}</span>
                <span className="block text-muted-foreground text-sm">
                  <Trans id="devices.field.same_device">
                    Same as this device
                  </Trans>
                </span>
              </>
            ) : (
              <span className="font-mono">{ip}</span>
            )}
          </Detail>
          <Detail label={<Trans id="devices.field.created">Requested</Trans>}>
            <RelativeTime value={pairing.createdAt} />
          </Detail>
          {!expired && (
            <Detail label={<Trans id="devices.field.expires">Expires</Trans>}>
              <RelativeTime value={pairing.expiresAt} />
            </Detail>
          )}
        </dl>
        {!pairing.alreadyBound && !expired && (
          <p className="text-sm">
            <Trans id="devices.review.consequence">
              After approval, this device is signed in to your account.
            </Trans>
          </p>
        )}
      </DialogPanel>
      <DialogFooter>
        {pairing.alreadyBound ? (
          <>
            <Button disabled={busy} onClick={onClose} variant="ghost">
              <Trans id="devices.close">Close</Trans>
            </Button>
            <Button
              disabled={busy}
              pending={revoke.isPending}
              onClick={() => revoke.mutate(code, { onSuccess: onClose })}
              variant="destructive-outline"
            >
              <Trans id="devices.revoke">Revoke approval</Trans>
            </Button>
          </>
        ) : (
          <>
            <Button
              disabled={busy}
              pending={decline.isPending}
              onClick={() => decline.mutate(code, { onSuccess: onClose })}
              variant="ghost"
            >
              <Trans id="devices.decline">Decline</Trans>
            </Button>
            <Button
              disabled={busy || expired}
              pending={approve.isPending}
              onClick={() =>
                approve.mutate(code, { onSuccess: () => onApproved(pairing) })
              }
            >
              <Trans id="devices.approve">Approve</Trans>
            </Button>
          </>
        )}
      </DialogFooter>
    </>
  );
}
