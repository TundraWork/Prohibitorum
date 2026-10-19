import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  IdCard,
  ListFilter,
  Lock,
  LockOpen,
  Plus,
  ShieldCheck,
  X,
} from "lucide-react";
import { type ReactNode, useId, useState } from "react";
import {
  replaceAppGroupsMutationOptions,
  setAppAccessRestrictedMutationOptions,
} from "@/api/mutations";
import { appAccessQueryOptions, groupsQueryOptions } from "@/api/queries";
import type {
  AppGroupView,
  ManagedApplicationKind,
} from "@/api/raw-admin-paths";
import { quietQuery, withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import {
  type EntityOption,
  EntityPicker,
} from "@/components/custom/EntityPicker";
import {
  InlineFailure,
  useInlineFailure,
} from "@/components/custom/InlineFailure";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { Section } from "@/components/custom/Section";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogFooter,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";
import { Switch } from "@/components/ui/switch";

const removeGroupMessage = msg({
  id: "app.groups.remove",
  message: "Remove {group}",
});

const isRule = (group: AppGroupView) => group.kind === "rule";

/** The kind of a group, in words: the badge on its row and the picker's note. */
function kindLabel(group: AppGroupView): ReactNode {
  return isRule(group) ? (
    <Trans id="app.groups.kind.rule">Rule</Trans>
  ) : (
    <Trans id="app.groups.kind.manual">Chosen by hand</Trans>
  );
}

/**
 * Who may use an application: the restriction switch on the heading, the order
 * the checks run in, and the user groups the restriction selects.
 *
 * The same section serves all three application kinds, because the access API
 * is protocol-neutral and only the identifiers in the path differ.
 *
 * The switch writes on flip, behind a confirmation in both directions, and
 * stays at the server's value until the dialog is answered. Nothing is drawn
 * before the policy has been read, so a restricted application never flashes
 * "every account can use this". An open application keeps its selection, shown
 * dimmed without removal, because the selection decides nothing until the
 * application is restricted again.
 *
 * The mutations live here, not in the dialogs: a dismissed dialog would lose a
 * failure, and only a mounted mutation runs its callbacks. The selection is
 * always written whole, so a concurrent change by another administrator is not
 * silently merged. A delegated manager's group read holds only their own
 * groups, so the current selection joins the picker's candidates; a selected
 * group they are not in keeps its name and stays selected.
 */
export function AppAccessSection({
  kind,
  appId,
  isAdmin,
}: {
  kind: ManagedApplicationKind;
  appId: string;
  /** Whether the signed-in account is an administrator. */
  isAdmin: boolean;
}) {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const switchLabel = useId();
  const access = useQuery(quietQuery(appAccessQueryOptions(kind, appId)));
  const groups = useQuery(
    quietQuery({ ...groupsQueryOptions(), enabled: true }),
  );
  const setRestricted = useMutation(
    withInlineReport(
      setAppAccessRestrictedMutationOptions(queryClient, kind),
      true,
    ),
  );
  const replace = useMutation(
    withInlineReport(replaceAppGroupsMutationOptions(queryClient, kind), true),
  );
  const { error, clear, report } = useInlineFailure();
  const dialogFailure = useInlineFailure();
  const [confirming, setConfirming] = useState(false);
  const [picking, setPicking] = useState(false);
  // A new key per opening starts the picker from the current selection.
  const [pickings, setPickings] = useState(0);
  const [removingLast, setRemovingLast] = useState<AppGroupView | null>(null);

  const title = <Trans id="app.access.title">Access</Trans>;

  if (access.isError) {
    return (
      <Section id="access" icon={<ShieldCheck />} title={title}>
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
  const selected: AppGroupView[] = access.data?.groups ?? [];
  const appName = access.data?.app.displayName ?? "";
  const listed = new Set((groups.data ?? []).map((group) => group.id));
  const groupOptions: EntityOption[] = [
    ...(groups.data ?? []),
    ...selected.filter((group) => !listed.has(group.id)),
  ].map((group) => ({
    id: String(group.id),
    label: group.displayName,
    identifier: group.slug,
    description: i18n._(
      isRule(group)
        ? msg({ id: "app.groups.kind.rule", message: "Rule" })
        : msg({ id: "app.groups.kind.manual", message: "Chosen by hand" }),
    ),
  }));

  function writeGroups(groupIds: number[], onSettled?: () => void) {
    clear();
    replace.mutate({ appId, groupIds }, { onError: report, onSettled });
  }

  function remove(group: AppGroupView) {
    if (selected.length === 1) {
      setRemovingLast(group);
      return;
    }
    writeGroups(
      selected
        .filter((other) => other.id !== group.id)
        .map((other) => other.id),
    );
  }

  const note = !restricted && selected.length > 0 && (
    <Trans id="app.access.open.kept">
      The user groups below are kept and apply again when you restrict this
      application.
    </Trans>
  );

  return (
    <>
      <Section
        id="access"
        icon={<ShieldCheck />}
        title={title}
        description={
          restricted ? (
            <Trans id="app.access.order.description">
              Checked in this order. The first step that matches someone
              decides.
            </Trans>
          ) : undefined
        }
        action={
          <div className="flex items-center gap-2 text-sm">
            <span id={switchLabel}>
              <Trans id="app.access.restricted.switch">Restricted</Trans>
            </span>
            <Switch
              aria-labelledby={switchLabel}
              checked={restricted}
              disabled={access.isPending || setRestricted.isPending}
              onCheckedChange={() => setConfirming(true)}
            />
          </div>
        }
      >
        {error !== null && (
          <div className="px-5 pb-3">
            <InlineFailure error={error} />
          </div>
        )}
        {restricted && <OrderStrip groups={selected} />}
        <ItemList
          label={t({ id: "app.groups.list", message: "Selected groups" })}
          loading={access.isPending}
          empty={
            access.data === undefined ? null : (
              <StateRow restricted={restricted} none={selected.length === 0} />
            )
          }
          footer={
            restricted ? (
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  dialogFailure.clear();
                  setPickings((count) => count + 1);
                  setPicking(true);
                }}
              >
                <Plus />
                <Trans id="app.groups.add">Choose groups</Trans>
              </Button>
            ) : undefined
          }
        >
          {access.data !== undefined && !restricted && (
            <ItemListRow
              key="state"
              icon={<LockOpen />}
              title={
                <Trans id="app.access.open">Every account can use this</Trans>
              }
              details={[note]}
            />
          )}
          {selected.map((group) => (
            <ItemListRow
              key={group.id}
              dimmed={!restricted}
              icon={isRule(group) ? <ListFilter /> : <IdCard />}
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
              details={[
                <span key="slug" className="font-mono">
                  {group.slug}
                </span>,
              ]}
              badge={
                <Badge variant={isRule(group) ? "info" : "outline"}>
                  {kindLabel(group)}
                </Badge>
              }
              actions={
                restricted ? (
                  <Button
                    aria-label={i18n._({
                      ...removeGroupMessage,
                      values: { group: group.displayName },
                    })}
                    pending={replace.isPending}
                    onClick={() => remove(group)}
                    variant="ghost"
                    size="icon-sm"
                  >
                    <X />
                  </Button>
                ) : undefined
              }
            />
          ))}
        </ItemList>
      </Section>

      <GroupPickerDialog
        open={picking}
        session={pickings}
        selectedIds={selected.map((group) => String(group.id))}
        options={groupOptions}
        loading={groups.isPending}
        pending={replace.isPending}
        error={dialogFailure.error}
        onCancel={() => setPicking(false)}
        onSave={(ids) => {
          dialogFailure.clear();
          replace.mutate(
            { appId, groupIds: ids.map(Number) },
            {
              onSuccess: () => setPicking(false),
              onError: dialogFailure.report,
            },
          );
        }}
      />

      <ConfirmDialog
        isOpen={removingLast !== null}
        onOpenChange={(open) => !open && setRemovingLast(null)}
        status="warning"
        title={<Trans id="app.groups.last.title">Remove the last group?</Trans>}
        body={
          <p>
            <Trans id="app.groups.last.body">
              Nobody can use {appName} until you select one.
            </Trans>
          </p>
        }
        confirmLabel={<Trans id="app.groups.last.confirm">Remove</Trans>}
        isPending={replace.isPending}
        onConfirm={() => {
          // A failure is shown in the section; the question is answered.
          writeGroups([], () => setRemovingLast(null));
        }}
      />

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
          clear();
          setRestricted.mutate(
            { appId, restricted: target === "restricted" },
            { onError: report, onSettled: () => setConfirming(false) },
          );
        }}
      />
    </>
  );
}

/** What an empty selection means, said where the groups would be listed. */
function StateRow({
  restricted,
  none,
}: {
  restricted: boolean;
  none: boolean;
}) {
  if (!restricted || !none) return null;
  return (
    <div className="flex items-center gap-3 px-4 py-4 text-muted-foreground text-sm">
      <Lock aria-hidden="true" className="size-4 shrink-0" />
      <Trans id="app.access.restricted.none">
        No user groups selected yet. Nobody can use this application until you
        select one.
      </Trans>
    </div>
  );
}

/**
 * The four checks in the order they run, each with the selected groups that
 * take part: manual groups hold the by-hand decisions, rule groups the rule.
 */
function OrderStrip({ groups }: { groups: AppGroupView[] }) {
  const { t, i18n } = useLingui();
  const names = (list: AppGroupView[]): ReactNode =>
    list.length === 0
      ? t({ id: "app.access.order.none", message: "None selected" })
      : new Intl.ListFormat(i18n.locale, { type: "conjunction" }).format(
          list.map((group) => group.displayName),
        );
  const manual = groups.filter((group) => !isRule(group));
  const rules = groups.filter(isRule);
  const steps: { key: string; title: ReactNode; detail: ReactNode }[] = [
    {
      key: "denied",
      title: (
        <Trans id="app.access.order.denied">Accounts denied by hand in</Trans>
      ),
      detail: names(manual),
    },
    {
      key: "allowed",
      title: (
        <Trans id="app.access.order.allowed">Accounts allowed by hand in</Trans>
      ),
      detail: names(manual),
    },
    {
      key: "rule",
      title: <Trans id="app.access.order.rule">Matches a rule</Trans>,
      detail: names(rules),
    },
    {
      key: "rest",
      title: <Trans id="app.access.order.rest">Everyone else</Trans>,
      detail: t({
        id: "app.access.order.rest.result",
        message: "Can't sign in",
      }),
    },
  ];
  return (
    <div className="px-5 pb-3">
      <ol
        aria-label={t({
          id: "app.access.order.label",
          message: "Order of checks",
        })}
        className="grid grid-cols-2 overflow-hidden rounded-xl bg-muted/60 ring-1 ring-border/60 sm:grid-cols-4"
      >
        {steps.map((step, index) => (
          <li
            key={step.key}
            className="border-t px-3 py-2.5 nth-[-n+2]:border-t-0 even:border-s sm:border-t-0 sm:border-s sm:first:border-s-0"
          >
            <div className="flex items-center gap-2 font-medium text-sm">
              <span
                aria-hidden="true"
                className="flex size-5 shrink-0 items-center justify-center rounded-full bg-background text-muted-foreground text-xs shadow-xs/5 ring-1 ring-border"
              >
                {index + 1}
              </span>
              {step.title}
            </div>
            <div
              data-tone={step.key === "rest" ? "danger" : undefined}
              className="mt-0.5 text-muted-foreground text-xs data-[tone=danger]:text-destructive-foreground"
            >
              {step.detail}
            </div>
          </li>
        ))}
      </ol>
    </div>
  );
}

/**
 * The picker for the selected groups. It replaces the whole selection, which
 * the hint says, because the control alone reads as "add".
 */
function GroupPickerDialog({
  open,
  session,
  selectedIds,
  options,
  loading,
  pending,
  error,
  onCancel,
  onSave,
}: {
  open: boolean;
  /** Changes with each opening, which resets the draft. */
  session: number;
  selectedIds: string[];
  options: EntityOption[];
  loading: boolean;
  pending: boolean;
  error: unknown;
  onCancel: () => void;
  onSave: (ids: string[]) => void;
}) {
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !next && !pending && onCancel()}
    >
      <DialogPopup className="sm:max-w-md">
        <GroupPickerForm
          key={session}
          initial={selectedIds}
          options={options}
          loading={loading}
          pending={pending}
          error={error}
          onCancel={onCancel}
          onSave={onSave}
        />
      </DialogPopup>
    </Dialog>
  );
}

function GroupPickerForm({
  initial,
  options,
  loading,
  pending,
  error,
  onCancel,
  onSave,
}: {
  initial: string[];
  options: EntityOption[];
  loading: boolean;
  pending: boolean;
  error: unknown;
  onCancel: () => void;
  onSave: (ids: string[]) => void;
}) {
  const { t } = useLingui();
  const [ids, setIds] = useState(initial);
  return (
    <form
      className="flex min-h-0 flex-1 flex-col"
      aria-label={t({
        id: "app.groups.dialog.title",
        message: "Choose the groups that take part",
      })}
      onSubmit={(event) => {
        event.preventDefault();
        if (!pending) onSave(ids);
      }}
    >
      <DialogHeader>
        <DialogTitle>
          <Trans id="app.groups.dialog.title">
            Choose the groups that take part
          </Trans>
        </DialogTitle>
      </DialogHeader>
      <DialogPanel className="flex flex-col gap-4">
        <p className="text-muted-foreground text-sm">
          <Trans id="app.groups.dialog.hint">
            The selection replaces the current one. Only accounts in the groups
            you choose can use this application.
          </Trans>
        </p>
        <InlineFailure error={error} />
        <EntityPicker
          multiple
          label={<Trans id="app.groups.dialog.label">User groups</Trans>}
          placeholder={t({
            id: "app.groups.dialog.placeholder",
            message: "Search user groups",
          })}
          searchPlaceholder={t({
            id: "app.groups.dialog.placeholder",
            message: "Search user groups",
          })}
          searchLabel={t({ id: "admin.picker.search", message: "Search" })}
          options={options}
          loading={loading}
          value={ids}
          onValueChange={setIds}
        />
      </DialogPanel>
      <DialogFooter>
        <Button variant="ghost" disabled={pending} onClick={onCancel}>
          <Trans id="admin.cancel">Cancel</Trans>
        </Button>
        <Button type="submit" pending={pending}>
          <Trans id="app.groups.dialog.save">Save</Trans>
        </Button>
      </DialogFooter>
    </form>
  );
}
