import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { LogOut, MonitorSmartphone } from "lucide-react";
import { useState } from "react";
import type { components } from "@/api/generated/schema";
import {
  revokeAccountSessionMutationOptions,
  revokeAccountSessionsMutationOptions,
} from "@/api/mutations";
import { accountSessionsQueryOptions } from "@/api/queries";
import { quietQuery, withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import {
  DeviceIcon,
  DeviceName,
  rawUserAgentDetail,
  useDeviceLabel,
} from "@/components/custom/DeviceName";
import { useInlineFailure } from "@/components/custom/InlineFailure";
import { ItemListRow } from "@/components/custom/ItemList";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import { AccessSection } from "@/pages/admin/users/AccessSection";

type Session = components["schemas"]["SessionListItem"];

/**
 * The devices signed in to an account, each of which an administrator can end,
 * and all at once when there is more than one. Ending a session signs that
 * device out immediately; it asks for no identity check, so the confirmation
 * is the only step.
 */
export function AccountSessionsSection({ accountId }: { accountId: number }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const sessions = useQuery(quietQuery(accountSessionsQueryOptions(accountId)));
  const revoke = useMutation(
    withInlineReport(revokeAccountSessionMutationOptions(queryClient), true),
  );
  const revokeAll = useMutation(
    withInlineReport(revokeAccountSessionsMutationOptions(queryClient), true),
  );
  const failure = useInlineFailure();
  const [target, setTarget] = useState<Session | null>(null);
  const [endingAll, setEndingAll] = useState(false);
  const rows = sessions.data?.items ?? [];
  const deviceLabel = useDeviceLabel();
  const endLabel = (session: Session) => {
    const device = deviceLabel(session.userAgent);
    return t({
      id: "admin.user.sessions.end.named",
      message: `End the session on ${device}`,
    });
  };

  return (
    <>
      <AccessSection
        id="sessions"
        icon={<MonitorSmartphone />}
        title={<Trans id="admin.user.sessions.title">Active sessions</Trans>}
        description={
          <Trans id="admin.user.sessions.description">
            The devices currently using this account.
          </Trans>
        }
        label={t({
          id: "admin.user.sessions.table",
          message: "Active sessions",
        })}
        query={sessions}
        writeError={failure.error}
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
              <Trans id="admin.user.sessions.empty">No active sessions</Trans>
            }
          />
        }
        footer={
          rows.length > 1 ? (
            <Button
              variant="destructive-outline"
              size="sm"
              pending={revokeAll.isPending}
              onClick={() => {
                failure.clear();
                setEndingAll(true);
              }}
            >
              <Trans id="admin.user.sessions.revoke_all">
                Sign out everywhere
              </Trans>
            </Button>
          ) : undefined
        }
      >
        {rows.map((session) => (
          <ItemListRow
            key={session.id}
            icon={<DeviceIcon userAgent={session.userAgent} />}
            title={
              <DeviceName
                userAgent={session.userAgent}
                className="block truncate"
              />
            }
            details={[
              session.lastSeenIp ? (
                <span key="ip" className="font-mono">
                  {session.lastSeenIp}
                </span>
              ) : undefined,
              <Trans key="issued" id="admin.user.sessions.detail.issued">
                Started <RelativeTime value={session.issuedAt} />
              </Trans>,
              rawUserAgentDetail(session.userAgent),
            ]}
            actions={
              <Button
                aria-label={endLabel(session)}
                onClick={() => {
                  failure.clear();
                  setTarget(session);
                }}
                variant="destructive-outline"
                size="icon-sm"
              >
                <LogOut aria-hidden="true" />
              </Button>
            }
          />
        ))}
      </AccessSection>

      <ConfirmDialog
        isOpen={target !== null}
        onOpenChange={(open) => !open && !revoke.isPending && setTarget(null)}
        status="danger"
        title={
          <Trans id="admin.user.sessions.revoke.title">End this session?</Trans>
        }
        body={
          <p>
            <Trans id="admin.user.sessions.revoke.body">
              That device is signed out immediately and will need to sign in
              again. Anything it is doing right now stops.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.user.sessions.revoke.action">End session</Trans>
        }
        isPending={revoke.isPending}
        onConfirm={() => {
          if (target === null) return;
          revoke.mutate(
            { accountId, id: target.id },
            { onError: failure.report, onSettled: () => setTarget(null) },
          );
        }}
      />

      <ConfirmDialog
        isOpen={endingAll}
        onOpenChange={(open) =>
          !open && !revokeAll.isPending && setEndingAll(false)
        }
        status="danger"
        title={
          <Trans id="admin.user.sessions.revoke_all.title">
            Sign this account out everywhere?
          </Trans>
        }
        body={
          <p>
            <Trans id="admin.user.sessions.revoke_all.body">
              Every device signed in to this account is signed out immediately
              and will need to sign in again.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.user.sessions.revoke_all.action">
            Sign out everywhere
          </Trans>
        }
        isPending={revokeAll.isPending}
        onConfirm={() =>
          revokeAll.mutate(accountId, {
            onError: failure.report,
            onSettled: () => setEndingAll(false),
          })
        }
      />
    </>
  );
}
