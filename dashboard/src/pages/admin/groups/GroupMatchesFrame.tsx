import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { useSuspenseQuery } from "@tanstack/react-query";
import { UsersIcon } from "lucide-react";
import { useState } from "react";
import { groupPreviewLimit, groupPreviewQueryOptions } from "@/api/queries";
import type { GroupPreviewView } from "@/api/raw-admin-paths";
import { quietQuery } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { EntityAvatar } from "@/components/custom/EntityAvatar";
import { AsyncSection, Section } from "@/components/custom/Section";
import { getTopLayerContainer } from "@/components/custom/top-layer";
import {
  Dialog,
  DialogDescription,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";

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
 * Who the saved rule matches today: the count, a few faces and the full list
 * behind "See all". The server answers with one fixed page of accounts, each
 * flagged matched or not, so the count is of that page and the frame says so
 * when the page was full.
 */
export function GroupMatchesFrame({ groupId }: { groupId: number }) {
  return (
    <AsyncSection resetKey={`group-${groupId}-matches`} {...heading}>
      <Matches groupId={groupId} />
    </AsyncSection>
  );
}

function Matches({ groupId }: { groupId: number }) {
  const { data } = useSuspenseQuery(
    quietQuery(groupPreviewQueryOptions(groupId)),
  );
  const [open, setOpen] = useState(false);
  const checked = data.items ?? [];
  const matches = checked.filter((item) => item.matched);
  const count = matches.length;
  const capped = checked.length >= groupPreviewLimit;
  const limit = groupPreviewLimit;
  const rest = count - shownAvatars;

  return (
    <Section {...heading}>
      <div className="flex flex-wrap items-baseline gap-x-2">
        <span className="font-semibold text-4xl tabular-nums tracking-tight">
          {count}
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
      {capped && (
        <p className="text-muted-foreground text-xs">
          <Trans id="admin.group.matches.capped">
            Counted among the first {limit} accounts.
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
        limit={capped ? limit : null}
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
  limit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  matches: GroupPreviewView[];
  /** The page size when the page was full, else `null`. */
  limit: number | null;
}) {
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
          {limit !== null && (
            <DialogDescription>
              <Trans id="admin.group.matches.dialog.capped">
                Showing matches among the first {limit} accounts.
              </Trans>
            </DialogDescription>
          )}
        </DialogHeader>
        <DialogPanel>
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
        </DialogPanel>
      </DialogPopup>
    </Dialog>
  );
}
