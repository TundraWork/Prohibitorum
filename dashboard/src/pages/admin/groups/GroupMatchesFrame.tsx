import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { UsersIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
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
/** Pages the frame reads on its own to find faces before it settles for none. */
const faceSearchPages = 3;

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
  const faces = matches.slice(0, shownAvatars);
  const rest = count - faces.length;
  const { hasMore, loadMore, loadingMore, error } = list;

  // The server pages every account, matched or not, so a rule that matches few
  // can leave the first page without a face: read on, within a bound.
  const wanted = Math.min(shownAvatars, count);
  useEffect(() => {
    if (total === undefined || !hasMore || loadingMore || error !== null)
      return;
    if (matches.length >= wanted || list.pages.length >= faceSearchPages)
      return;
    loadMore();
  }, [
    total,
    hasMore,
    loadingMore,
    error,
    matches.length,
    wanted,
    list.pages.length,
    loadMore,
  ]);

  if (error !== null && total === undefined) {
    return (
      <Section {...heading}>
        <InlineFailure error={error} />
        <Button variant="outline" size="sm" onClick={list.refetch}>
          <Trans id="admin.group.matches.retry">Try again</Trans>
        </Button>
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
      {error !== null && (
        <p className="text-muted-foreground text-xs">
          <Trans id="admin.group.matches.stale">
            The latest check failed, so this may be out of date.
          </Trans>
        </p>
      )}
      {count === 0 ? (
        <p className="text-muted-foreground text-sm">
          <Trans id="admin.group.matches.none">
            No account matches the saved rule.
          </Trans>
        </p>
      ) : (
        <div className="flex items-center gap-2">
          {faces.length > 0 && <MatchAvatars matches={faces} />}
          {faces.length > 0 && rest > 0 && (
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
        hasMore={hasMore}
        loadingMore={loadingMore}
        loadMore={loadMore}
        error={error}
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

/**
 * "Load more" for a list of matches inside pages of every account: one press
 * reads on until a match arrives or the pages end, so it never adds nothing.
 * A failed page stops the search rather than repeating it.
 */
function useSeekMatch({
  shown,
  hasMore,
  loadingMore,
  loadMore,
  error,
}: {
  shown: number;
  hasMore: boolean;
  loadingMore: boolean;
  loadMore: () => void;
  error: unknown;
}) {
  const [target, setTarget] = useState<number | null>(null);
  const asked = useRef(false);
  useEffect(() => {
    if (target === null || loadingMore) return;
    if (shown >= target || !hasMore || (asked.current && error !== null)) {
      setTarget(null);
      return;
    }
    asked.current = true;
    loadMore();
  }, [target, shown, hasMore, loadingMore, error, loadMore]);
  return {
    seeking: target !== null,
    start: () => {
      asked.current = false;
      setTarget(shown + 1);
    },
  };
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
  // Every match found: the pages left hold only accounts that do not match.
  const more = hasMore && shown < total;
  const seek = useSeekMatch({
    shown,
    hasMore: more,
    loadingMore,
    loadMore,
    error,
  });
  const popup = useRef<HTMLDivElement>(null);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogPopup
        ref={popup}
        className="sm:max-w-md"
        // The list is read from its first account; focusing "Load more" would
        // scroll the panel to its end.
        initialFocus={() => popup.current ?? true}
        portalProps={{ container: getTopLayerContainer() }}
      >
        <DialogHeader>
          <DialogTitle>
            <Trans id="admin.group.matches.dialog.title">
              Accounts that match
            </Trans>
          </DialogTitle>
          {more && (
            <DialogDescription>
              <Plural
                id="admin.group.matches.dialog.partial"
                value={total}
                one={`Showing ${shown} of # matching account so far.`}
                other={`Showing ${shown} of # matching accounts so far.`}
              />
            </DialogDescription>
          )}
        </DialogHeader>
        <DialogPanel className="flex flex-col gap-3">
          <ul className="flex flex-col divide-y empty:hidden">
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
          {more && (
            <Button
              variant="outline"
              pending={seek.seeking || loadingMore}
              onClick={seek.start}
            >
              <Trans id="admin.group.matches.load-more">Load more</Trans>
            </Button>
          )}
        </DialogPanel>
      </DialogPopup>
    </Dialog>
  );
}
