import type { ReactNode } from "react";

/**
 * The page's heading: its only `h1`, with an optional line saying what the page
 * is for and actions on the trailing edge from `sm`. The console header's
 * breadcrumb names where the page sits and is not a heading.
 */
export function PageHeader({
  title,
  description,
  actions,
}: {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <header className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
      <div className="flex min-w-0 flex-col gap-1">
        <h1 className="font-semibold text-2xl tracking-tight">{title}</h1>
        {description !== undefined && (
          <p
            data-slot="page-description"
            className="text-pretty text-muted-foreground"
          >
            {description}
          </p>
        )}
      </div>
      {actions !== undefined && (
        <div
          data-slot="page-actions"
          className="flex shrink-0 items-center gap-2 sm:ms-auto"
        >
          {actions}
        </div>
      )}
    </header>
  );
}
