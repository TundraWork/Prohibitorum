import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Trash2, UserCheck, UserPlus, UsersRound } from "lucide-react";
import { useState } from "react";
import {
  assignAppManagerMutationOptions,
  removeAppManagerMutationOptions,
} from "@/api/mutations";
import {
  accountSearchQueryOptions,
  appManagersQueryOptions,
} from "@/api/queries";
import type { ManagedApplicationKind } from "@/api/raw-admin-paths";
import { quietQuery, withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { EntityAvatar } from "@/components/custom/EntityAvatar";
import {
  type EntityOption,
  EntityPicker,
} from "@/components/custom/EntityPicker";
import {
  InlineFailure,
  useInlineFailure,
} from "@/components/custom/InlineFailure";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { Section } from "@/components/custom/Section";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { getTopLayerContainer } from "@/components/custom/top-layer";
import {
  Dialog,
  DialogFooter,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";

const removeManagerMessage = msg({
  id: "app.managers.remove",
  message: "Remove {name}",
});

/**
 * A quiet line for a delegated manager saying why they can edit this page.
 * Drawn under the page header; it renders nothing for an administrator.
 */
export function AppManagerNote({ isAdmin }: { isAdmin: boolean }) {
  if (isAdmin) return null;
  return (
    <p className="flex items-center gap-2 text-muted-foreground text-sm">
      <UserCheck aria-hidden="true" className="size-4 shrink-0" />
      <Trans id="app.managers.self">You manage this application.</Trans>
    </p>
  );
}

/**
 * The accounts allowed to manage an application. Administrators only: a
 * delegated manager gets `AppManagerNote` instead.
 *
 * The list endpoint answers an administrator alone, so a manager's page never
 * asks for it, and an empty section would claim the application has no
 * managers when the truth is that they cannot be seen. A failed read is not an
 * empty list for the same reason.
 *
 * Both writes are held here, not in the dialog that starts one: only a mounted
 * mutation runs its callbacks. The assign dialog cannot be dismissed while its
 * write is in flight, so a refusal is read inside it; removing shows its
 * failure in the section.
 */
export function AppManagersSection({
  kind,
  appId,
  isAdmin,
}: {
  kind: ManagedApplicationKind;
  appId: string;
  isAdmin: boolean;
}) {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const managers = useQuery(
    quietQuery({ ...appManagersQueryOptions(kind, appId), enabled: isAdmin }),
  );
  const assign = useMutation(
    withInlineReport(assignAppManagerMutationOptions(queryClient, kind), true),
  );
  const removeManager = useMutation(
    withInlineReport(removeAppManagerMutationOptions(queryClient, kind), true),
  );
  const assignFailure = useInlineFailure();
  const removeFailure = useInlineFailure();
  const [assigning, setAssigning] = useState(false);
  // A new key per opening starts the dialog from an empty choice.
  const [assignings, setAssignings] = useState(0);
  const [removing, setRemoving] = useState<number | null>(null);

  if (!isAdmin) return null;

  const target = (managers.data ?? []).find(
    (manager) => manager.id === removing,
  );
  // Named, so the confirmation carries it as `{name}` in both catalogs.
  const name = target?.displayName ?? "";

  return (
    <>
      <Section
        id="managers"
        icon={<UsersRound />}
        title={<Trans id="app.managers.label">Managers</Trans>}
        description={
          <Trans id="app.managers.description">
            Managers can change everything on this page except who manages it.
          </Trans>
        }
        action={
          <Button
            onClick={() => {
              assignFailure.clear();
              setAssignings((count) => count + 1);
              setAssigning(true);
            }}
            variant="outline"
            size="sm"
          >
            <UserPlus />
            <Trans id="app.managers.assign">Assign a manager</Trans>
          </Button>
        }
      >
        {removeFailure.error !== null && (
          <div className="px-5 pb-3">
            <InlineFailure error={removeFailure.error} />
          </div>
        )}
        {managers.isError ? (
          <div className="p-4">
            <SurfaceAlert status="danger" role="alert">
              <SurfaceAlert.Indicator />
              <SurfaceAlert.Content>
                <SurfaceAlert.Title>
                  {t({
                    id: "app.managers.load_failed",
                    message: "Could not load the managers.",
                  })}
                </SurfaceAlert.Title>
              </SurfaceAlert.Content>
            </SurfaceAlert>
          </div>
        ) : (
          <ItemList
            label={t({ id: "app.managers.label", message: "Managers" })}
            loading={managers.isPending}
            empty={
              <p className="px-4 py-6 text-center text-muted-foreground text-sm">
                <Trans id="app.managers.empty">No managers assigned yet</Trans>
              </p>
            }
          >
            {(managers.data ?? []).map((manager) => (
              <ItemListRow
                key={manager.id}
                iconBare
                icon={
                  <EntityAvatar
                    name={manager.displayName}
                    shape="round"
                    className="size-9"
                  />
                }
                title={manager.displayName}
                details={[
                  <span key="username" className="font-mono">
                    @{manager.username}
                  </span>,
                ]}
                meta={
                  <Trans id="app.managers.assigned">
                    Assigned <RelativeTime value={manager.assignedAt} />
                  </Trans>
                }
                actions={
                  <Button
                    aria-label={i18n._({
                      ...removeManagerMessage,
                      values: { name: manager.displayName },
                    })}
                    onClick={() => {
                      removeFailure.clear();
                      setRemoving(manager.id);
                    }}
                    variant="destructive-outline"
                    size="icon-sm"
                  >
                    <Trash2 />
                  </Button>
                }
              />
            ))}
          </ItemList>
        )}
      </Section>

      <AssignManagerDialog
        open={assigning}
        session={assignings}
        assignedIds={
          new Set((managers.data ?? []).map((manager) => manager.id))
        }
        pending={assign.isPending}
        error={assignFailure.error}
        onCancel={() => setAssigning(false)}
        onAssign={(accountId) => {
          assignFailure.clear();
          assign.mutate(
            { appId, accountId },
            {
              onSuccess: () => setAssigning(false),
              onError: assignFailure.report,
            },
          );
        }}
      />

      <ConfirmDialog
        isOpen={removing !== null}
        onOpenChange={(open) => !open && setRemoving(null)}
        status="danger"
        title={
          <Trans id="app.managers.confirm.title">Remove this manager?</Trans>
        }
        body={
          <p>
            <Trans id="app.managers.confirm.body">
              {name} will no longer be able to change this application. Their
              account is not affected.
            </Trans>
          </p>
        }
        confirmLabel={<Trans id="app.managers.confirm.action">Remove</Trans>}
        isPending={removeManager.isPending}
        onConfirm={() => {
          if (removing === null) return;
          removeManager.mutate(
            { appId, accountId: removing },
            {
              onError: removeFailure.report,
              onSettled: () => setRemoving(null),
            },
          );
        }}
      />
    </>
  );
}

/** One account chosen from the directory, which is searched rather than listed. */
function AssignManagerDialog({
  open,
  session,
  assignedIds,
  pending,
  error,
  onCancel,
  onAssign,
}: {
  open: boolean;
  /** Changes with each opening, which resets the choice. */
  session: number;
  assignedIds: ReadonlySet<number>;
  pending: boolean;
  error: unknown;
  onCancel: () => void;
  onAssign: (accountId: number) => void;
}) {
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !next && !pending && onCancel()}
    >
      <DialogPopup
        className="sm:max-w-md"
        portalProps={{ container: getTopLayerContainer() }}
      >
        <AssignManagerForm
          key={session}
          assignedIds={assignedIds}
          pending={pending}
          error={error}
          onCancel={onCancel}
          onAssign={onAssign}
        />
      </DialogPopup>
    </Dialog>
  );
}

function AssignManagerForm({
  assignedIds,
  pending,
  error,
  onCancel,
  onAssign,
}: {
  assignedIds: ReadonlySet<number>;
  pending: boolean;
  error: unknown;
  onCancel: () => void;
  onAssign: (accountId: number) => void;
}) {
  const { t } = useLingui();
  const [search, setSearch] = useState("");
  const [chosen, setChosen] = useState<string[]>([]);
  const [missing, setMissing] = useState(false);
  const candidates = useQuery(accountSearchQueryOptions(search));
  // Accounts that cannot be assigned stay listed with the reason, so an
  // administrator looking for one learns why rather than finding it missing.
  const options: EntityOption[] = (candidates.data?.items ?? []).map(
    (account) => {
      const unavailableReason = account.disabled
        ? t({
            id: "app.managers.candidate.disabled",
            message: "Disabled account",
          })
        : assignedIds.has(account.id)
          ? t({
              id: "app.managers.candidate.assigned",
              message: "Already manages this application",
            })
          : undefined;
      return {
        id: String(account.id),
        label: account.displayName || account.username,
        ...(account.displayName ? { identifier: `@${account.username}` } : {}),
        ...(unavailableReason === undefined ? {} : { unavailableReason }),
      };
    },
  );

  return (
    <form
      className="flex min-h-0 flex-1 flex-col"
      aria-label={t({
        id: "app.managers.dialog.title",
        message: "Assign a manager",
      })}
      onSubmit={(event) => {
        event.preventDefault();
        if (pending) return;
        const [id] = chosen;
        if (id === undefined) {
          setMissing(true);
          return;
        }
        onAssign(Number(id));
      }}
    >
      <DialogHeader>
        <DialogTitle>
          <Trans id="app.managers.dialog.title">Assign a manager</Trans>
        </DialogTitle>
      </DialogHeader>
      <DialogPanel className="flex flex-col gap-4">
        <InlineFailure error={error} />
        <EntityPicker
          label={<Trans id="app.managers.dialog.label">Manager</Trans>}
          placeholder={t({
            id: "app.managers.dialog.placeholder",
            message: "Search accounts",
          })}
          searchPlaceholder={t({
            id: "app.managers.dialog.placeholder",
            message: "Search accounts",
          })}
          searchLabel={t({ id: "admin.picker.search", message: "Search" })}
          options={options}
          loading={candidates.isFetching}
          onSearch={setSearch}
          value={chosen}
          onValueChange={(next) => {
            setChosen(next);
            setMissing(false);
          }}
          isInvalid={missing}
          errorMessage={t({
            id: "app.managers.dialog.required",
            message: "Choose an account to assign.",
          })}
        />
      </DialogPanel>
      <DialogFooter>
        <Button variant="ghost" disabled={pending} onClick={onCancel}>
          <Trans id="admin.cancel">Cancel</Trans>
        </Button>
        <Button type="submit" pending={pending}>
          <Trans id="app.managers.dialog.save">Assign</Trans>
        </Button>
      </DialogFooter>
    </form>
  );
}
