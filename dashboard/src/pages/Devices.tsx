import { Trans } from "@lingui/react/macro";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { sessionsQueryOptions } from "@/api/queries";
import { Button } from "@/components/custom/Button";
import {
  DeviceSessionList,
  type SessionWatch,
} from "@/pages/devices/DeviceSessionList";
import { PairDeviceDialog } from "@/pages/devices/PairDeviceDialog";

/**
 * The devices signed in to this account, and signing in another one.
 *
 * A list page: the action first at the top left, then the list on the page
 * background, which the console header already names. Signing in a new device
 * happens in a dialog — the code from the new device, then the approval — and
 * the device then signs itself in, so once it is approved the list watches for
 * it until it arrives or its pairing runs out.
 */
export function Devices() {
  const queryClient = useQueryClient();
  const [pairing, setPairing] = useState(false);
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
