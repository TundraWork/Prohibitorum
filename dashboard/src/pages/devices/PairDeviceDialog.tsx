import { Modal, Tooltip } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import {
  approveDeviceMutationOptions,
  cancelDeviceMutationOptions,
  deviceLookupQueryOptions,
} from "@/api/mutations";
import { sessionsQueryOptions } from "@/api/queries";
import type { DevicePairing } from "@/api/raw-paths";
import { successMessage } from "@/api/success-messages";
import { Button } from "@/components/custom/Button";
import { Detail } from "@/components/custom/Detail";
import { DeviceName } from "@/components/custom/DeviceName";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

/** The server's code alphabet: base32 without 0, 1, I, L and O. */
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789";
/** Either case is accepted; the server compares codes without it. */
const codePattern = `^[${codeAlphabet}${codeAlphabet.toLowerCase()}]+$`;
const codeLength = 8;

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
 * keeps the last request to draw on its way out.
 */
export function PairDeviceDialog({
  isOpen,
  attempt,
  onClose,
  onApproved,
}: {
  isOpen: boolean;
  attempt: number;
  onClose: () => void;
  onApproved: (pairing: DevicePairing) => void;
}) {
  const [busy, setBusy] = useState(false);
  return (
    <Modal
      isOpen={isOpen}
      onOpenChange={(open) => {
        if (!open && !busy) onClose();
      }}
    >
      <Modal.Backdrop>
        <Modal.Container placement="center" size="md" scroll="inside">
          <Modal.Dialog>
            <PairingFlow
              key={attempt}
              onBusy={setBusy}
              onClose={onClose}
              onApproved={onApproved}
            />
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}

function PairingFlow({
  onBusy,
  onClose,
  onApproved,
}: {
  onBusy: (busy: boolean) => void;
  onClose: () => void;
  onApproved: (pairing: DevicePairing) => void;
}) {
  const [request, setRequest] = useState<PairingRequest | null>(null);
  return request === null ? (
    <CodeStep onCancel={onClose} onFound={setRequest} />
  ) : (
    <ReviewStep
      request={request}
      onBusy={onBusy}
      onClose={onClose}
      onApproved={onApproved}
    />
  );
}

/**
 * The code the new device shows, in slots that take only the characters the
 * server's alphabet contains. It goes out as typed; the server owns the
 * format and ignores case.
 */
function CodeStep({
  onCancel,
  onFound,
}: {
  onCancel: () => void;
  onFound: (request: PairingRequest) => void;
}) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const form = useAppForm({
    defaultValues: { code: "" },
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
      <Modal.Header>
        <Modal.Heading>
          <Trans id="devices.pair.title">Sign in a new device</Trans>
        </Modal.Heading>
      </Modal.Header>
      <form.Form
        label={t({ id: "devices.form", message: "Device pairing" })}
        className="flex min-h-0 flex-1 flex-col"
      >
        <Modal.Body>
          <div className="flex flex-col gap-4">
            <form.FormError />
            <form.AppField
              name="code"
              validators={{
                onSubmit: ({ value }) =>
                  value.length === codeLength ? undefined : codeIncomplete,
              }}
            >
              {(field) => (
                <field.OtpField
                  digits={codeLength}
                  pattern={codePattern}
                  inputMode="text"
                  variant="secondary"
                  label={
                    <Trans id="devices.code.label">
                      Pairing code shown on the new device
                    </Trans>
                  }
                />
              )}
            </form.AppField>
          </div>
        </Modal.Body>
        <Modal.Footer className="mt-5">
          <Button variant="secondary" onPress={onCancel}>
            <Trans id="devices.cancel">Cancel</Trans>
          </Button>
          <form.SubmitButton>
            <Trans id="devices.continue">Continue</Trans>
          </form.SubmitButton>
        </Modal.Footer>
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
 * that approval back instead. A failed write is the global toast's to report.
 */
function ReviewStep({
  request,
  onBusy,
  onClose,
  onApproved,
}: {
  request: PairingRequest;
  onBusy: (busy: boolean) => void;
  onClose: () => void;
  onApproved: (pairing: DevicePairing) => void;
}) {
  const queryClient = useQueryClient();
  const approve = useMutation(approveDeviceMutationOptions(queryClient));
  const decline = useMutation(cancelDeviceMutationOptions(queryClient));
  const revoke = useMutation(
    cancelDeviceMutationOptions(
      queryClient,
      successMessage.cancelDevicePairing,
    ),
  );
  const sessions = useQuery(sessionsQueryOptions());
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

  const busy = approve.isPending || decline.isPending || revoke.isPending;
  useEffect(() => onBusy(busy), [busy, onBusy]);

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

  const approveButton = (
    <Button
      isDisabled={busy || expired}
      isPending={approve.isPending}
      onPress={() =>
        approve.mutate(code, { onSuccess: () => onApproved(pairing) })
      }
    >
      <Trans id="devices.approve">Approve</Trans>
    </Button>
  );

  return (
    <>
      <Modal.Header>
        <Modal.Heading>
          {pairing.alreadyBound ? (
            <Trans id="devices.review.approved_title">
              Device already approved
            </Trans>
          ) : (
            <Trans id="devices.review.title">Let this device sign in?</Trans>
          )}
        </Modal.Heading>
      </Modal.Header>
      <Modal.Body className="flex flex-col gap-4">
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
                  <ExpiredReason />
                </SurfaceAlert.Title>
              </SurfaceAlert.Content>
            </SurfaceAlert>
          )
        )}
        <dl ref={facts} className="flex min-w-0 flex-col gap-3">
          <Detail label={<Trans id="devices.field.device">Device</Trans>}>
            <DeviceName userAgent={pairing.initiatorUa} />
          </Detail>
          <Detail label={<Trans id="devices.field.ip">IP address</Trans>}>
            {ip === "" ? (
              "—"
            ) : sameNetwork ? (
              <Trans id="devices.field.ip_same">
                {ip} · Same as this device
              </Trans>
            ) : (
              ip
            )}
          </Detail>
          <Detail label={<Trans id="devices.field.created">Requested</Trans>}>
            <RelativeTime value={pairing.createdAt} />
          </Detail>
          <Detail label={<Trans id="devices.field.expires">Expires</Trans>}>
            <RelativeTime value={pairing.expiresAt} />
          </Detail>
        </dl>
        {!pairing.alreadyBound && (
          <p>
            <Trans id="devices.review.consequence">
              Once approved, this device is signed in to your account.
            </Trans>
          </p>
        )}
      </Modal.Body>
      <Modal.Footer>
        {pairing.alreadyBound ? (
          <>
            <Button variant="secondary" isDisabled={busy} onPress={onClose}>
              <Trans id="devices.close">Close</Trans>
            </Button>
            <Button
              variant="danger-soft"
              isDisabled={busy}
              isPending={revoke.isPending}
              onPress={() => revoke.mutate(code, { onSuccess: onClose })}
            >
              <Trans id="devices.revoke">Revoke approval</Trans>
            </Button>
          </>
        ) : (
          <>
            <Button
              variant="secondary"
              isDisabled={busy}
              isPending={decline.isPending}
              onPress={() => decline.mutate(code, { onSuccess: onClose })}
            >
              <Trans id="devices.decline">Decline</Trans>
            </Button>
            {/* A disabled button emits no hover or focus, so the tooltip
                listens on the trigger wrapper instead. */}
            {expired ? (
              <Tooltip delay={0}>
                <Tooltip.Trigger>{approveButton}</Tooltip.Trigger>
                <Tooltip.Content>
                  <ExpiredReason />
                </Tooltip.Content>
              </Tooltip>
            ) : (
              approveButton
            )}
          </>
        )}
      </Modal.Footer>
    </>
  );
}

function ExpiredReason() {
  return (
    <Trans id="devices.review.expired">
      This pairing code has expired. Start again on the new device.
    </Trans>
  );
}
