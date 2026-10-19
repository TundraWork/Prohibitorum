import { Trans, useLingui } from "@lingui/react/macro";
import { useSuspenseQuery } from "@tanstack/react-query";
import { HandIcon, ListFilterIcon } from "lucide-react";
import { groupQueryOptions } from "@/api/queries";
import { ObjectHeader } from "@/components/custom/ObjectHeader";
import { SectionRegistryProvider } from "@/components/custom/SectionRegistry";
import { UnsavedBar } from "@/components/custom/UnsavedBar";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { GroupDangerSection } from "@/pages/admin/groups/GroupDangerSection";
import { GroupDetailsSection } from "@/pages/admin/groups/GroupDetailsSection";
import { GroupUsedBySection } from "@/pages/admin/groups/GroupUsedBySection";
import { LegacyRuleSection } from "@/pages/admin/groups/RuleEditorLegacy";

/**
 * One user group's page: who it is at the top, a main column of sections and,
 * from `lg`, a side column of context beside it.
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
        statuses={[group.exposedToDownstream ? "offered" : "not-offered"]}
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

      <div className="mt-8 grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_300px]">
        <div className="flex min-w-0 flex-col gap-6 lg:col-start-1 lg:row-start-1">
          <GroupDetailsSection group={group} />
          {isRule && <LegacyRuleSection group={group} />}
        </div>
        <aside
          aria-label={t({ id: "admin.group.side", message: "Group context" })}
          className="flex min-w-0 flex-col gap-6 lg:col-start-2 lg:row-span-2 lg:row-start-1"
        >
          <GroupUsedBySection groupId={group.id} />
        </aside>
        <div className="min-w-0 lg:col-start-1 lg:row-start-2">
          <GroupDangerSection group={group} />
        </div>
      </div>
      <UnsavedBar />
    </SectionRegistryProvider>
  );
}
