import { Chip, Tooltip } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { LogOut, MonitorSmartphone } from "lucide-react";
import { useState } from "react";
import { describeError } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { revokeSessionMutationOptions } from "@/api/mutations";
import { sessionsQueryOptions } from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { TableEmptyState } from "@/components/custom/TableEmptyState";

type Session = components["schemas"]["SessionListItem"];

/** A one-line summary of a User-Agent; the raw string is not parsed further. */
function agentSummary(value?: string): string {
  if (!value) return "—";
  return value.length > 80 ? `${value.slice(0, 79)}…` : value;
}

/**
 * Sign-ins that are still active. The server marks the row belonging to the
 * current request and refuses to revoke it, so that row's action is disabled
 * and its tooltip explains that signing out is the way to end it.
 */
export function SessionsPanel() {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const sessions = useQuery(sessionsQueryOptions());
  const [target, setTarget] = useState<Session | null>(null);
  const [error, setError] = useState<unknown>(null);
  const revoke = useMutation(revokeSessionMutationOptions(queryClient));

  const endSession = (session: Session) => {
    const button = (
      <Button
        isIconOnly
        size="sm"
        variant="danger-soft"
        aria-label={t({
          id: "security.sessions.sign_out",
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
          <Trans id="security.sessions.use_sign_out">
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
      {error !== null && (
        <p role="alert" className="mt-2 text-sm">
          {t(describeError(error))}
        </p>
      )}

      <ItemList
        label={t({ id: "security.sessions.table", message: "Active sessions" })}
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
              <Trans id="security.sessions.empty">No other sessions</Trans>
            }
          />
        }
      >
        {(sessions.data ?? []).map((session) => (
          <ItemListRow
            key={session.id}
            icon={<MonitorSmartphone size={18} aria-hidden="true" />}
            title={
              <span title={session.userAgent ?? undefined}>
                {agentSummary(session.userAgent)}
              </span>
            }
            badges={
              session.isCurrent && (
                <Chip color="accent" size="sm" variant="soft">
                  <Trans id="security.sessions.current">This device</Trans>
                </Chip>
              )
            }
            details={[
              session.lastSeenIp || undefined,
              <Trans key="issued" id="security.sessions.detail.issued">
                Started <RelativeTime value={session.issuedAt} />
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
          <Trans id="security.sessions.revoke.title">End this session?</Trans>
        }
        body={
          <p>
            <Trans id="security.sessions.revoke.body">
              That device is signed out immediately and will need to sign in
              again. Anything it is doing right now stops.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="security.sessions.revoke.action">End session</Trans>
        }
        isPending={revoke.isPending}
        onConfirm={() => {
          if (!target) return;
          revoke.mutate(target.id, {
            onSuccess: () => setTarget(null),
            onError: (failure) => {
              setError(failure);
              setTarget(null);
            },
          });
        }}
      />
    </>
  );
}
