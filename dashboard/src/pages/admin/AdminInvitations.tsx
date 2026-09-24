import { Alert, AlertDialog, Chip } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ClipboardCopy, MailPlus } from "lucide-react";
import { useState } from "react";
import { useCursorList } from "@/api/cursor-list";
import { describeError } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { revokeInvitationMutationOptions } from "@/api/mutations";
import {
  identityProvidersQueryOptions,
  invitationsListOptions,
} from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { TableEmptyState } from "@/components/custom/TableEmptyState";

type Invitation = components["schemas"]["InvitationView"];
type Locale = ReturnType<typeof useLingui>["i18n"];

/**
 * Outstanding invitations.
 *
 * The registration link in each row is a bearer value: whoever holds it can
 * create the account. It is therefore copied rather than printed — a table cell
 * wide enough for the whole URL would put the secret on screen at every glance
 * over someone's shoulder.
 *
 * `groupIds` is what the invitation was created with, and `groups` only the
 * subset still resolving, so a row where the two disagree is flagged: an admin
 * reading a shorter list than the request would otherwise trust the list.
 */
export function AdminInvitations() {
  const { t, i18n } = useLingui();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const providers = useQuery(identityProvidersQueryOptions());
  const [target, setTarget] = useState<Invitation | null>(null);
  const [error, setError] = useState<unknown>(null);
  const revoke = useMutation(revokeInvitationMutationOptions(queryClient));

  const list = useCursorList<Invitation>(invitationsListOptions());

  const providerName = (slug?: string) => {
    if (!slug) return null;
    return (
      (providers.data?.items ?? []).find((provider) => provider.slug === slug)
        ?.displayName ?? slug
    );
  };

  return (
    <>
      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center justify-end gap-4">
          <Button
            onPress={() => void navigate({ to: "/admin/invitations/new" })}
          >
            <Trans id="admin.invitations.invite">Invite a user</Trans>
          </Button>
        </div>

        {error !== null && (
          <Alert status="danger" role="alert">
            <Alert.Indicator />
            <Alert.Content>
              <Alert.Title>{t(describeError(error))}</Alert.Title>
            </Alert.Content>
          </Alert>
        )}

        {list.error !== null && list.error !== undefined && (
          <Alert status="danger" role="alert">
            <Alert.Indicator />
            <Alert.Content>
              <Alert.Title>
                <Trans id="admin.invitations.error">
                  The list could not be loaded. Try again.
                </Trans>
              </Alert.Title>
            </Alert.Content>
          </Alert>
        )}

        <DataTable
          label={t({ id: "admin.invitations.table", message: "Invitations" })}
          columns={invitationColumns(i18n, providerName, setTarget)}
          rows={list.items}
          rowId={(invitation) => invitation.token}
          loading={list.loading}
          hasMore={list.hasMore}
          loadingMore={list.loadingMore}
          onLoadMore={list.loadMore}
          empty={
            <TableEmptyState
              icon={
                <MailPlus size={18} strokeWidth={1.75} aria-hidden="true" />
              }
              title={
                <Trans id="admin.invitations.empty">No invitations yet</Trans>
              }
            />
          }
        />
      </div>

      <AlertDialog
        isOpen={target !== null}
        onOpenChange={(open) => !open && setTarget(null)}
      >
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="md">
            <AlertDialog.Dialog>
              <AlertDialog.Header>
                <AlertDialog.Heading>
                  <Trans id="admin.invitations.revoke.title">
                    Revoke this invitation?
                  </Trans>
                </AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>
                <p>
                  <Trans id="admin.invitations.revoke.body">
                    The registration link stops working immediately, and the
                    invitation no longer appears in this list. This cannot be
                    undone.
                  </Trans>
                </p>
              </AlertDialog.Body>
              <AlertDialog.Footer>
                <Button
                  variant="secondary"
                  onPress={() => setTarget(null)}
                  isDisabled={revoke.isPending}
                >
                  <Trans id="admin.cancel">Cancel</Trans>
                </Button>
                <Button
                  variant="danger"
                  isPending={revoke.isPending}
                  onPress={() => {
                    if (!target) return;
                    revoke.mutate(target.token, {
                      onSuccess: async () => {
                        setTarget(null);
                        await navigate({ to: "/admin/invitations" });
                      },
                      onError: (failure) => {
                        setError(failure);
                        setTarget(null);
                      },
                    });
                  }}
                >
                  <Trans id="admin.invitations.revoke.action">Revoke</Trans>
                </Button>
              </AlertDialog.Footer>
            </AlertDialog.Dialog>
          </AlertDialog.Container>
        </AlertDialog.Backdrop>
      </AlertDialog>
    </>
  );
}

function invitationColumns(
  i18n: Locale,
  providerName: (slug?: string) => string | null,
  revoke: (invitation: Invitation) => void,
): TableColumn<Invitation>[] {
  const format = (value?: string) =>
    value === undefined
      ? "—"
      : new Intl.DateTimeFormat(i18n.locale, {
          dateStyle: "medium",
          timeStyle: "short",
        }).format(new Date(value));
  return [
    {
      id: "role",
      header: <Trans id="admin.invitations.column.role">Role</Trans>,
      cell: (invitation) =>
        invitation.role === "admin" ? (
          <Chip color="accent" size="sm" variant="soft">
            <Trans id="admin.invitations.role.admin">Admin</Trans>
          </Chip>
        ) : (
          <Trans id="admin.invitations.role.user">User</Trans>
        ),
    },
    {
      id: "provider",
      header: (
        <Trans id="admin.invitations.column.provider">Upstream provider</Trans>
      ),
      // Absent, not empty, when the invitation does not insist on one.
      cell: (invitation) =>
        providerName(invitation.expectedUpstreamIdpSlug) ?? "—",
    },
    {
      id: "username",
      header: (
        <Trans id="admin.invitations.column.username">Account username</Trans>
      ),
      cell: (invitation) => (
        <span className="wrap-anywhere">{invitation.username || "—"}</span>
      ),
    },
    {
      id: "groups",
      header: <Trans id="admin.invitations.column.groups">User groups</Trans>,
      cell: (invitation) => {
        const groups = invitation.groups ?? [];
        if (groups.length === 0) return "—";
        // A shorter resolved list than the saved ids means a group was deleted
        // after the invitation was created; the invitation still carries it.
        const missing = groups.length < (invitation.groupIds ?? []).length;
        return (
          <span className="wrap-anywhere">
            {groups.map((group) => group.displayName).join(", ")}
            {missing && (
              <span className="text-muted">
                {" "}
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
      align: "end",
      id: "createdAt",
      header: <Trans id="admin.invitations.column.created">Created</Trans>,
      cell: (invitation) => format(invitation.createdAt),
    },
    {
      align: "end",
      id: "expiresAt",
      header: <Trans id="admin.invitations.column.expires">Expires</Trans>,
      cell: (invitation) => format(invitation.expiresAt),
    },
    {
      id: "url",
      header: <Trans id="admin.invitations.column.link">Link</Trans>,
      cell: (invitation) => <CopyLink url={invitation.url} />,
    },
    {
      id: "actions",
      header: <Trans id="admin.column.actions">Actions</Trans>,
      cell: (invitation) => (
        <Button
          size="sm"
          variant="danger-soft"
          onPress={() => revoke(invitation)}
        >
          <Trans id="admin.invitations.revoke.action">Revoke</Trans>
        </Button>
      ),
      pinned: true,
    },
  ];
}

/**
 * The registration link as an affordance rather than a value. The URL is long
 * and it is a bearer token, so the row offers to put it on the clipboard and
 * confirms only that the copy happened.
 */
function CopyLink({ url }: { url: string }) {
  const { t } = useLingui();
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle");

  return (
    <span className="flex items-center gap-2">
      <Button
        size="sm"
        variant="secondary"
        aria-label={t({
          id: "admin.invitations.copy",
          message: "Copy the registration link",
        })}
        onPress={() => {
          void (async () => {
            setState("idle");
            try {
              await navigator.clipboard.writeText(url);
              setState("copied");
            } catch {
              setState("failed");
            }
          })();
        }}
      >
        <ClipboardCopy size={16} aria-hidden="true" />
        <Trans id="admin.invitations.copy.action">Copy link</Trans>
      </Button>
      {state === "copied" && (
        <span className="text-sm text-muted" role="status">
          <Trans id="admin.invitations.copy.done">Copied.</Trans>
        </span>
      )}
      {state === "failed" && (
        <span className="text-sm text-danger" role="status">
          <Trans id="admin.invitations.copy.failed">
            Could not copy the link. Your browser blocked the clipboard; allow
            it and try again.
          </Trans>
        </span>
      )}
    </span>
  );
}
