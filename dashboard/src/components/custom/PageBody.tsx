import type { ReactNode } from "react";

/**
 * A member page's column: the page header and its sections at a reading width,
 * left-aligned in the shell so the heading shares the breadcrumb's edge. The shell itself stays wide for the admin pages.
 */
export function PageBody({ children }: { children: ReactNode }) {
  return (
    <div data-slot="page-body" className="flex w-full max-w-3xl flex-col gap-8">
      {children}
    </div>
  );
}
