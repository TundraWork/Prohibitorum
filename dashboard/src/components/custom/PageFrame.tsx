import type { ReactNode } from "react";

/**
 * Centers a page in the window below the toolbar. The toolbar scrolls with the
 * page, so the frame fills the height it leaves and, from `sm`, keeps a space
 * of the same height underneath: the card then sits at the window's centre.
 * Content taller than that scrolls with the page.
 */
export function PageFrame({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-[calc(var(--app-viewport-height)-4rem)] flex-col py-4 sm:pb-20">
      <div className="m-auto w-full">{children}</div>
    </div>
  );
}
