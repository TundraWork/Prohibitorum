import { Trans } from "@lingui/react/macro";
import { useQueryClient } from "@tanstack/react-query";
import { QrCode } from "lucide-react";
import { useEffect, useState } from "react";
import { sessionsQueryOptions } from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { PageBody } from "@/components/custom/PageBody";
import { PageHeader } from "@/components/custom/PageHeader";
import { AsyncSection } from "@/components/custom/Section";
import {
  DeviceSessionList,
  type SessionWatch,
} from "@/pages/devices/DeviceSessionList";
import { PairDeviceDialog } from "@/pages/devices/PairDeviceDialog";
import { Route } from "@/routes/_protected.devices";

/**
 * The code a QR link brought is for this one arrival: the page keeps it for
 * the dialog and takes it out of the address, so a reload or a step back
 * through history does not offer a code that is used up by then.
 */
export function DevicesPage() {
  const { code } = Route.useSearch();
  const navigate = Route.useNavigate();
  const [arrivedWith] = useState(code);
  useEffect(() => {
    if (arrivedWith !== undefined) {
      void navigate({ search: {}, replace: true });
    }
  }, [arrivedWith, navigate]);
  return <Devices code={arrivedWith} />;
}

/**
 * The devices signed in to this account, and signing in another one.
 *
 * Signing in a new device happens in a dialog — the code from the new device,
 * then the approval — and the device then signs itself in, so once it is
 * approved the list watches for it until it arrives or its pairing runs out.
 *
 * `code` is a pairing code the new device's QR link brought along. The page
 * then opens with the dialog showing it, and the reader still presses
 * Continue; any later opening starts from an empty code.
 */
export function Devices({ code }: { code?: string }) {
  const queryClient = useQueryClient();
  const [pairing, setPairing] = useState(code !== undefined);
  const [attempt, setAttempt] = useState(0);
  const [watch, setWatch] = useState<SessionWatch | null>(null);

  return (
    <PageBody>
      <PageHeader
        title={<Trans id="devices.page.title">Devices</Trans>}
        description={
          <Trans id="devices.page.description">
            Where you are signed in right now, and a way to sign in a device
            that shows a pairing code.
          </Trans>
        }
        actions={
          <Button
            size="sm"
            onClick={() => {
              setAttempt((count) => count + 1);
              setPairing(true);
            }}
          >
            <QrCode aria-hidden="true" />
            <Trans id="devices.pair.open">Sign in a new device</Trans>
          </Button>
        }
      />

      <AsyncSection resetKey="devices">
        <DeviceSessionList watchUntil={watch} />
      </AsyncSection>

      <PairDeviceDialog
        isOpen={pairing}
        attempt={attempt}
        initialCode={attempt === 0 ? code : undefined}
        onClose={() => setPairing(false)}
        onApproved={(approved) => {
          setPairing(false);
          const sessions = queryClient.getQueryData(
            sessionsQueryOptions().queryKey,
          );
          setWatch({
            count: sessions?.length ?? 0,
            until: Date.parse(approved.expiresAt),
          });
        }}
      />
    </PageBody>
  );
}
