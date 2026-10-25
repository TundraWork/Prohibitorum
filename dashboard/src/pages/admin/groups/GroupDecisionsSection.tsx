import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { MoreHorizontalIcon, UserPlusIcon, UsersIcon } from "lucide-react";
import { useState } from "react";
import { useCursorList } from "@/api/cursor-list";
import {
  clearDecisionMutationOptions,
  upsertDecisionMutationOptions,
} from "@/api/mutations";
import { groupDecisionsListOptions } from "@/api/queries";
import type { ManualDecisionView } from "@/api/raw-admin-paths";
import { withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { EntityAvatar } from "@/components/custom/EntityAvatar";
import {
  InlineFailure,
  useInlineFailure,
} from "@/components/custom/InlineFailure";
import {
  ItemList,
  ItemListEmpty,
  ItemListRow,
} from "@/components/custom/ItemList";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { RowMenu } from "@/components/custom/RowMenu";
import { Section } from "@/components/custom/Section";
import { MenuItem } from "@/components/ui/menu";
import { AddDecisionDialog } from "@/pages/admin/groups/AddDecisionDialog";
import {
  type DecisionEffect,
  EffectBadge,
  effectOf,
} from "@/pages/admin/groups/decision-effect";

const rowMenuLabel = msg({
  id: "admin.group.decisions.row-menu",
  message: "More actions for {name}",
});

const nameOf = ({ account }: ManualDecisionView) =>
  account.displayName || account.username;

/**
 * A username with a place to wrap after each dot, dash and underscore, so a
 * long handle on a narrow screen breaks between its words rather than inside
 * one. The `@` stays with the first word.
 */
const breakable = (username: string) => {
  let offset = 0;
  return username.split(/(?<=[._-])/).flatMap((part) => {
    const at = offset;
    offset += part.length;
    return at === 0 ? [part] : [<wbr key={at} />, part];
  });
};

/**
 * Who a manual group allows and denies, one row per account with a decision.
 *
 * An account with no row is neutral, so "clear" is how an account goes back to
 * that. The writes are held here, not in the dialog or the row that starts one:
 * only a mounted mutation runs its callbacks. The add dialog cannot be
 * dismissed while its write is in flight, so a refusal is read inside it; a
 * switch or a clear shows its failure in the section. Both end by reading the
 * list again, through the invalidation the mutations carry.
 */
export function GroupDecisionsSection({ groupId }: { groupId: number }) {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const list = useCursorList<ManualDecisionView>(
    groupDecisionsListOptions(groupId),
  );
  const upsert = useMutation(
    withInlineReport(upsertDecisionMutationOptions(queryClient), true),
  );
  const clear = useMutation(
    withInlineReport(clearDecisionMutationOptions(queryClient), true),
  );
  const addFailure = useInlineFailure();
  const rowFailure = useInlineFailure();
  const [adding, setAdding] = useState(false);
  // A new key per opening starts the dialog from an empty choice.
  const [addings, setAddings] = useState(0);
  const [clearing, setClearing] = useState<ManualDecisionView | null>(null);

  const current = new Map<number, DecisionEffect>(
    list.items.map((decision) => [decision.account.id, effectOf(decision)]),
  );
  // A second row write would take over the first one's callbacks, so every
  // menu waits while one is in flight; the row it is for shows the spinner.
  const writing = upsert.isPending || clear.isPending;
  const busyAccount = upsert.isPending
    ? upsert.variables.body.accountId
    : clear.isPending
      ? clear.variables.body.accountId
      : undefined;
  const clearingName = clearing === null ? "" : nameOf(clearing);

  const change = (decision: ManualDecisionView, effect: DecisionEffect) => {
    rowFailure.clear();
    upsert.mutate(
      { groupId, body: { accountId: decision.account.id, effect } },
      { onError: rowFailure.report },
    );
  };
  const clearDecision = (decision: ManualDecisionView, after?: () => void) => {
    rowFailure.clear();
    clear.mutate(
      { groupId, body: { accountId: decision.account.id } },
      { onError: rowFailure.report, onSettled: after },
    );
  };

  return (
    <>
      <Section
        id="decisions"
        icon={<UsersIcon />}
        title={
          <Trans id="admin.group.decisions.title">
            Allowed and denied accounts
          </Trans>
        }
        description={
          <Trans id="admin.group.decisions.description">
            Allow or deny individual accounts. Allowed accounts can use the
            applications that select this group. Denied accounts cannot, and a
            deny overrides any other match. Other accounts are not affected by
            this group.
          </Trans>
        }
        action={
          <Button
            onClick={() => {
              addFailure.clear();
              setAddings((count) => count + 1);
              setAdding(true);
            }}
            variant="outline"
            disabled={writing}
          >
            <UserPlusIcon />
            <Trans id="admin.group.decisions.add">Add an account</Trans>
          </Button>
        }
      >
        {rowFailure.error !== null && (
          <InlineFailure error={rowFailure.error} />
        )}
        {list.error !== null && list.items.length === 0 ? (
          <div className="flex flex-col items-start gap-3 p-4">
            <InlineFailure error={list.error} />
            <Button variant="outline" onClick={list.refetch}>
              <Trans id="admin.group.decisions.retry">Try again</Trans>
            </Button>
          </div>
        ) : (
          <ItemList
            label={t({
              id: "admin.group.decisions.label",
              message: "Decisions",
            })}
            loading={list.loading}
            empty={
              <ItemListEmpty>
                <Trans id="admin.group.decisions.empty">
                  No accounts have been allowed or denied yet.
                </Trans>
              </ItemListEmpty>
            }
            footer={
              list.hasMore ? (
                <div className="flex flex-col items-center gap-3">
                  <InlineFailure error={list.error} />
                  <Button
                    variant="outline"
                    pending={list.loadingMore}
                    onClick={list.loadMore}
                  >
                    <Trans id="list.load-more">Load more</Trans>
                  </Button>
                </div>
              ) : undefined
            }
          >
            {list.items.map((decision) => {
              const effect = effectOf(decision);
              const name = nameOf(decision);
              const busy = busyAccount === decision.account.id;
              return (
                <ItemListRow
                  key={decision.account.id}
                  iconBare
                  icon={
                    <EntityAvatar
                      name={name}
                      shape="round"
                      className="size-9"
                    />
                  }
                  title={name}
                  details={
                    decision.account.displayName
                      ? [
                          <span key="username" className="font-mono">
                            @{breakable(decision.account.username)}
                          </span>,
                        ]
                      : undefined
                  }
                  badge={<EffectBadge effect={effect} />}
                  meta={
                    <Trans id="admin.group.decisions.updated">
                      Updated <RelativeTime value={decision.updatedAt} />
                    </Trans>
                  }
                  actions={
                    writing ? (
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        pending={busy}
                        disabled={!busy}
                        aria-label={i18n._({
                          ...rowMenuLabel,
                          values: { name },
                        })}
                      >
                        <MoreHorizontalIcon />
                      </Button>
                    ) : (
                      <RowMenu
                        label={i18n._({ ...rowMenuLabel, values: { name } })}
                      >
                        <MenuItem
                          onClick={() =>
                            change(
                              decision,
                              effect === "allow" ? "deny" : "allow",
                            )
                          }
                        >
                          {effect === "allow" ? (
                            <Trans id="admin.group.decisions.to-deny">
                              Change to deny
                            </Trans>
                          ) : (
                            <Trans id="admin.group.decisions.to-allow">
                              Change to allow
                            </Trans>
                          )}
                        </MenuItem>
                        <MenuItem
                          onClick={() =>
                            // A deny may be what keeps an account out, so
                            // lifting it is confirmed; an allow is not.
                            effect === "deny"
                              ? setClearing(decision)
                              : clearDecision(decision)
                          }
                        >
                          <Trans id="admin.group.decisions.clear">Clear</Trans>
                        </MenuItem>
                      </RowMenu>
                    )
                  }
                />
              );
            })}
          </ItemList>
        )}
      </Section>

      <AddDecisionDialog
        open={adding}
        session={addings}
        decisions={{
          current,
          loaded: list.pages.length > 0,
          loading: list.loading,
          hasMore: list.hasMore,
          loadingMore: list.loadingMore,
          error: list.error,
          loadMore: list.loadMore,
        }}
        pending={upsert.isPending && adding}
        error={addFailure.error}
        onCancel={() => setAdding(false)}
        onSubmit={(accountId, effect) => {
          addFailure.clear();
          upsert.mutate(
            { groupId, body: { accountId, effect } },
            {
              onSuccess: () => setAdding(false),
              onError: addFailure.report,
            },
          );
        }}
      />

      <ConfirmDialog
        isOpen={clearing !== null}
        onOpenChange={(open) => !open && !clear.isPending && setClearing(null)}
        status="warning"
        title={
          <Trans id="admin.group.decisions.clear.title">Clear this deny?</Trans>
        }
        subject={clearingName !== "" && clearingName}
        body={
          <p>
            <Trans id="admin.group.decisions.clear.body">
              The account is no longer denied, but may still get access through
              another group or rule.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.group.decisions.clear.action">Clear</Trans>
        }
        isPending={clear.isPending}
        onConfirm={() => {
          if (clearing !== null)
            clearDecision(clearing, () => setClearing(null));
        }}
      />
    </>
  );
}
