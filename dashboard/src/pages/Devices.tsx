import { Trans } from "@lingui/react/macro";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { sessionsQueryOptions } from "@/api/queries";
import { Button } from "@/components/custom/Button";
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
 * A list page: the action first at the top left, then the list on the page
 * background, which the console header already names. Signing in a new device
 * happens in a dialog — the code from the new device, then the approval — and
 * the device then signs itself in, so once it is approved the list watches for
 * it until it arrives or its pairing runs out.
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
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-4">
        <Button
          onPress={() => {
            setAttempt((count) => count + 1);
            setPairing(true);
          }}
        >
          <Trans id="devices.pair.open">Sign in a new device</Trans>
        </Button>
      </div>

      <DeviceSessionList watchUntil={watch} />

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
    </div>
  );
}
