import { Trans } from "@lingui/react/macro";
import { ScaleIcon } from "lucide-react";
import { useCursorList } from "@/api/cursor-list";
import { groupDecisionsListOptions } from "@/api/queries";
import type { ManualDecisionView } from "@/api/raw-admin-paths";
import { Section } from "@/components/custom/Section";

/**
 * How many accounts a manual group allows and denies.
 *
 * The server sends no total, so the counts are only drawn once every page is
 * loaded: a count of some of the decisions would read as the whole. While
 * pages remain, while nothing is loaded and when there are no decisions, the
 * frame is not drawn at all. It reads the section's own list, so both stay in
 * step.
 */
export function GroupDecisionsFrame({ groupId }: { groupId: number }) {
  const { items, hasMore } = useCursorList<ManualDecisionView>(
    groupDecisionsListOptions(groupId),
  );
  if (items.length === 0 || hasMore) return null;
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
    </Section>
  );
}
