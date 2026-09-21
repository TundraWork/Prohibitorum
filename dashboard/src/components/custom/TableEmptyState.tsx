import { EmptyState } from "@heroui/react";
import type { ReactNode } from "react";

/**
 * Empty state for the console's own tables: the mark, what is missing, and the
 * next step where the panel has exactly one. The mark is sized for an 18px icon.
 */
export function TableEmptyState({
  icon,
  title,
  hint,
  action,
}: {
  icon: ReactNode;
  title: ReactNode;
  hint?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <EmptyState className="flex flex-col items-center gap-3 py-10 text-center max-sm:items-start max-sm:text-start">
      <span className="grid size-9 shrink-0 place-items-center rounded-[0.375rem] bg-accent-soft text-accent-soft-foreground">
        {icon}
      </span>
      <span className="text-sm font-medium text-foreground">{title}</span>
      {hint ? <p className="max-w-[46ch] text-sm text-muted">{hint}</p> : null}
      {action}
    </EmptyState>
  );
}
