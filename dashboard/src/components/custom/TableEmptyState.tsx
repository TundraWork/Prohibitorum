import type { ReactNode } from "react";
import {
  Empty,
  EmptyContent,
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
 * for. A list that can be empty because of what the reader asked for passes
 * `action`, the one way back out of it.
 */
export function TableEmptyState({
  icon,
  title,
  description,
  action,
}: {
  icon: ReactNode;
  title: ReactNode;
  description?: ReactNode;
  action?: ReactNode;
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
      {action !== undefined && (
        <EmptyContent className="mt-4">{action}</EmptyContent>
      )}
    </Empty>
  );
}
