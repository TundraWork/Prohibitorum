import { Trans, useLingui } from "@lingui/react/macro";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { LogOut, MonitorSmartphone } from "lucide-react";
import { type ReactNode, useEffect, useState } from "react";
import type { components } from "@/api/generated/schema";
import { revokeSessionMutationOptions } from "@/api/mutations";
import { sessionsQueryOptions } from "@/api/queries";
import { quietQuery, withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import {
  DeviceIcon,
  DeviceName,
  rawUserAgentDetail,
  useDeviceLabel,
} from "@/components/custom/DeviceName";
import {
  InlineFailure,
  useInlineFailure,
} from "@/components/custom/InlineFailure";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import { Badge } from "@/components/ui/badge";
import { Spinner } from "@/components/ui/spinner";

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
 * the row belonging to the current request and refuses to end it, so that row
 * says to use sign out instead of offering an action that would fail.
 *
 * An approved device signs in on its own, a moment later. While `watchUntil`
 * holds, the list says it is waiting and asks again every few seconds, until
 * a session more than the approval saw has arrived or the pairing has expired
 * and none will.
 */
export function DeviceSessionList({
  watchUntil,
}: {
  watchUntil: SessionWatch | null;
}) {
  const { t } = useLingui();
  const deviceLabel = useDeviceLabel();
  const queryClient = useQueryClient();
  const { data } = useSuspenseQuery({
    ...quietQuery(sessionsQueryOptions()),
    refetchInterval: (query) => {
      if (watchUntil === null) return false;
      const arrived = (query.state.data?.length ?? 0) > watchUntil.count;
      return arrived || Date.now() >= watchUntil.until ? false : watchInterval;
    },
  });
  const [target, setTarget] = useState<Session | null>(null);
  const { error, clear, report } = useInlineFailure();

  // The failure is drawn on this page, so the toast stays silent.
  const revoke = useMutation(
    withInlineReport(revokeSessionMutationOptions(queryClient), true),
  );

  const waiting = useWaiting(watchUntil, data.length);

  return (
    <div className="flex flex-col gap-4">
      <InlineFailure error={error} />

      {/* The region stays mounted so the line is announced when it appears. */}
      <div aria-live="polite" className="empty:hidden">
        {waiting && (
          <p className="flex items-center gap-2 text-muted-foreground text-sm">
            <Spinner className="size-4" aria-hidden="true" />
            <Trans id="devices.sessions.waiting">
              Waiting for the new device to finish signing in…
            </Trans>
          </p>
        )}
      </div>

      <ItemList
        label={t({
          id: "devices.sessions.list",
          message: "Signed-in devices",
        })}
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
        {data.map((session) => {
          const device = deviceLabel(session.userAgent);
          return (
            <ItemListRow
              key={session.id}
              icon={<DeviceIcon userAgent={session.userAgent} />}
              title={
                <DeviceName
                  userAgent={session.userAgent}
                  className="block truncate"
                />
              }
              badge={
                session.isCurrent && (
                  <Badge variant="info">
                    <Trans id="devices.sessions.current">This device</Trans>
                  </Badge>
                )
              }
              details={sessionDetails(session)}
              actions={
                session.isCurrent ? (
                  <span className="max-w-28 text-end text-muted-foreground text-xs leading-snug sm:max-w-none">
                    <Trans id="devices.sessions.use_sign_out">
                      Use sign out to end this one
                    </Trans>
                  </span>
                ) : (
                  <Button
                    aria-label={t({
                      id: "devices.sessions.end.named",
                      message: `End the session on ${device}`,
                    })}
                    onClick={() => setTarget(session)}
                    variant="destructive-outline"
                    size="icon-sm"
                  >
                    <LogOut aria-hidden="true" />
                  </Button>
                )
              }
            />
          );
        })}
      </ItemList>

      <ConfirmDialog
        isOpen={target !== null}
        onOpenChange={(open) => !open && setTarget(null)}
        status="danger"
        title={
          <Trans id="devices.sessions.revoke.title">End this session?</Trans>
        }
        subject={
          target !== null && (
            <DeviceName userAgent={target.userAgent} className="block" />
          )
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
          clear();
          revoke.mutate(target.id, {
            onSuccess: () => setTarget(null),
            onError: (failure) => {
              report(failure);
              setTarget(null);
            },
          });
        }}
      />
    </div>
  );
}

/**
 * Where it signed in from and when. A User-Agent the title could not name is
 * shown whole here, since the title only has room for its start.
 */
function sessionDetails(session: Session): ReactNode[] {
  const details: ReactNode[] = [];
  if (session.lastSeenIp) {
    details.push(
      <span key="ip" className="font-mono">
        {session.lastSeenIp}
      </span>,
    );
  }
  details.push(
    <Trans key="issued" id="devices.sessions.issued">
      Signed in <RelativeTime value={session.issuedAt} />
    </Trans>,
  );
  details.push(rawUserAgentDetail(session.userAgent));
  return details;
}

/**
 * Whether a watched approval is still waiting: its device has not appeared
 * and its pairing has not run out. The timer flips it at the expiry, since
 * nothing else re-renders the list once polling stops.
 */
function useWaiting(watch: SessionWatch | null, sessionCount: number): boolean {
  const [lapsed, setLapsed] = useState(false);
  useEffect(() => {
    if (watch === null) return;
    const remaining = watch.until - Date.now();
    if (remaining <= 0) {
      setLapsed(true);
      return;
    }
    setLapsed(false);
    const timer = window.setTimeout(() => setLapsed(true), remaining);
    return () => window.clearTimeout(timer);
  }, [watch]);
  return watch !== null && !lapsed && sessionCount <= watch.count;
}
