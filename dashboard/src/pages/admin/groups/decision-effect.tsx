import { Trans } from "@lingui/react/macro";
import type { ManualDecisionView } from "@/api/raw-admin-paths";
import { Badge } from "@/components/ui/badge";

export type DecisionEffect = "allow" | "deny";

/** The server only writes these two; anything else reads as the milder one. */
export const effectOf = ({ effect }: ManualDecisionView): DecisionEffect =>
  effect === "deny" ? "deny" : "allow";

/** The effect as a badge: the same words and colours wherever a decision shows. */
export function EffectBadge({ effect }: { effect: DecisionEffect }) {
  return effect === "deny" ? (
    <Badge variant="destructive">
      <Trans id="admin.group.decisions.effect.deny">Deny</Trans>
    </Badge>
  ) : (
    <Badge variant="success">
      <Trans id="admin.group.decisions.effect.allow">Allow</Trans>
    </Badge>
  );
}
