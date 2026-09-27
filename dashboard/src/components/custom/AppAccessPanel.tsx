import { Description, Modal, Switch } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Lock, LockOpen, Pencil, Trash2, UserPlus } from "lucide-react";
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
import { Section } from "@/components/custom/Section";
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
 * ## Two sections, and why the switch is in a heading
 *
 * `AppAccess` is one section: the restriction is a switch on the heading row,
 * and the user groups it applies to are the card below it. The two are one
 * decision — a selection of groups decides nothing while the application is
 * open — so they are read together and saved apart, each through the endpoint
 * that owns it.
 *
 * The switch writes on flip rather than on a submit, which is why its handler
 * lives beside the state it reads. Opening an application back up is reversible
 * and runs from the switch; restricting it is not, because until a group is
 * selected nobody can reach the application at all, so that direction asks
 * first and the switch is held back until the dialog is answered.
 *
 * The groups are the card's own rows, so the card keeps one shape whether or
 * not a group is selected: an empty state where the rows would be, and the
 * "Add user groups" action on the card's footer. That action is drawn only
 * while the application is restricted — an open application ignores the
 * selection, and offering to change it would imply otherwise.
 *
 * `AppManagers` is a section of its own because only an administrator sees it:
 * the endpoint behind it is administrator-only, so for a delegated manager the
 * panel draws nothing at all and never asks.
 *
 * ## Administrators and delegated managers see different things
 *
 * A manager assigned to the application reaches the access section too. The
 * group picker reads `GET /groups`, which for a non-administrator answers with
 * their own memberships rather than the whole directory. That is exactly the set
 * the server will accept from them, so the candidate list is right without the
 * console having to filter it.
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

/**
 * The access policy of one application: the restriction switch on the heading
 * row, and the user groups the restriction selects.
 */
export function AppAccess({
  kind,
  appId,
  isAdmin,
}: {
  kind: ManagedApplicationKind;
  appId: string;
  /** Whether the signed-in account is an administrator. */
  isAdmin: boolean;
}) {
  return <AccessRestrictionKind kind={kind} appId={appId} isAdmin={isAdmin} />;
}

/**
 * The accounts allowed to manage the application, as a section of its own.
 *
 * Drawn only for an administrator, and the section is not drawn at all for
 * anyone else: the endpoint behind it answers an administrator alone, so asking
 * on a manager's behalf would 403 and an empty section would say the
 * application has no managers when the truth is that they cannot be seen.
 *
 * The action that assigns one is on the heading row, where every other section
 * keeps its action. It stays there when the list is empty, so the way to add
 * the first manager is in the same place as the way to add the fifth.
 */
export function AppManagers({
  kind,
  appId,
  isAdmin,
}: {
  kind: ManagedApplicationKind;
  appId: string;
  isAdmin: boolean;
}) {
  const { t } = useLingui();
  const [adding, setAdding] = useState(false);

  if (!isAdmin) return null;

  return (
    <>
      <Section
        title={<Trans id="app.managers.label">Managers</Trans>}
        action={
          <Button size="sm" variant="secondary" onPress={() => setAdding(true)}>
            <UserPlus size={16} aria-hidden="true" />
            <Trans id="app.managers.assign">Assign a manager</Trans>
          </Button>
        }
      >
        <ApplicationManagersKind kind={kind} appId={appId} />
      </Section>

      <AssignManagerDialog
        kind={kind}
        appId={appId}
        label={t({ id: "app.managers.dialog.label", message: "Manager" })}
        isOpen={adding}
        onOpenChange={setAdding}
      />
    </>
  );
}
/**
 * Whether the application is open to everyone or limited to selected groups,
 * with those groups as the card under the switch.
 *
 * The switch is the only control on the heading row and it is held at the
 * server's own value while a write is in flight, so it never shows a state the
 * server has not accepted. A failed write leaves it where it was and says why
 * in the card below.
 */
function AccessRestrictionKind({
  kind,
  appId,
  isAdmin,
}: {
  kind: ManagedApplicationKind;
  appId: string;
  isAdmin: boolean;
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
      <Section
        title={
          <Trans id="app.access.restriction.label">Access restriction</Trans>
        }
      >
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
      </Section>
    );
  }

  const restricted = access.data?.accessRestricted ?? false;
  const target = restricted ? "open" : "restricted";

  return (
    <>
      <Section
        title={
          <Trans id="app.access.restriction.label">Access restriction</Trans>
        }
        action={
          <Switch
            aria-label={t({
              id: "app.access.restriction.label",
              message: "Access restriction",
            })}
            size="sm"
            isSelected={restricted}
            isDisabled={access.isPending || setRestricted.isPending}
            onChange={() => setConfirming(true)}
          >
            <Switch.Content>
              <Switch.Control>
                <Switch.Thumb />
              </Switch.Control>
            </Switch.Content>
          </Switch>
        }
      >
        <AccessSectionBody
          kind={kind}
          appId={appId}
          isAdmin={isAdmin}
          restricted={restricted}
        />
      </Section>

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

/**
 * The card under the switch: what the current setting means, the groups it
 * selects, and the way into editing them.
 *
 * The meaning is stated as a row rather than as a sentence on the page,
 * because it is the answer to the question the switch asks — the icon and the
 * wording change with the setting, and a reader who flipped the switch reads
 * the result in the place they flipped it.
 */
function AccessSectionBody({
  kind,
  appId,
  isAdmin,
  restricted,
}: {
  kind: ManagedApplicationKind;
  appId: string;
  isAdmin: boolean;
  restricted: boolean;
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
        label={t({
          id: "app.access.restriction.label",
          message: "Access restriction",
        })}
        loading={access.isPending}
        empty={
          <p className="px-4 py-6 text-center text-sm text-muted">
            <Trans id="app.groups.empty">No user groups selected yet</Trans>
          </p>
        }
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
            // Choosing the groups is only meaningful while the application is
            // restricted: an open application ignores the selection, and
            // offering to change it would imply otherwise.
            restricted ? (
              <Button
                size="sm"
                variant="secondary"
                onPress={() => {
                  form.reset();
                  form.setFieldValue(
                    "groupIds",
                    selected.map((group) => String(group.id)),
                  );
                  setEditing(true);
                }}
              >
                <Pencil size={16} aria-hidden="true" />
                <Trans id="app.groups.select">Select user groups</Trans>
              </Button>
            ) : undefined
          }
        />

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
              // The same rule as the action above: while the application is
              // open the selection decides nothing, so a row does not offer to
              // edit it either.
              restricted ? (
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
              ) : undefined
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
                  <Trans id="app.groups.dialog.title">Select user groups</Trans>
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

                    {/* Says what saving does, because the control above the
                        list does not: the picker opens on the current
                        selection, and the write replaces the whole of it
                        rather than adding to it. */}
                    <Description className="text-sm text-muted">
                      <Trans id="app.groups.dialog.hint">
                        The selection replaces the current one. Only accounts in
                        the groups you choose can use this application.
                      </Trans>
                    </Description>

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
  const removeManager = useMutation(
    removeAppManagerMutationOptions(queryClient, kind),
  );
  const [removing, setRemoving] = useState<number | null>(null);

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
              {target?.displayName ?? ""} will no longer be able to change this
              application. Their account is not affected.
            </Trans>
          </p>
        }
        confirmLabel={<Trans id="app.managers.confirm.action">Remove</Trans>}
        isPending={removeManager.isPending}
        onConfirm={() => {
          if (removing === null) return;
          removeManager.mutate(
            { appId, accountId: removing },
            { onSettled: () => setRemoving(null) },
          );
        }}
      />
    </>
  );
}

/**
 * Assigning one account as a manager, in a dialog of its own.
 *
 * A dialog rather than a control in the list because the account directory is
 * searched rather than browsed — it pages, and an administrator knows the
 * username they are looking for. The dialog is owned by `AppManagers` rather
 * than by the list it writes to, because the action that opens it is on the
 * section's heading row.
 */
function AssignManagerDialog({
  kind,
  appId,
  label,
  isOpen,
  onOpenChange,
}: {
  kind: ManagedApplicationKind;
  appId: string;
  label: string;
  isOpen: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const assign = useMutation(
    assignAppManagerMutationOptions(queryClient, kind),
  );
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
        onOpenChange(false);
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, {
          codes: { invalid_manager_role: "accountId" },
          locations: {},
        });
      }
    },
  });

  return (
    <Modal isOpen={isOpen} onOpenChange={onOpenChange}>
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
                <form.Form label={label} className="flex flex-col gap-4">
                  <form.FormError />
                  <form.AppField name="accountId">
                    {(field) => (
                      <field.AccountPicker
                        label={
                          <Trans id="app.managers.dialog.label">Manager</Trans>
                        }
                        options={accountOptions}
                        loading={candidates.isFetching}
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
  );
}
