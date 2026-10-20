import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
  Ban,
  Check,
  ClipboardCopy,
  MailPlus,
  Plus,
  UserRound,
} from "lucide-react";
import { useState } from "react";
import { useCursorList } from "@/api/cursor-list";
import type { components } from "@/api/generated/schema";
import { revokeInvitationMutationOptions } from "@/api/mutations";
import {
  identityProvidersQueryOptions,
  invitationsListOptions,
} from "@/api/queries";
import { quietQuery, withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { EntityCell } from "@/components/custom/EntityCell";
import {
  InlineFailure,
  useInlineFailure,
} from "@/components/custom/InlineFailure";
import { PageHeader } from "@/components/custom/PageHeader";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { LaterPageFailure, SectionFailure } from "@/components/custom/Section";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import { useCopyValue } from "@/components/custom/use-copy-value";
import { Badge } from "@/components/ui/badge";

type Invitation = components["schemas"]["InvitationView"];

const copyNamed = msg({
  id: "admin.invitations.copy.named",
  message: "Copy the registration link for {name}",
});
const copyUnnamed = msg({
  id: "admin.invitations.copy.unnamed",
  message: "Copy the registration link without a username",
});
const revokeNamed = msg({
  id: "admin.invitations.revoke.named",
  message: "Revoke the invitation for {name}",
});
const revokeUnnamed = msg({
  id: "admin.invitations.revoke.unnamed",
  message: "Revoke the invitation without a username",
});

/**
 * The invitations still waiting to be used. The server lists only unused,
 * unexpired ones, so a row has no status: it leaves the list when redeemed,
 * expired or revoked. The registration link is a bearer value, so a row copies
 * it rather than printing it.
 */
export function AdminInvitations() {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const providers = useQuery(quietQuery(identityProvidersQueryOptions()));
  const [target, setTarget] = useState<Invitation | null>(null);
  const [copyFailed, setCopyFailed] = useState(false);
  const revoke = useMutation(
    withInlineReport(revokeInvitationMutationOptions(queryClient), true),
  );
  const failure = useInlineFailure();
  const list = useCursorList<Invitation>(quietQuery(invitationsListOptions()));

  const providerName = (slug?: string) =>
    slug === undefined || slug === ""
      ? null
      : ((providers.data?.items ?? []).find((item) => item.slug === slug)
          ?.displayName ?? slug);

  const day = (value: string) =>
    new Intl.DateTimeFormat(i18n.locale, { dateStyle: "medium" }).format(
      new Date(value),
    );

  const columns: TableColumn<Invitation>[] = [
    {
      id: "invitee",
      // A fixed track, so a long name truncates inside it instead of pushing
      // Created and Expires out of the frame.
      width: "w-80 max-w-80",
      header: <Trans id="admin.invitations.column.invitee">Invitee</Trans>,
      cell: (invitation) => (
        <EntityCell
          shape="round"
          fallback={<UserRound className="size-4" aria-hidden="true" />}
          name={
            invitation.username ? (
              <span className="font-mono" title={`@${invitation.username}`}>
                @{invitation.username}
              </span>
            ) : (
              <span className="text-muted-foreground">
                <Trans id="admin.invitations.username.later">
                  Chosen at registration
                </Trans>
              </span>
            )
          }
          marks={
            invitation.role === "admin" ? (
              <Badge variant="outline">
                <Trans id="object.status.admin">Admin</Trans>
              </Badge>
            ) : null
          }
        />
      ),
    },
    {
      id: "provider",
      width: "w-48 max-w-48",
      header: (
        <Trans id="admin.invitations.column.provider">Upstream provider</Trans>
      ),
      // Absent, not empty, when the invitation does not insist on one.
      cell: (invitation) => {
        const name = providerName(invitation.expectedUpstreamIdpSlug);
        return name === null ? (
          <span className="text-muted-foreground">—</span>
        ) : (
          <span className="block truncate" title={name}>
            {name}
          </span>
        );
      },
    },
    {
      id: "groups",
      width: "w-48 max-w-48",
      header: <Trans id="admin.invitations.column.groups">User groups</Trans>,
      cell: (invitation) => {
        const groups = invitation.groups ?? [];
        // A shorter resolved list than the saved ids means a group was deleted
        // after the invitation was created; the invitation still carries it.
        const missing = groups.length < (invitation.groupIds ?? []).length;
        if (groups.length === 0 && !missing) {
          return <span className="text-muted-foreground">—</span>;
        }
        const names = groups.map((group) => group.displayName).join(", ");
        return (
          <span className="flex min-w-0 flex-col">
            {groups.length > 0 && (
              <span className="min-w-0 truncate" title={names}>
                {names}
              </span>
            )}
            {missing && (
              <span className="truncate text-muted-foreground text-xs">
                <Trans id="admin.invitations.groups.missing">
                  (some groups no longer exist)
                </Trans>
              </span>
            )}
          </span>
        );
      },
    },
    {
      id: "createdAt",
      header: <Trans id="admin.invitations.column.created">Created</Trans>,
      cell: (invitation) => <RelativeTime value={invitation.createdAt} />,
    },
    {
      id: "expiresAt",
      header: <Trans id="admin.invitations.column.expires">Expires</Trans>,
      cell: (invitation) => day(invitation.expiresAt),
    },
    {
      align: "end",
      id: "actions",
      header: "",
      pinned: true,
      cell: (invitation) => {
        const name = invitation.username;
        return (
          <>
            <CopyLink
              url={invitation.url}
              label={
                name
                  ? i18n._({ ...copyNamed, values: { name } })
                  : i18n._(copyUnnamed)
              }
              onCopied={(ok) => setCopyFailed(!ok)}
            />
            <Button
              aria-label={
                name
                  ? i18n._({ ...revokeNamed, values: { name } })
                  : i18n._(revokeUnnamed)
              }
              onClick={() => {
                failure.clear();
                setTarget(invitation);
              }}
              variant="destructive-outline"
              size="icon-sm"
            >
              <Ban aria-hidden="true" />
            </Button>
          </>
        );
      },
    },
  ];

  const listFailed = list.error !== null && list.error !== undefined;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={<Trans id="admin.invitations.page.title">Invitations</Trans>}
        description={
          <Trans id="admin.invitations.page.description">
            Invitations waiting to be used. Used or expired ones leave this
            list.
          </Trans>
        }
        actions={
          <Button render={<Link to="/admin/invitations/new" />}>
            <Plus aria-hidden="true" />
            <Trans id="admin.invitations.invite">Invite a user</Trans>
          </Button>
        }
      />

      <div className="flex flex-col gap-4">
        {listFailed && !list.laterFailed && (
          <SectionFailure
            error={list.error}
            onRetry={list.refetch}
            title={
              <Trans id="admin.invitations.error">
                The invitations could not be loaded.
              </Trans>
            }
          />
        )}
        {providers.isError && (
          <p className="text-muted-foreground text-sm">
            <Trans id="admin.invitations.providers-failed">
              Provider names could not be loaded, so providers are shown by
              their identifier.
            </Trans>
          </p>
        )}
        <InlineFailure error={failure.error} />
        {copyFailed && (
          <SurfaceAlert status="danger" role="alert">
            <SurfaceAlert.Indicator />
            <SurfaceAlert.Content>
              <SurfaceAlert.Title>
                <Trans id="admin.invitations.copy.failed">
                  Could not copy the link. Your browser blocked the clipboard.
                </Trans>
              </SurfaceAlert.Title>
            </SurfaceAlert.Content>
          </SurfaceAlert>
        )}

        {/* A failed first read has no rows to show, and an empty table under
            the failure would say there are none. */}
        {(!listFailed || list.items.length > 0) && (
          <DataTable
            label={t({ id: "admin.invitations.table", message: "Invitations" })}
            columns={columns}
            rows={list.items}
            rowId={(invitation) => invitation.token}
            loading={list.loading}
            hasMore={list.hasMore && !list.laterFailed}
            loadingMore={list.loadingMore}
            onLoadMore={list.loadMore}
            empty={
              <TableEmptyState
                icon={
                  <MailPlus size={18} strokeWidth={1.75} aria-hidden="true" />
                }
                title={
                  <Trans id="admin.invitations.empty.waiting">
                    No invitations waiting
                  </Trans>
                }
                description={
                  <Trans id="admin.invitations.empty.description">
                    Invite someone and their link appears here until they use
                    it.
                  </Trans>
                }
              />
            }
          />
        )}
        {list.laterFailed && (
          <LaterPageFailure
            error={list.error}
            onRetry={list.loadMore}
            title={
              <Trans id="admin.invitations.more_failed">
                The rest of the invitations could not be loaded.
              </Trans>
            }
          />
        )}
      </div>

      <ConfirmDialog
        isOpen={target !== null}
        onOpenChange={(open) => !open && !revoke.isPending && setTarget(null)}
        status="danger"
        title={
          <Trans id="admin.invitations.revoke.title">
            Revoke this invitation?
          </Trans>
        }
        subject={
          target !== null &&
          (target.username ? (
            <span className="font-mono">@{target.username}</span>
          ) : (
            <Trans id="admin.invitations.username.later">
              Chosen at registration
            </Trans>
          ))
        }
        body={
          <p>
            <Trans id="admin.invitations.revoke.body">
              The registration link stops working immediately, and the
              invitation no longer appears in this list. This cannot be undone.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.invitations.revoke.action">Revoke</Trans>
        }
        isPending={revoke.isPending}
        onConfirm={() => {
          if (target === null) return;
          revoke.mutate(target.token, {
            onError: failure.report,
            onSettled: () => setTarget(null),
          });
        }}
      />
    </div>
  );
}

/**
 * The registration link as an affordance rather than a value: it is a bearer
 * token, so the row offers to copy it and confirms with a check. The outcome is
 * reported from the press itself, so a refusal on one row cannot outlive a
 * later success on another.
 */
function CopyLink({
  url,
  label,
  onCopied,
}: {
  url: string;
  label: string;
  onCopied: (ok: boolean) => void;
}) {
  const { t } = useLingui();
  const { state, confirm } = useCopyValue();

  return (
    <Button
      aria-label={
        state === "copied"
          ? t({ id: "admin.invitations.copy.done", message: "Copied." })
          : label
      }
      onClick={() => {
        navigator.clipboard.writeText(url).then(
          () => {
            confirm();
            onCopied(true);
          },
          () => onCopied(false),
        );
      }}
      variant="ghost"
      size="icon-sm"
    >
      {state === "copied" ? (
        <Check className="text-success" aria-hidden="true" />
      ) : (
        <ClipboardCopy aria-hidden="true" />
      )}
    </Button>
  );
}
