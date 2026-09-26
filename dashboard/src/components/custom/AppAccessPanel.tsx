import { Modal } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Lock, LockOpen, Plus, Trash2, UserPlus } from "lucide-react";
import { useState } from "react";
import { isCancellation } from "@/api/errors";
import {
  assignAppManagerMutationOptions,
  removeAppManagerMutationOptions,
  replaceAppGroupsMutationOptions,
  setAppAccessRestrictedMutationOptions,
} from "@/api/mutations";
import {
  accountSearchQueryOptions,
  appAccessQueryOptions,
  appManagersQueryOptions,
  groupsQueryOptions,
} from "@/api/queries";
import type {
  AppGroupView,
  ManagedApplicationKind,
} from "@/api/raw-admin-paths";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import type { EntityOption } from "@/components/custom/EntityPicker";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

/**
 * Who may use an application, and who may manage it.
 *
 * The same panel appears on all three application detail pages: the access
 * policy API is protocol-neutral (`/managed-applications/{kind}/{appId}/…`), so
 * the protocol only decides which identifiers go in the path. Writing it once
 * means the restricted/unrestricted wording, the group picker's candidate rules
 * and the manager list's admin-only nature are decided in one place rather than
 * three times over.
 *
 * ## Administrators and delegated managers see different things
 *
 * A manager assigned to the application reaches this panel too — the server
 * authorises them for everything here except the manager list, which is
 * administrators-only. So the managers block is drawn only for an
 * administrator, and the request behind it is not made at all for anyone else.
 *
 * The group picker reads `GET /groups`, which for a non-administrator answers
 * with their own memberships rather than the whole directory. That is exactly
 * the set the server will accept from them, so the candidate list is right
 * without the console having to filter it.
 *
 * ## Why the group list is written whole
 *
 * Adding and removing both submit the complete id list. The endpoint replaces
 * the selection rather than patching it, so a concurrent change by another
 * administrator is not silently merged — the next read shows what happened.
 */
const removeGroupMessage = msg({
  id: "app.groups.remove",
  message: "Remove {group}",
});

const removeManagerMessage = msg({
  id: "app.managers.remove",
  message: "Remove {name}",
});

export function AppAccessPanel({
  kind,
  appId,
  /** Whether the signed-in account is an administrator. */
  isAdmin,
}: {
  kind: ManagedApplicationKind;
  appId: string;
  isAdmin: boolean;
}) {
  return (
    <div className="flex flex-col gap-8">
      <AccessRestrictionKind kind={kind} appId={appId} />
      <ApplicationGroupsKind kind={kind} appId={appId} isAdmin={isAdmin} />
      {isAdmin && <ApplicationManagersKind kind={kind} appId={appId} />}
    </div>
  );
}

/** Whether the application is open to everyone or limited to selected groups. */
function AccessRestrictionKind({
  kind,
  appId,
}: {
  kind: ManagedApplicationKind;
  appId: string;
}) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const access = useQuery(appAccessQueryOptions(kind, appId));
  const setRestricted = useMutation(
    setAppAccessRestrictedMutationOptions(queryClient, kind),
  );
  const [confirming, setConfirming] = useState(false);

  if (access.isError) {
    return (
      <SurfaceAlert status="danger" role="alert">
        <SurfaceAlert.Indicator />
        <SurfaceAlert.Content>
          <SurfaceAlert.Title>
            {t({
              id: "app.access.load_failed",
              message: "Could not load the access policy.",
            })}
          </SurfaceAlert.Title>
        </SurfaceAlert.Content>
      </SurfaceAlert>
    );
  }

  const restricted = access.data?.accessRestricted ?? false;
  const target = restricted ? "open" : "restricted";

  return (
    <>
      <ItemList
        label={t({
          id: "app.access.restriction.label",
          message: "Access restriction",
        })}
        loading={access.isPending}
        empty={null}
      >
        <ItemListRow
          icon={
            restricted ? (
              <Lock size={18} aria-hidden="true" />
            ) : (
              <LockOpen size={18} aria-hidden="true" />
            )
          }
          title={
            restricted ? (
              <Trans id="app.access.restricted">
                Available only to the selected user groups
              </Trans>
            ) : (
              <Trans id="app.access.open">Every account can use this</Trans>
            )
          }
          actions={
            <Button
              size="sm"
              variant={restricted ? "outline" : "secondary"}
              isPending={setRestricted.isPending}
              onPress={() => setConfirming(true)}
            >
              {restricted ? (
                <Trans id="app.access.open.action">Make it open</Trans>
              ) : (
                <Trans id="app.access.restrict.action">Restrict access</Trans>
              )}
            </Button>
          }
        />
      </ItemList>

      <ConfirmDialog
        isOpen={confirming}
        onOpenChange={setConfirming}
        status="warning"
        title={
          target === "restricted" ? (
            <Trans id="app.access.restrict.title">
              Restrict this application?
            </Trans>
          ) : (
            <Trans id="app.access.open.title">
              Open this application to everyone?
            </Trans>
          )
        }
        body={
          target === "restricted" ? (
            <p>
              <Trans id="app.access.restrict.body">
                Only accounts in the user groups you select can use this
                application. Until you select one, nobody can.
              </Trans>
            </p>
          ) : (
            <p>
              <Trans id="app.access.open.body">
                Every account on this instance can use this application. The
                selected user groups stay where they are, ready if you restrict
                it again.
              </Trans>
            </p>
          )
        }
        confirmLabel={
          target === "restricted" ? (
            <Trans id="app.access.restrict.confirm">Restrict</Trans>
          ) : (
            <Trans id="app.access.open.confirm">Open to everyone</Trans>
          )
        }
        isPending={setRestricted.isPending}
        onConfirm={() => {
          setRestricted.mutate(
            { appId, restricted: target === "restricted" },
            { onSettled: () => setConfirming(false) },
          );
        }}
      />
    </>
  );
}

/** The global user groups whose accounts may use the application. */
function ApplicationGroupsKind({
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
  const access = useQuery(appAccessQueryOptions(kind, appId));
  const groups = useQuery({ ...groupsQueryOptions(), enabled: true });
  const replace = useMutation(
    replaceAppGroupsMutationOptions(queryClient, kind),
  );
  const [editing, setEditing] = useState(false);

  const selected: AppGroupView[] = access.data?.groups ?? [];

  const form = useAppForm({
    defaultValues: { groupIds: [] as string[] },
    onSubmit: async ({ value }) => {
      try {
        await replace.mutateAsync({
          appId,
          groupIds: value.groupIds.map(Number),
        });
        setEditing(false);
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, {
          codes: {},
          locations: {},
        });
      }
    },
  });

  return (
    <>
      <ItemList
        label={t({ id: "app.groups.label", message: "User groups" })}
        loading={access.isPending}
        empty={
          <p className="px-4 py-6 text-center text-sm text-muted">
            <Trans id="app.groups.empty">No user groups selected yet</Trans>
          </p>
        }
        footer={
          // Adding is only meaningful while the application is restricted: an
          // open application ignores the selection, and offering to change it
          // would imply otherwise.
          access.data?.accessRestricted === true ? (
            <div className="px-4 py-3">
              <Button
                size="sm"
                variant="outline"
                onPress={() => {
                  form.reset();
                  form.setFieldValue(
                    "groupIds",
                    selected.map((group) => String(group.id)),
                  );
                  setEditing(true);
                }}
              >
                <Plus size={16} aria-hidden="true" />
                <Trans id="app.groups.add">Add user groups</Trans>
              </Button>
            </div>
          ) : undefined
        }
      >
        {selected.map((group) => (
          <ItemListRow
            key={group.id}
            title={
              isAdmin ? (
                <a
                  className="hover:underline"
                  href={`/admin/groups/${group.id}`}
                >
                  {group.displayName}
                </a>
              ) : (
                group.displayName
              )
            }
            details={[group.slug]}
            actions={
              <Button
                isIconOnly
                size="sm"
                variant="ghost"
                aria-label={i18n._({
                  ...removeGroupMessage,
                  values: { group: group.displayName },
                })}
                isPending={replace.isPending}
                onPress={() => {
                  replace.mutate({
                    appId,
                    groupIds: selected
                      .filter((candidate) => candidate.id !== group.id)
                      .map((candidate) => candidate.id),
                  });
                }}
              >
                <Trash2 size={16} aria-hidden="true" />
              </Button>
            }
          />
        ))}
      </ItemList>

      <Modal isOpen={editing} onOpenChange={setEditing}>
        <Modal.Backdrop>
          <Modal.Container placement="center" size="md">
            <Modal.Dialog>
              <Modal.Header>
                <Modal.Heading>
                  <Trans id="app.groups.dialog.title">User groups</Trans>
                </Modal.Heading>
              </Modal.Header>
              <Modal.Body>
                <form.AppForm>
                  <form.Form
                    label={t({
                      id: "app.groups.dialog.label",
                      message: "User groups",
                    })}
                    className="flex flex-col gap-4"
                  >
                    <form.FormError />
                    <form.AppField name="groupIds">
                      {(field) => (
                        <field.GroupPicker
                          label={
                            <Trans id="app.groups.dialog.label">
                              User groups
                            </Trans>
                          }
                          // For a non-admin this read answers with their own groups,
                          // which is exactly what the server will accept back.
                          options={(groups.data ?? []).map((group) => ({
                            id: String(group.id),
                            label: group.displayName,
                            description: group.slug,
                          }))}
                          loading={groups.isPending}
                          variant="secondary"
                        />
                      )}
                    </form.AppField>
                    <form.SubmitButton>
                      <Trans id="app.groups.dialog.save">Save</Trans>
                    </form.SubmitButton>
                  </form.Form>
                </form.AppForm>
              </Modal.Body>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>
    </>
  );
}

/** The accounts allowed to manage the application. Administrators only. */
function ApplicationManagersKind({
  kind,
  appId,
}: {
  kind: ManagedApplicationKind;
  appId: string;
}) {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const managers = useQuery(appManagersQueryOptions(kind, appId));
  const assign = useMutation(
    assignAppManagerMutationOptions(queryClient, kind),
  );
  const removeManager = useMutation(
    removeAppManagerMutationOptions(queryClient, kind),
  );
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<number | null>(null);
  // The account directory is searched rather than listed: it runs to a page at a
  // time, and an administrator assigning a manager knows the username.
  const [accountSearch, setAccountSearch] = useState("");
  const candidates = useQuery(accountSearchQueryOptions(accountSearch));
  const accountOptions: EntityOption[] = (candidates.data?.items ?? []).map(
    (account) => ({
      id: String(account.id),
      label: account.username,
      ...(account.displayName ? { description: account.displayName } : {}),
    }),
  );

  const form = useAppForm({
    defaultValues: { accountId: "" },
    onSubmit: async ({ value }) => {
      try {
        await assign.mutateAsync({ appId, accountId: Number(value.accountId) });
        setAdding(false);
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, {
          codes: { invalid_manager_role: "accountId" },
          locations: {},
        });
      }
    },
  });

  const target = (managers.data ?? []).find(
    (manager) => manager.id === removing,
  );

  return (
    <>
      <ItemList
        label={t({ id: "app.managers.label", message: "Managers" })}
        loading={managers.isPending}
        empty={
          <p className="px-4 py-6 text-center text-sm text-muted">
            <Trans id="app.managers.empty">No managers assigned yet</Trans>
          </p>
        }
        footer={
          <div className="px-4 py-3">
            <Button size="sm" variant="outline" onPress={() => setAdding(true)}>
              <UserPlus size={16} aria-hidden="true" />
              <Trans id="app.managers.assign">Assign a manager</Trans>
            </Button>
          </div>
        }
      >
        {(managers.data ?? []).map((manager) => (
          <ItemListRow
            key={manager.id}
            title={manager.displayName}
            details={[
              manager.username,
              <Trans key="assigned" id="app.managers.assigned">
                Assigned <RelativeTime value={manager.assignedAt} />
              </Trans>,
            ]}
            actions={
              <Button
                isIconOnly
                size="sm"
                variant="ghost"
                aria-label={i18n._({
                  ...removeManagerMessage,
                  values: { name: manager.displayName },
                })}
                onPress={() => setRemoving(manager.id)}
              >
                <Trash2 size={16} aria-hidden="true" />
              </Button>
            }
          />
        ))}
      </ItemList>

      <Modal isOpen={adding} onOpenChange={setAdding}>
        <Modal.Backdrop>
          <Modal.Container placement="center" size="md">
            <Modal.Dialog>
              <Modal.Header>
                <Modal.Heading>
                  <Trans id="app.managers.dialog.title">Assign a manager</Trans>
                </Modal.Heading>
              </Modal.Header>
              <Modal.Body>
                <form.AppForm>
                  <form.Form
                    label={t({
                      id: "app.managers.dialog.label",
                      message: "Manager",
                    })}
                    className="flex flex-col gap-4"
                  >
                    <form.FormError />
                    <form.AppField name="accountId">
                      {(field) => (
                        <field.AccountPicker
                          label={
                            <Trans id="app.managers.dialog.label">
                              Manager
                            </Trans>
                          }
                          options={accountOptions}
                          loading={candidates.isPending}
                          onSearch={setAccountSearch}
                          variant="secondary"
                        />
                      )}
                    </form.AppField>
                    <form.SubmitButton>
                      <Trans id="app.managers.dialog.save">Assign</Trans>
                    </form.SubmitButton>
                  </form.Form>
                </form.AppForm>
              </Modal.Body>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>

      <ConfirmDialog
        isOpen={target !== undefined}
        onOpenChange={(open) => !open && setRemoving(null)}
        status="danger"
        title={
          <Trans id="app.managers.confirm.title">Remove this manager?</Trans>
        }
        body={
          <p>
            <Trans id="app.managers.confirm.body">
              {target?.displayName ?? ""} will no longer be able to change this
              application. Their account is not affected.
            </Trans>
          </p>
        }
        confirmLabel={<Trans id="app.managers.confirm.action">Remove</Trans>}
        isPending={removeManager.isPending}
        onConfirm={() => {
          if (target === undefined) return;
          removeManager.mutate(
            { appId, accountId: target.id },
            { onSettled: () => setRemoving(null) },
          );
        }}
      />
    </>
  );
}
