import { EmptyState } from "@heroui/react";
import type { ReactNode } from "react";

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
    <EmptyState className="flex flex-col items-center gap-3 py-8 text-center max-sm:items-start max-sm:text-start">
      <span className="grid size-9 shrink-0 place-items-center rounded-[0.375rem] bg-accent-soft text-accent-soft-foreground">
        {icon}
      </span>
      <span className="text-sm font-medium text-foreground">{title}</span>
    </EmptyState>
  );
}
