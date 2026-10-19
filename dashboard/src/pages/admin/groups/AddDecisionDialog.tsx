import { Trans, useLingui } from "@lingui/react/macro";
import { useQuery } from "@tanstack/react-query";
import { useId, useState } from "react";
import { accountSearchQueryOptions } from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { ChoiceTile } from "@/components/custom/ChoiceTile";
import {
  type EntityOption,
  EntityPicker,
} from "@/components/custom/EntityPicker";
import { InlineFailure } from "@/components/custom/InlineFailure";
import { getTopLayerContainer } from "@/components/custom/top-layer";
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

/**
 * Allow or deny one account, searched in the directory rather than listed.
 *
 * It never closes itself or writes: the section holds the write, so its
 * callbacks outlive the dialog, and dismissal is ignored while it is in flight.
 * An account that already has a decision, as far as the loaded pages show,
 * says so, and saving the decision it already has is refused in words.
 */
export function AddDecisionDialog({
  open,
  session,
  current,
  pending,
  error,
  onCancel,
  onSubmit,
}: {
  open: boolean;
  /** Changes with each opening, which resets the choice. */
  session: number;
  /** The decisions the loaded pages hold, by account. */
  current: ReadonlyMap<number, DecisionEffect>;
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
      <DialogPopup
        className="sm:max-w-md"
        portalProps={{ container: getTopLayerContainer() }}
      >
        <AddDecisionForm
          key={session}
          current={current}
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
  current,
  pending,
  error,
  onCancel,
  onSubmit,
}: {
  current: ReadonlyMap<number, DecisionEffect>;
  pending: boolean;
  error: unknown;
  onCancel: () => void;
  onSubmit: (accountId: number, effect: DecisionEffect) => void;
}) {
  const { t } = useLingui();
  const effectLabelId = useId();
  const noteId = useId();
  const [search, setSearch] = useState("");
  const [chosen, setChosen] = useState<{ id: number; name: string } | null>(
    null,
  );
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
              id: "admin.group.decisions.candidate.disabled",
              message: "Disabled account",
            }),
          }
        : {}),
    }),
  );

  const name = chosen?.name ?? "";
  const existing = chosen === null ? undefined : current.get(chosen.id);
  const unchanged = existing !== undefined && existing === effect;
  const note =
    existing === undefined
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
        if (unchanged) return;
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
          searchLabel={t({ id: "admin.picker.search", message: "Search" })}
          options={options}
          loading={candidates.isFetching}
          onSearch={setSearch}
          value={chosen === null ? [] : [String(chosen.id)]}
          onValueChange={([id]) => {
            const picked = options.find((option) => option.id === id);
            setChosen(
              id === undefined
                ? null
                : { id: Number(id), name: picked?.label ?? id },
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
                  Cannot use them, whatever else matches.
                </Trans>
              }
            />
          </RadioGroup>
        </div>
        {note !== null && (
          <p id={noteId} className="text-muted-foreground text-sm">
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
          className="w-full sm:w-auto"
          variant={effect === "deny" ? "destructive" : "default"}
          pending={pending}
          aria-disabled={unchanged || undefined}
          aria-describedby={unchanged ? noteId : undefined}
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
