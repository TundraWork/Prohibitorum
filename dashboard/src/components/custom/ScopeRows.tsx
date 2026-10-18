import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

export interface ScopeRow {
  key: string;
  icon: ReactNode;
  title: ReactNode;
  detail?: ReactNode;
}

/**
 * What an application will receive, as one bordered panel with a hairline
 * between the rows: an icon well, the thing's name and, under it, what it
 * means. The icon only pictures the title beside it, so it is hidden from
 * assistive technology.
 *
 * `labelledBy` is the id of the label that introduces the list.
 */
export function ScopeRows({
  rows,
  labelledBy,
}: {
  rows: readonly ScopeRow[];
  labelledBy?: string;
}) {
  return (
    <ul
      aria-labelledby={labelledBy}
      className="divide-y overflow-hidden rounded-xl border bg-background shadow-xs/5"
    >
      {rows.map((row) => (
        <li key={row.key} className="flex items-start gap-3 px-3.5 py-3">
          <span
            aria-hidden="true"
            className={cn(
              "flex size-7 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground [&_svg]:size-4",
              row.detail && "mt-0.5",
            )}
          >
            {row.icon}
          </span>
          <div className={cn("min-w-0", !row.detail && "self-center")}>
            <div className="font-medium text-sm wrap-anywhere">{row.title}</div>
            {row.detail && (
              <div className="text-muted-foreground text-xs">{row.detail}</div>
            )}
          </div>
        </li>
      ))}
    </ul>
  );
}
