import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

/**
 * The page's heading: its only `h1`, with an optional line saying what the page
 * is for and actions on the trailing edge from `sm`. The console header's
 * breadcrumb names where the page sits and is not a heading. The primary
 * action wears the sign-in page's passkey button: taller, its leading icon set
 * in an inset chip.
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
          className={cn(
            "flex shrink-0 items-center gap-2 sm:ms-auto",
            "[&_[data-variant=default]]:h-10 [&_[data-variant=default]]:gap-2.5 [&_[data-variant=default]]:ps-1.5",
            "[&_[data-variant=default]>svg:first-child]:box-content [&_[data-variant=default]>svg:first-child]:mx-0 [&_[data-variant=default]>svg:first-child]:rounded-md [&_[data-variant=default]>svg:first-child]:bg-primary-foreground/12 [&_[data-variant=default]>svg:first-child]:p-1.5 [&_[data-variant=default]>svg:first-child]:opacity-100 [&_[data-variant=default]>svg:first-child]:ring-1 [&_[data-variant=default]>svg:first-child]:ring-primary-foreground/16",
          )}
        >
          {actions}
        </div>
      )}
    </header>
  );
}
