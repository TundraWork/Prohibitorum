import type { ReactNode } from "react";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";

/**
 * Empty state for the console's own tables: the mark for the kind of thing that
 * is missing, and one line naming what is missing.
 *
 * It carries no action on purpose: every one of these tables sits under a
 * header with its own, so a button repeated here reads as two offers of the same
 * thing. A list that is empty precisely when someone has just arrived — the
 * application lists — adds `description`, one line saying what the things are
 * for.
 */
export function TableEmptyState({
  icon,
  title,
  description,
}: {
  icon: ReactNode;
  title: ReactNode;
  description?: ReactNode;
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
        {description !== undefined && (
          <EmptyDescription>{description}</EmptyDescription>
        )}
      </EmptyHeader>
    </Empty>
  );
}
