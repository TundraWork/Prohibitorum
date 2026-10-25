import { Trans, useLingui } from "@lingui/react/macro";
import { useQuery } from "@tanstack/react-query";
import { TriangleAlertIcon } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { accountSearchQueryOptions } from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { ChoiceTile } from "@/components/custom/ChoiceTile";
import {
  type EntityOption,
  EntityPicker,
} from "@/components/custom/EntityPicker";
import { InlineFailure } from "@/components/custom/InlineFailure";
import {
  Dialog,
  DialogFooter,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";
import { RadioGroup } from "@/components/ui/radio-group";
import type { DecisionEffect } from "@/pages/admin/groups/decision-effect";

/** What the section's list knows, and how to read on when it does not know enough. */
export interface DecisionLookup {
  /** The decisions the loaded pages hold, by account. */
  current: ReadonlyMap<number, DecisionEffect>;
  /** Whether the first page has arrived. */
  loaded: boolean;
  /** Whether the first page is still on its way. */
  loading: boolean;
  hasMore: boolean;
  loadingMore: boolean;
  /** The list's latest failure; while pages remain it is a failed page. */
  error: unknown;
  loadMore: () => void;
}

/**
 * Allow or deny one account, searched in the directory rather than listed.
 *
 * It never closes itself or writes: the section holds the write, so its
 * callbacks outlive the dialog, and dismissal is ignored while it is in flight.
 * An account's current decision is read from the list, which is paged: when
 * the chosen account is not in the pages loaded, the rest are read first, and
 * the submit waits. If that fails the dialog says so and still allows the
 * save, since the server's answer is the truth. An account that already has a
 * decision says so, and saving the one it already has is refused in words.
 */
export function AddDecisionDialog({
  open,
  session,
  decisions,
  pending,
  error,
  onCancel,
  onSubmit,
}: {
  open: boolean;
  /** Changes with each opening, which resets the choice. */
  session: number;
  decisions: DecisionLookup;
  pending: boolean;
  error: unknown;
  onCancel: () => void;
  onSubmit: (accountId: number, effect: DecisionEffect) => void;
}) {
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !next && !pending && onCancel()}
    >
      <DialogPopup className="sm:max-w-md">
        <AddDecisionForm
          key={session}
          decisions={decisions}
          pending={pending}
          error={error}
          onCancel={onCancel}
          onSubmit={onSubmit}
        />
      </DialogPopup>
    </Dialog>
  );
}

function AddDecisionForm({
  decisions,
  pending,
  error,
  onCancel,
  onSubmit,
}: {
  decisions: DecisionLookup;
  pending: boolean;
  error: unknown;
  onCancel: () => void;
  onSubmit: (accountId: number, effect: DecisionEffect) => void;
}) {
  const { t } = useLingui();
  const effectLabelId = useId();
  const noteId = useId();
  const [search, setSearch] = useState("");
  // The chosen option rides along so the field keeps the account's name when
  // a later search no longer lists it.
  const [chosen, setChosen] = useState<{
    id: number;
    name: string;
    option: EntityOption;
  } | null>(null);
  const [effect, setEffect] = useState<DecisionEffect>("allow");
  const [missing, setMissing] = useState(false);
  const candidates = useQuery(accountSearchQueryOptions(search));
  // A disabled account stays listed with the reason, so an administrator
  // looking for one learns why rather than finding it missing.
  const options: EntityOption[] = (candidates.data?.items ?? []).map(
    (account) => ({
      id: String(account.id),
      label: account.displayName || account.username,
      ...(account.displayName ? { identifier: `@${account.username}` } : {}),
      ...(account.disabled
        ? {
            unavailableReason: t({
              id: "app.managers.candidate.disabled",
              message: "Disabled account",
            }),
          }
        : {}),
    }),
  );
  const listed =
    chosen === null || options.some((option) => option.id === chosen.option.id)
      ? options
      : [chosen.option, ...options];

  const name = chosen?.name ?? "";
  const existing =
    chosen === null ? undefined : decisions.current.get(chosen.id);
  // Nothing is known until the first page is in, and an account not in the
  // pages loaded may be on one still to come: read on until it turns up or the
  // pages end. A failed earlier page is tried once per choice before the
  // dialog gives up and says it could not read the decision.
  const [tried, setTried] = useState<number | null>(null);
  const {
    loadMore,
    loadingMore,
    loaded,
    loading,
    hasMore,
    error: listError,
  } = decisions;
  const unknown =
    chosen !== null && existing === undefined && (!loaded || hasMore);
  const firstFailed = !loaded && !loading;
  const walkFailed =
    unknown &&
    listError !== null &&
    (firstFailed || (tried === chosen?.id && !loadingMore));
  const checking = unknown && !walkFailed;
  const walking = checking && loaded;
  useEffect(() => {
    if (!walking || loadingMore || chosen === null) return;
    if (listError !== null && tried === chosen.id) return;
    setTried(chosen.id);
    loadMore();
  }, [walking, loadingMore, listError, tried, chosen, loadMore]);
  const unchanged = existing !== undefined && existing === effect;
  const note = checking
    ? t({
        id: "admin.group.decisions.dialog.checking",
        message: "Checking this account's current decision…",
      })
    : walkFailed
      ? t({
          id: "admin.group.decisions.dialog.check-failed",
          message: `The current decision for ${name} could not be read. Saving may overwrite an existing one.`,
        })
      : existing === undefined
        ? null
        : unchanged
          ? existing === "allow"
            ? t({
                id: "admin.group.decisions.dialog.same-allow",
                message: `${name} is already allowed in this group. Choose Deny to change it.`,
              })
            : t({
                id: "admin.group.decisions.dialog.same-deny",
                message: `${name} is already denied in this group. Choose Allow to change it.`,
              })
          : existing === "allow"
            ? t({
                id: "admin.group.decisions.dialog.change-allow",
                message: `${name} is allowed in this group now. Saving changes it to deny.`,
              })
            : t({
                id: "admin.group.decisions.dialog.change-deny",
                message: `${name} is denied in this group now. Saving changes it to allow.`,
              });

  return (
    <form
      className="flex min-h-0 flex-1 flex-col"
      aria-label={t({
        id: "admin.group.decisions.dialog.title",
        message: "Add an account",
      })}
      onSubmit={(event) => {
        event.preventDefault();
        if (pending) return;
        if (chosen === null) {
          setMissing(true);
          return;
        }
        if (unchanged || checking) return;
        onSubmit(chosen.id, effect);
      }}
    >
      <DialogHeader>
        <DialogTitle>
          <Trans id="admin.group.decisions.dialog.title">Add an account</Trans>
        </DialogTitle>
      </DialogHeader>
      <DialogPanel className="flex flex-col gap-4">
        <InlineFailure error={error} />
        <EntityPicker
          label={
            <Trans id="admin.group.decisions.dialog.account">Account</Trans>
          }
          placeholder={t({
            id: "admin.group.decisions.dialog.placeholder",
            message: "Search accounts",
          })}
          searchPlaceholder={t({
            id: "admin.group.decisions.dialog.placeholder",
            message: "Search accounts",
          })}
          searchLabel={t({ id: "admin.picker.search", message: "Search" })}
          options={listed}
          loading={candidates.isFetching}
          onSearch={setSearch}
          value={chosen === null ? [] : [String(chosen.id)]}
          onValueChange={([id]) => {
            const picked = listed.find((option) => option.id === id);
            setChosen(
              id === undefined
                ? null
                : {
                    id: Number(id),
                    name: picked?.label ?? id,
                    option: picked ?? { id, label: id },
                  },
            );
            setMissing(false);
          }}
          isInvalid={missing}
          errorMessage={t({
            id: "admin.group.decisions.dialog.required",
            message: "Choose an account first.",
          })}
        />
        <div className="flex flex-col gap-2">
          <span id={effectLabelId} className="font-medium text-sm">
            <Trans id="admin.group.decisions.dialog.effect">Decision</Trans>
          </span>
          <RadioGroup
            aria-labelledby={effectLabelId}
            value={effect}
            disabled={pending}
            onValueChange={(next) => setEffect(next as DecisionEffect)}
          >
            <ChoiceTile
              layout="list"
              value="allow"
              label={
                <Trans id="admin.group.decisions.effect.allow">Allow</Trans>
              }
              description={
                <Trans id="admin.group.decisions.dialog.allow-hint">
                  Can use the applications that select this group.
                </Trans>
              }
            />
            <ChoiceTile
              layout="list"
              value="deny"
              label={<Trans id="admin.group.decisions.effect.deny">Deny</Trans>}
              description={
                <Trans id="admin.group.decisions.dialog.deny-hint">
                  Cannot use them, regardless of any other match.
                </Trans>
              }
            />
          </RadioGroup>
        </div>
        {note !== null && (
          <p
            id={noteId}
            // A list that never loaded already reports its failure in the
            // section, so the note there is a status, not a second alert.
            role={
              checking
                ? "status"
                : walkFailed
                  ? firstFailed
                    ? "status"
                    : "alert"
                  : undefined
            }
            className={
              walkFailed
                ? "flex items-start gap-2 text-pretty text-sm text-warning-foreground"
                : "text-pretty text-muted-foreground text-sm"
            }
          >
            {walkFailed && (
              <TriangleAlertIcon
                aria-hidden="true"
                className="mt-0.5 size-4 shrink-0"
              />
            )}
            {note}
          </p>
        )}
      </DialogPanel>
      <DialogFooter>
        <Button variant="ghost" disabled={pending} onClick={onCancel}>
          <Trans id="admin.cancel">Cancel</Trans>
        </Button>
        <Button
          type="submit"
          className="w-full data-refused:opacity-64 sm:w-auto"
          variant={effect === "deny" ? "destructive" : "default"}
          pending={pending}
          data-refused={unchanged || checking || undefined}
          aria-disabled={unchanged || checking || undefined}
          aria-describedby={unchanged || checking ? noteId : undefined}
        >
          {submitLabel(effect, chosen === null ? null : name)}
        </Button>
      </DialogFooter>
    </form>
  );
}

/** The action the button takes, naming the account once one is chosen. */
function submitLabel(effect: DecisionEffect, name: string | null) {
  if (name === null) {
    return effect === "deny" ? (
      <Trans id="admin.group.decisions.dialog.deny-none">Deny</Trans>
    ) : (
      <Trans id="admin.group.decisions.dialog.allow-none">Allow</Trans>
    );
  }
  return effect === "deny" ? (
    <Trans id="admin.group.decisions.dialog.deny-name">Deny {name}</Trans>
  ) : (
    <Trans id="admin.group.decisions.dialog.allow-name">Allow {name}</Trans>
  );
}
