import { AlertDialog, Button, Chip, Tooltip } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { LogOut, MonitorSmartphone } from "lucide-react";
import { useState } from "react";
import { describeError } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { revokeSessionMutationOptions } from "@/api/mutations";
import { sessionsQueryOptions } from "@/api/queries";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
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
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const sessions = useQuery(sessionsQueryOptions());
  const [target, setTarget] = useState<Session | null>(null);
  const [error, setError] = useState<unknown>(null);
  const revoke = useMutation(revokeSessionMutationOptions(queryClient));

  const format = (value: string) =>
    new Intl.DateTimeFormat(i18n.locale, {
      dateStyle: "medium",
      timeStyle: "short",
    }).format(new Date(value));

  const columns: readonly TableColumn<Session>[] = [
    {
      id: "current",
      header: <Trans id="security.sessions.column.status">Status</Trans>,
      cell: (session) =>
        session.isCurrent ? (
          <Chip color="accent" size="sm">
            <Trans id="security.sessions.current">This device</Trans>
          </Chip>
        ) : (
          <span className="text-muted">
            <Trans id="security.sessions.other">Signed in</Trans>
          </span>
        ),
    },
    {
      id: "issuedAt",
      header: <Trans id="security.sessions.column.issued">Started</Trans>,
      cell: (session) => format(session.issuedAt),
    },
    {
      id: "expiresAt",
      header: <Trans id="security.sessions.column.expires">Expires</Trans>,
      cell: (session) => format(session.expiresAt),
    },
    {
      id: "lastSeenIp",
      header: <Trans id="security.sessions.column.ip">Last seen from</Trans>,
      cell: (session) => (
        <span className="wrap-anywhere">{session.lastSeenIp || "—"}</span>
      ),
    },
    {
      id: "userAgent",
      header: <Trans id="security.sessions.column.agent">Device</Trans>,
      cell: (session) => (
        <span className="wrap-anywhere" title={session.userAgent ?? undefined}>
          {agentSummary(session.userAgent)}
        </span>
      ),
    },
    {
      id: "actions",
      header: <Trans id="security.column.actions">Actions</Trans>,
      cell: (session) => {
        const endSession = (
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
            <Tooltip.Trigger>{endSession}</Tooltip.Trigger>
            <Tooltip.Content>
              <Trans id="security.sessions.use_sign_out">
                Use sign out to end this one
              </Trans>
            </Tooltip.Content>
          </Tooltip>
        ) : (
          endSession
        );
      },
      pinned: true,
    },
  ];

  return (
    <>
      {error !== null && (
        <p role="alert" className="mt-2 text-sm">
          {t(describeError(error))}
        </p>
      )}

      <DataTable
        label={t({ id: "security.sessions.table", message: "Active sessions" })}
        columns={columns}
        rows={sessions.data ?? []}
        rowId={(session) => session.id}
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
      />

      <AlertDialog
        isOpen={target !== null}
        onOpenChange={(open) => !open && setTarget(null)}
      >
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="md">
            <AlertDialog.Dialog>
              <AlertDialog.Header>
                <AlertDialog.Heading>
                  <Trans id="security.sessions.revoke.title">
                    End this session?
                  </Trans>
                </AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>
                <p>
                  <Trans id="security.sessions.revoke.body">
                    That device is signed out immediately and will need to sign
                    in again. Anything it is doing right now stops.
                  </Trans>
                </p>
              </AlertDialog.Body>
              <AlertDialog.Footer>
                <Button
                  variant="secondary"
                  onPress={() => setTarget(null)}
                  isDisabled={revoke.isPending}
                >
                  <Trans id="security.cancel">Cancel</Trans>
                </Button>
                <Button
                  variant="danger"
                  isPending={revoke.isPending}
                  onPress={() => {
                    if (!target) return;
                    revoke.mutate(target.id, {
                      onSuccess: () => setTarget(null),
                      onError: (failure) => {
                        setError(failure);
                        setTarget(null);
                      },
                    });
                  }}
                >
                  <Trans id="security.sessions.revoke.action">
                    End session
                  </Trans>
                </Button>
              </AlertDialog.Footer>
            </AlertDialog.Dialog>
          </AlertDialog.Container>
        </AlertDialog.Backdrop>
      </AlertDialog>
    </>
  );
}
