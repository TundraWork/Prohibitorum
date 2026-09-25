import {
  Description,
  Modal,
  REGEXP_ONLY_DIGITS_AND_CHARS,
} from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type ReactNode, useState } from "react";
import { describeError, isCancellation } from "@/api/errors";
import {
  approveDeviceMutationOptions,
  cancelDeviceMutationOptions,
  deviceLookupQueryOptions,
} from "@/api/mutations";
import type { DevicePairing } from "@/api/raw-paths";
import { Button } from "@/components/custom/Button";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

type PairingRequest = { code: string; pairing: DevicePairing };

/**
 * The already-signed-in half of device pairing. The other device shows a code
 * and polls for approval; this page takes the code, then asks in a dialog
 * whether the device it names is the one the user started, and approves or
 * declines it from there.
 *
 * The code is typed into slot boxes that accept the letters and digits the
 * server's alphabet contains; the server still owns the exact format and
 * normalises case, so the value goes out as typed. Only an empty box is
 * rejected locally.
 */
export function Devices() {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const [request, setRequest] = useState<PairingRequest | null>(null);

  const form = useAppForm({
    defaultValues: { code: "" },
    onSubmit: async ({ value }) => {
      try {
        // Fetched afresh on every submit: a pairing changes state on the
        // other device, so an earlier answer for the same code may be stale.
        const pairing = await queryClient.fetchQuery(
          deviceLookupQueryOptions(value.code),
        );
        setRequest({ code: value.code, pairing });
      } catch (error) {
        applyServerError(form, error, {
          locations: {},
          codes: { pairing_not_found: "code", pairing_expired: "code" },
        });
      }
    },
  });

  return (
    <>
      <form.AppForm>
        <form.Form label={t({ id: "devices.form", message: "Device pairing" })}>
          <form.FormError />
          <form.AppField
            name="code"
            validators={{
              onSubmit: ({ value }) =>
                value.trim() === ""
                  ? t({
                      id: "devices.code.required",
                      message:
                        "Enter the pairing code shown on the other device.",
                    })
                  : undefined,
            }}
          >
            {(field) => (
              <field.OtpField
                digits={8}
                pattern={REGEXP_ONLY_DIGITS_AND_CHARS}
                inputMode="text"
                label={
                  <Trans id="devices.code.label">
                    Code from the other device
                  </Trans>
                }
              />
            )}
          </form.AppField>
          <form.SubmitButton>
            <Trans id="devices.pair">Pair</Trans>
          </form.SubmitButton>
        </form.Form>
      </form.AppForm>

      <ApproveDialog
        request={request}
        onClose={() => setRequest(null)}
        onDone={() => {
          setRequest(null);
          form.reset();
        }}
      />
    </>
  );
}

/**
 * Who is asking, and the decision. The dialog stays mounted between requests
 * so its closing animation still has the last request to draw.
 */
function ApproveDialog({
  request,
  onClose,
  onDone,
}: {
  request: PairingRequest | null;
  onClose: () => void;
  onDone: () => void;
}) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const approve = useMutation(approveDeviceMutationOptions(queryClient));
  const decline = useMutation(cancelDeviceMutationOptions(queryClient));
  const [shown, setShown] = useState<PairingRequest | null>(request);
  const [error, setError] = useState<unknown>(null);

  // Keep the last request while the dialog closes, and drop the previous
  // request's error when a new one arrives.
  if (request !== null && request !== shown) {
    setShown(request);
    setError(null);
  }

  const busy = approve.isPending || decline.isPending;
  const pairing = shown?.pairing;

  const decide = (mutation: typeof approve | typeof decline) => {
    if (!shown) return;
    setError(null);
    mutation.mutate(shown.code, {
      onSuccess: onDone,
      onError: (failure) => {
        // Dismissing the identity check is a decision, not a failure.
        if (!isCancellation(failure)) setError(failure);
      },
    });
  };

  return (
    <Modal
      isOpen={request !== null}
      onOpenChange={(open) => !open && !busy && onClose()}
    >
      <Modal.Backdrop>
        <Modal.Container placement="center" size="md">
          <Modal.Dialog>
            <Modal.Header>
              <Modal.Heading>
                <Trans id="devices.confirm.title">Approve this device?</Trans>
              </Modal.Heading>
            </Modal.Header>
            <Modal.Body className="flex flex-col gap-4">
              {error !== null && (
                <SurfaceAlert status="danger" role="alert">
                  <SurfaceAlert.Indicator />
                  <SurfaceAlert.Content>
                    <SurfaceAlert.Title>
                      {t(describeError(error))}
                    </SurfaceAlert.Title>
                  </SurfaceAlert.Content>
                </SurfaceAlert>
              )}
              {pairing?.alreadyBound && (
                <SurfaceAlert status="warning">
                  <SurfaceAlert.Indicator />
                  <SurfaceAlert.Content>
                    <SurfaceAlert.Title>
                      <Trans id="devices.already_bound">
                        You have already approved this pairing. The other device
                        should be finishing its setup.
                      </Trans>
                    </SurfaceAlert.Title>
                  </SurfaceAlert.Content>
                </SurfaceAlert>
              )}
              <p>
                <Trans id="devices.confirm.body">
                  Only approve it if these details match the device you are
                  signing in on.
                </Trans>
              </p>
              {pairing && (
                <dl className="flex min-w-0 flex-col gap-3">
                  <Detail
                    label={<Trans id="devices.field.agent">Device</Trans>}
                  >
                    {pairing.initiatorUa || "—"}
                  </Detail>
                  <Detail
                    label={<Trans id="devices.field.ip">Requested from</Trans>}
                  >
                    {pairing.initiatorIp || "—"}
                  </Detail>
                  <Detail
                    label={<Trans id="devices.field.created">Requested</Trans>}
                  >
                    <RelativeTime value={pairing.createdAt} />
                  </Detail>
                </dl>
              )}
              <Description>
                <Trans id="devices.note">
                  Approving lets this device continue, but it is not signed in
                  yet: it still has to set up a passkey of its own.
                </Trans>
              </Description>
            </Modal.Body>
            <Modal.Footer>
              <Button
                variant="secondary"
                isDisabled={busy}
                isPending={decline.isPending}
                onPress={() => decide(decline)}
              >
                <Trans id="devices.decline">Decline</Trans>
              </Button>
              <Button
                isDisabled={busy || pairing?.alreadyBound}
                isPending={approve.isPending}
                onPress={() => decide(approve)}
              >
                <Trans id="devices.approve">Approve</Trans>
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}

function Detail({
  label,
  children,
}: {
  label: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="min-w-0">
      <dt className="text-sm text-muted">{label}</dt>
      <dd className="wrap-anywhere">{children}</dd>
    </div>
  );
}
