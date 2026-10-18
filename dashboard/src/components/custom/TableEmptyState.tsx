import type { ReactNode } from "react";
import {
  Empty,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";

/**
 * Empty state for the console's own tables: the mark for the kind of thing that
 * is missing, and one line naming what is missing.
 *
 * It carries nothing else on purpose. Every one of these tables sits under the
 * panel's own action, so a button repeated here reads as two offers of the same
 * thing; and the list is empty precisely when the member has just arrived, which
 * is when an explanation is least needed.
 */
export function TableEmptyState({
  icon,
  title,
}: {
  icon: ReactNode;
  title: ReactNode;
}) {
  return (
    // Compact: inside a table the published empty state's page-sized padding
    // and heading scale would dwarf the rows around it.
    <Empty className="gap-0 py-8 md:py-8">
      <EmptyHeader>
        <EmptyMedia variant="icon" className="mb-4">
          {icon}
        </EmptyMedia>
        <EmptyTitle className="font-medium font-sans text-sm">
          {title}
        </EmptyTitle>
      </EmptyHeader>
    </Empty>
  );
}
