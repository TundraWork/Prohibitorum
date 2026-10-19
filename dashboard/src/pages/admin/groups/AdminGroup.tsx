import { Trans, useLingui } from "@lingui/react/macro";
import { useSuspenseQuery } from "@tanstack/react-query";
import { HandIcon, ListFilterIcon } from "lucide-react";
import { useLayoutEffect, useRef, useState } from "react";
import { groupQueryOptions } from "@/api/queries";
import type { AppGroupView } from "@/api/raw-admin-paths";
import { ObjectHeader } from "@/components/custom/ObjectHeader";
import { SectionRegistryProvider } from "@/components/custom/SectionRegistry";
import {
  UnsavedBar,
  unsavedReserveClass,
  useUnsavedReserve,
} from "@/components/custom/UnsavedBar";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { GroupDangerSection } from "@/pages/admin/groups/GroupDangerSection";
import { GroupDecisionsFrame } from "@/pages/admin/groups/GroupDecisionsFrame";
import { GroupDecisionsSection } from "@/pages/admin/groups/GroupDecisionsSection";
import { GroupDetailsSection } from "@/pages/admin/groups/GroupDetailsSection";
import { GroupExplainFrame } from "@/pages/admin/groups/GroupExplainFrame";
import { GroupMatchesFrame } from "@/pages/admin/groups/GroupMatchesFrame";
import { GroupRuleSection } from "@/pages/admin/groups/GroupRuleSection";
import { GroupUsedBySection } from "@/pages/admin/groups/GroupUsedBySection";

/**
 * One user group's page: who it is at the top, a main column of sections and,
 * from `lg`, a side column of context beside it: for a rule group who it
 * matches and why, for a manual group how many decisions it holds, then where
 * it is used.
 *
 * The danger section is the last of the main column, but its own grid item, so
 * below `lg` the side column's context reads before it instead of after the
 * deletion row. Each section saves the record through `groupUpdateBody` and
 * none shares unsaved state with another.
 */
export function AdminGroup({ groupId }: { groupId: number }) {
  const { t } = useLingui();
  const { data: group } = useSuspenseQuery(groupQueryOptions(groupId));
  const isRule = group.kind === "rule";

  return (
    <SectionRegistryProvider>
      <ObjectHeader
        icon={
          <span
            aria-hidden="true"
            className={cn(
              "flex size-14 shrink-0 items-center justify-center rounded-2xl ring-1 *:size-6",
              isRule
                ? "bg-info/10 text-info-foreground ring-info/20"
                : "bg-muted text-muted-foreground ring-border",
            )}
          >
            {isRule ? <ListFilterIcon /> : <HandIcon />}
          </span>
        }
        title={group.displayName}
        statuses={[group.exposedToDownstream ? "sent" : "not-sent"]}
        identifier={{
          value: group.slug,
          label: t({
            id: "admin.group.copy-slug",
            message: "Copy the identifier",
          }),
        }}
        facts={[
          isRule ? (
            <Badge key="kind" variant="info">
              <Trans id="admin.group.kind.badge.rule">Rule group</Trans>
            </Badge>
          ) : (
            <Badge key="kind" variant="outline">
              <Trans id="admin.group.kind.badge.manual">Manual group</Trans>
            </Badge>
          ),
        ]}
      />

      <GroupColumns group={group} />
      <UnsavedBar />
    </SectionRegistryProvider>
  );
}

/**
 * Whether the side column fits in the viewport below the console header. A
 * column that does can stay in view while the page scrolls; one that does not
 * would hide its last block behind an inner scroll nobody sees, so it scrolls
 * with the page instead.
 */
function useFitsViewport() {
  const ref = useRef<HTMLElement>(null);
  const [fits, setFits] = useState(false);
  useLayoutEffect(() => {
    const element = ref.current;
    if (element === null) return;
    const measure = () => {
      const top = Number.parseFloat(getComputedStyle(element).top) || 0;
      setFits(element.offsetHeight + top + 24 <= window.innerHeight);
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    window.addEventListener("resize", measure);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, []);
  return { ref, fits };
}

/** The page's columns. A separate component so it can read the section registry. */
function GroupColumns({ group }: { group: AppGroupView }) {
  const { t } = useLingui();
  const isRule = group.kind === "rule";
  const reserve = useUnsavedReserve();
  const side = useFitsViewport();
  return (
    <div
      {...reserve}
      className={`mt-8 grid items-start gap-10 ${unsavedReserveClass} lg:grid-cols-[minmax(0,1fr)_300px]`}
    >
      <div className="flex min-w-0 flex-col gap-6 lg:col-start-1 lg:row-start-1">
        <GroupDetailsSection group={group} />
        {isRule ? (
          <GroupRuleSection key={group.id} group={group} />
        ) : (
          <GroupDecisionsSection key={group.id} groupId={group.id} />
        )}
      </div>
      <aside
        ref={side.ref}
        data-pinned={side.fits}
        aria-label={t({ id: "admin.group.side", message: "Group context" })}
        className="flex min-w-0 flex-col gap-6 lg:top-[calc(var(--app-sticky-offset)+5rem)] lg:data-[pinned=true]:sticky lg:col-start-2 lg:row-span-2 lg:row-start-1 lg:self-start"
      >
        {isRule && group.rule !== undefined && (
          <>
            <GroupMatchesFrame
              key={`matches-${group.id}`}
              groupId={group.id}
              rule={group.rule}
            />
            <GroupExplainFrame key={`explain-${group.id}`} groupId={group.id} />
          </>
        )}
        {!isRule && (
          <GroupDecisionsFrame
            key={`decisions-${group.id}`}
            groupId={group.id}
          />
        )}
        <GroupUsedBySection groupId={group.id} />
      </aside>
      <div className="min-w-0 lg:col-start-1 lg:row-start-2">
        <GroupDangerSection group={group} />
      </div>
    </div>
  );
}
