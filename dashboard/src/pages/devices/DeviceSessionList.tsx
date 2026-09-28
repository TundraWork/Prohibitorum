import { Chip, Tooltip } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { LogOut, MonitorSmartphone } from "lucide-react";
import { useState } from "react";
import type { components } from "@/api/generated/schema";
import { revokeSessionMutationOptions } from "@/api/mutations";
import { sessionsQueryOptions } from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { DeviceIcon, DeviceName } from "@/components/custom/DeviceName";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { TableEmptyState } from "@/components/custom/TableEmptyState";

type Session = components["schemas"]["SessionListItem"];

/**
 * A wait for a newly approved device to appear: the number of sessions there
 * were when it was approved, and the moment its pairing expires.
 */
export type SessionWatch = { count: number; until: number };

/** How often the list asks again while it waits for an approved device. */
const watchInterval = 3000;

/**
 * The devices signed in to this account, one session each. The server marks
 * the row belonging to the current request and refuses to end it, so that
 * row's action is disabled and its tooltip explains that signing out is the
 * way to end it.
 *
 * An approved device signs in on its own, a moment later. While `watchUntil`
 * holds, the list asks again every few seconds until a session more than the
 * approval saw has arrived, or the pairing has expired and none will.
 */
export function DeviceSessionList({
  watchUntil,
}: {
  watchUntil: SessionWatch | null;
}) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const sessions = useQuery({
    ...sessionsQueryOptions(),
    refetchInterval: (query) => {
      if (watchUntil === null) return false;
      const arrived = (query.state.data?.length ?? 0) > watchUntil.count;
      return arrived || Date.now() >= watchUntil.until ? false : watchInterval;
    },
  });
  const [target, setTarget] = useState<Session | null>(null);
  const revoke = useMutation(revokeSessionMutationOptions(queryClient));

  const endSession = (session: Session) => {
    const button = (
      <Button
        isIconOnly
        size="sm"
        variant="danger-soft"
        aria-label={t({
          id: "devices.sessions.end",
          message: "End session",
        })}
        isDisabled={session.isCurrent}
        onPress={() => setTarget(session)}
      >
        <LogOut size={16} aria-hidden="true" />
      </Button>
    );
    // The current session is disabled rather than hidden, and the tooltip
    // carries the reason. A disabled button emits no hover or focus, so the
    // tooltip listens on the trigger wrapper instead.
    return session.isCurrent ? (
      <Tooltip delay={0}>
        <Tooltip.Trigger>{button}</Tooltip.Trigger>
        <Tooltip.Content>
          <Trans id="devices.sessions.use_sign_out">
            Use sign out to end this one
          </Trans>
        </Tooltip.Content>
      </Tooltip>
    ) : (
      button
    );
  };

  return (
    <>
      <ItemList
        label={t({
          id: "devices.sessions.list",
          message: "Signed-in devices",
        })}
        loading={sessions.isPending}
        empty={
          <TableEmptyState
            icon={
              <MonitorSmartphone
                size={18}
                strokeWidth={1.75}
                aria-hidden="true"
              />
            }
            title={
              <Trans id="devices.sessions.empty">No signed-in devices</Trans>
            }
          />
        }
      >
        {(sessions.data ?? []).map((session) => (
          <ItemListRow
            key={session.id}
            icon={<DeviceIcon userAgent={session.userAgent} />}
            title={<DeviceName userAgent={session.userAgent} />}
            badges={
              session.isCurrent && (
                <Chip color="accent" size="sm" variant="soft">
                  <Trans id="devices.sessions.current">This device</Trans>
                </Chip>
              )
            }
            details={[
              session.lastSeenIp || undefined,
              <Trans key="issued" id="devices.sessions.issued">
                Signed in <RelativeTime value={session.issuedAt} />
              </Trans>,
            ]}
            actions={endSession(session)}
          />
        ))}
      </ItemList>

      <ConfirmDialog
        isOpen={target !== null}
        onOpenChange={(open) => !open && setTarget(null)}
        status="danger"
        title={
          <Trans id="devices.sessions.revoke.title">End this session?</Trans>
        }
        body={
          <p>
            <Trans id="devices.sessions.revoke.body">
              That device is signed out immediately and will need to sign in
              again. Anything it is doing right now stops.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="devices.sessions.revoke.action">End session</Trans>
        }
        isPending={revoke.isPending}
        onConfirm={() => {
          if (!target) return;
          // A failure is the global toast's to report; the dialog just closes.
          revoke.mutate(target.id, { onSettled: () => setTarget(null) });
        }}
      />
    </>
  );
}
