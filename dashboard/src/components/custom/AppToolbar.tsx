import type { ReactNode } from "react";
import type { LoginSurface } from "@/api/raw-paths";
import { LanguageMenu } from "@/components/custom/LanguageMenu";
import { capsuleSurfaceStyle } from "@/components/custom/login-appearance/surface-style";
import { ThemeMenu } from "@/components/custom/ThemeSelect";
import { cn } from "@/lib/utils";

/**
 * The public pages' toolbar. The bar itself has no fill, so a custom
 * background runs from the top of the window to the bottom; what it carries
 * sits on two capsules, drawn as the sign-in page's settings say (translucent
 * and frosted by default), which keep it readable on any picture. It scrolls
 * with the page, so the stage never passes underneath it. The theme button is
 * always there; where the settings force light or dark it offers only the
 * colour scheme.
 */
export function AppToolbar({
  children,
  surface,
  chooseMode,
  account,
  className,
}: {
  children: ReactNode;
  /** Classes for the bar, such as the public pages' wider edge. */
  className?: string;
  surface: LoginSurface;
  /** False where the sign-in page's settings fix light or dark. */
  chooseMode: boolean;
  /** The signed-in account's menu, on its own capsule at the trailing end. */
  account?: ReactNode;
}) {
  return (
    <header
      className={cn(
        "flex h-16 min-w-0 shrink-0 items-center justify-between gap-3 px-5",
        className,
      )}
    >
      <Capsule surface={surface} className="ps-1.5 pe-3 font-medium text-sm">
        {children}
      </Capsule>
      <div className="flex shrink-0 items-center gap-2">
        <Capsule surface={surface} className="shrink-0 gap-0.5 px-1">
          <LanguageMenu />
          <ThemeMenu modes={chooseMode} />
        </Capsule>
        {account !== undefined && (
          <Capsule surface={surface} className="shrink-0 px-1">
            {account}
          </Capsule>
        )}
      </div>
    </header>
  );
}

function Capsule({
  className,
  surface,
  children,
}: {
  className: string;
  surface: LoginSurface;
  children: ReactNode;
}) {
  const style = capsuleSurfaceStyle(surface);
  return (
    <div
      data-toolbar-capsule=""
      style={style.style}
      className={cn(
        "flex h-9 min-w-0 items-center gap-2 rounded-xl shadow-xs/5 ring-1 ring-black/8 dark:ring-white/12",
        style.className,
        className,
      )}
    >
      {children}
    </div>
  );
}
