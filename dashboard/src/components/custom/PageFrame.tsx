import type { ReactNode } from "react";

/**
 * Centers a page below the fixed toolbar. The symmetric padding matches the
 * toolbar height, so the card keeps the window center and the toolbar stays
 * clear when the content is taller than the window; from `lg` the frame is the
 * scroll container, leaving the toolbar in place.
 */
export function PageFrame({ children }: { children: ReactNode }) {
  return (
    <div className="flex lg:min-h-dvh flex-col py-4 lg:py-16">
      <div className="m-auto w-full">{children}</div>
    </div>
  );
}
