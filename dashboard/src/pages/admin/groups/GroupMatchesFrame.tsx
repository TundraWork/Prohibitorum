import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { UsersIcon } from "lucide-react";
import { useState } from "react";
import { useCursorList } from "@/api/cursor-list";
import { groupMatchesListOptions } from "@/api/queries";
import type { AppAccessRule, GroupPreviewView } from "@/api/raw-admin-paths";
import { Button } from "@/components/custom/Button";
import { EntityAvatar } from "@/components/custom/EntityAvatar";
import { InlineFailure } from "@/components/custom/InlineFailure";
import { Section } from "@/components/custom/Section";
import { getTopLayerContainer } from "@/components/custom/top-layer";
import {
  Dialog,
  DialogDescription,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";

const shownAvatars = 5;

const heading = {
  id: "matches",
  title: <Trans id="admin.group.matches.title">Matches right now</Trans>,
  description: (
    <Trans id="admin.group.matches.description">From the saved rule</Trans>
  ),
  icon: <UsersIcon />,
};

/** A person's name when they have one, else the username they sign in with. */
const nameOf = ({ account }: GroupPreviewView) =>
  account.displayName || account.username;

/**
 * Who the saved rule matches today: the true count, a few faces and the list
 * behind "See all".
 *
 * The rule is evaluated against every active account by the server, which
 * reports the total on each page and pages the accounts it checked, matched or
 * not. The faces and the list therefore show the matches among the pages
 * loaded so far.
 */
export function GroupMatchesFrame({
  groupId,
  rule,
}: {
  groupId: number;
  rule: AppAccessRule;
}) {
  const list = useCursorList<GroupPreviewView, { matchedCount: number }>(
    groupMatchesListOptions(groupId, rule),
  );
  const [open, setOpen] = useState(false);
  const total = list.pages[0]?.matchedCount;
  const matches = list.items.filter((item) => item.matched);
  const count = total ?? 0;
  const rest = count - shownAvatars;

  if (list.error !== null && total === undefined) {
    return (
      <Section {...heading}>
        <InlineFailure error={list.error} />
      </Section>
    );
  }
  if (total === undefined) {
    return (
      <Section {...heading}>
        <div aria-hidden="true" className="flex flex-col gap-3">
          <Skeleton className="h-9 w-24" />
          <Skeleton className="h-7 w-40" />
        </div>
      </Section>
    );
  }

  return (
    <Section {...heading}>
      <div className="flex flex-wrap items-baseline gap-x-2">
        <span
          data-slot="match-total"
          className="font-semibold text-4xl tabular-nums tracking-tight"
        >
          {total}
        </span>
        <span className="text-muted-foreground text-sm">
          <Plural
            id="admin.group.matches.unit"
            value={count}
            one="account matches"
            other="accounts match"
          />
        </span>
      </div>
      {total === 0 ? (
        <p className="text-muted-foreground text-sm">
          <Trans id="admin.group.matches.none">
            No account matches the saved rule.
          </Trans>
        </p>
      ) : (
        <div className="flex items-center gap-2">
          <MatchAvatars matches={matches.slice(0, shownAvatars)} />
          {rest > 0 && (
            <span className="text-muted-foreground text-xs tabular-nums">
              +{rest}
            </span>
          )}
          <Button
            variant="ghost"
            size="sm"
            className="ms-auto"
            onClick={() => setOpen(true)}
          >
            <Trans id="admin.group.matches.see-all">See all</Trans>
          </Button>
        </div>
      )}
      <MatchesDialog
        open={open}
        onOpenChange={setOpen}
        matches={matches}
        total={total}
        hasMore={list.hasMore}
        loadingMore={list.loadingMore}
        loadMore={list.loadMore}
        error={list.error}
      />
    </Section>
  );
}

function MatchAvatars({ matches }: { matches: GroupPreviewView[] }) {
  const { t } = useLingui();
  return (
    <ul
      aria-label={t({
        id: "admin.group.matches.sample",
        message: "Some of the matching accounts",
      })}
      className="flex -space-x-1.5 *:ring-2 *:ring-card"
    >
      {matches.map((match) => (
        <li key={match.account.id} className="rounded-full">
          <EntityAvatar size="md" shape="round" name={nameOf(match)} />
          <span className="sr-only">{nameOf(match)}</span>
        </li>
      ))}
    </ul>
  );
}

function MatchesDialog({
  open,
  onOpenChange,
  matches,
  total,
  hasMore,
  loadingMore,
  loadMore,
  error,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  matches: GroupPreviewView[];
  total: number;
  hasMore: boolean;
  loadingMore: boolean;
  loadMore: () => void;
  error: unknown;
}) {
  const shown = matches.length;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogPopup
        className="sm:max-w-md"
        portalProps={{ container: getTopLayerContainer() }}
      >
        <DialogHeader>
          <DialogTitle>
            <Trans id="admin.group.matches.dialog.title">
              Accounts that match
            </Trans>
          </DialogTitle>
          {hasMore && (
            <DialogDescription>
              <Trans id="admin.group.matches.dialog.partial">
                Showing {shown} of {total} matching accounts so far.
              </Trans>
            </DialogDescription>
          )}
        </DialogHeader>
        <DialogPanel className="flex flex-col gap-3">
          <ul className="flex flex-col divide-y">
            {matches.map((match) => (
              <li
                key={match.account.id}
                className="flex items-center gap-3 py-2 first:pt-0 last:pb-0"
              >
                <EntityAvatar size="md" shape="round" name={nameOf(match)} />
                <span className="flex min-w-0 flex-col">
                  {match.account.displayName !== "" && (
                    <span className="truncate text-sm">
                      {match.account.displayName}
                    </span>
                  )}
                  <span className="truncate font-mono text-muted-foreground text-xs">
                    @{match.account.username}
                  </span>
                </span>
              </li>
            ))}
          </ul>
          <InlineFailure error={error} />
          {hasMore && (
            <Button variant="outline" pending={loadingMore} onClick={loadMore}>
              <Trans id="admin.group.matches.load-more">Load more</Trans>
            </Button>
          )}
        </DialogPanel>
      </DialogPopup>
    </Dialog>
  );
}
