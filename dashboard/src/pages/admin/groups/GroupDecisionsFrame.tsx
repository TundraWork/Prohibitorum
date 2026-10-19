import { Trans } from "@lingui/react/macro";
import { ScaleIcon } from "lucide-react";
import { useCursorList } from "@/api/cursor-list";
import { groupDecisionsListOptions } from "@/api/queries";
import type { ManualDecisionView } from "@/api/raw-admin-paths";
import { Section } from "@/components/custom/Section";

/**
 * How many accounts a manual group allows and denies, as far as the pages the
 * section has loaded show.
 *
 * The server sends no total, so the counts are never a guess: while pages are
 * left they say they cover the decisions loaded so far, and while nothing is
 * loaded, nothing has failed to load, or there are no decisions, the frame is
 * not drawn at all. It reads the section's own list, so both stay in step.
 */
export function GroupDecisionsFrame({ groupId }: { groupId: number }) {
  const { items, hasMore } = useCursorList<ManualDecisionView>(
    groupDecisionsListOptions(groupId),
  );
  if (items.length === 0) return null;
  const denied = items.filter(({ effect }) => effect === "deny").length;
  const allowed = items.length - denied;

  return (
    <Section
      id="decision-counts"
      icon={<ScaleIcon />}
      title={<Trans id="admin.group.decision-counts.title">Decisions</Trans>}
    >
      <p
        data-slot="decision-counts"
        className="font-medium text-sm tabular-nums"
      >
        <Trans id="admin.group.decision-counts.summary">
          {allowed} allowed · {denied} denied
        </Trans>
      </p>
      {hasMore && (
        <p className="text-muted-foreground text-xs">
          <Trans id="admin.group.decision-counts.partial">
            Among the decisions loaded so far.
          </Trans>
        </p>
      )}
    </Section>
  );
}
