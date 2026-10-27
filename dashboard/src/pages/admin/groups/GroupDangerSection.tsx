import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Trash2, TriangleAlertIcon } from "lucide-react";
import { useId, useState } from "react";
import { deleteGroupMutationOptions } from "@/api/mutations";
import { groupApplicationsQueryOptions } from "@/api/queries";
import type { AppGroupView } from "@/api/raw-admin-paths";
import { quietQuery, withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { DangerZoneFrame } from "@/components/custom/DangerZone";
import {
  InlineFailure,
  useInlineFailure,
} from "@/components/custom/InlineFailure";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";

/**
 * Deleting the group: the only change that stops a group, since a group has no
 * disabled state.
 *
 * The server refuses to delete a group an application still selects, so the
 * button is disabled while the applications read says any do, and the row
 * states in words how many and what to do. Until that read answers, the button
 * waits; if it fails the button is left to the server, which gives the same
 * refusal as `group_in_use`. The dialog stays open while the write is in
 * flight, and a refusal is shown once, under the row.
 */
export function GroupDangerSection({ group }: { group: AppGroupView }) {
  const { t } = useLingui();
  const reasonId = useId();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  // The reason only decorates the button, so a failed read is not a failure
  // worth a toast: the server's own refusal says the same.
  const applications = useQuery(
    quietQuery(groupApplicationsQueryOptions(group.id)),
  );
  const remove = useMutation(
    withInlineReport(deleteGroupMutationOptions(queryClient), true),
  );
  const failure = useInlineFailure();
  // Named, so the confirmation carries it as `{name}` in both catalogs.
  const name = group.displayName;
  const [confirming, setConfirming] = useState(false);
  const [leaving, setLeaving] = useState(false);
  const busy = remove.isPending || leaving;

  const used = applications.data?.items?.length ?? 0;
  const waiting = applications.isPending;

  return (
    <DangerZoneFrame
      id="danger"
      icon={<TriangleAlertIcon />}
      title={<Trans id="admin.group.danger.title">Delete</Trans>}
    >
      <ItemList
        label={t({ id: "admin.group.danger.label", message: "Danger zone" })}
        empty={null}
      >
        <ItemListRow
          icon={<Trash2 size={18} aria-hidden="true" />}
          title={
            <Trans id="admin.group.danger.delete">Delete this group</Trans>
          }
          details={[
            <Trans key="note" id="admin.group.danger.delete-note">
              Deletes the group and its membership settings. This cannot be
              undone.
            </Trans>,
          ]}
          stackActions
          actions={
            <Button
              disabled={waiting || used > 0 || busy}
              aria-describedby={used > 0 ? reasonId : undefined}
              onClick={() => {
                failure.clear();
                setConfirming(true);
              }}
              variant="destructive-outline"
            >
              <Trans id="admin.group.danger.delete.action">Delete</Trans>
            </Button>
          }
        >
          {used > 0 && (
            <p
              id={reasonId}
              className="flex items-center gap-1.5 text-pretty text-warning-foreground text-caption"
            >
              <TriangleAlertIcon
                aria-hidden="true"
                className="size-3.5 shrink-0"
              />
              <Plural
                id="admin.group.danger.in-use"
                value={used}
                one="# application uses this group. Remove the group from its access settings first."
                other="# applications use this group. Remove the group from their access settings first."
              />
            </p>
          )}
        </ItemListRow>
      </ItemList>

      <InlineFailure error={failure.error} />

      <ConfirmDialog
        isOpen={confirming}
        onOpenChange={(open) => !open && !busy && setConfirming(false)}
        status="danger"
        title={
          <Trans id="admin.group.danger.delete.title">Delete this group?</Trans>
        }
        subject={name}
        body={
          <p>
            <Trans id="admin.group.danger.delete.body">
              The group and its membership settings will be deleted. This cannot
              be undone.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.group.danger.delete.confirm">Delete group</Trans>
        }
        isPending={busy}
        onConfirm={() => {
          remove.mutate(group.id, {
            // The dialog stays pending until the list has replaced this page.
            onSuccess: async () => {
              setLeaving(true);
              try {
                await navigate({ to: "/admin/groups" });
              } finally {
                setLeaving(false);
                setConfirming(false);
              }
            },
            onError: (error) => {
              setConfirming(false);
              failure.report(error);
            },
          });
        }}
      />
    </DangerZoneFrame>
  );
}
