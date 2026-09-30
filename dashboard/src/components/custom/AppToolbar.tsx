import type { ReactNode } from "react";
import type { LoginSurface } from "@/api/raw-paths";
import { LanguageMenu } from "@/components/custom/LanguageMenu";
import { capsuleSurfaceStyle } from "@/components/custom/login-appearance/surface-style";
import { ThemeSelect } from "@/components/custom/ThemeSelect";

/**
 * The public pages' toolbar. The bar itself has no fill, so a custom
 * background runs from the top of the window to the bottom; what it carries
 * sits on two capsules, drawn as the sign-in page's settings say (translucent
 * and frosted by default), which keep it readable on any picture. Inside a
 * capsule every field and button is fully rounded. It scrolls with the page,
 * so the card never passes underneath it. The theme control is left out when
 * the settings force a theme on the public pages.
 */
export function AppToolbar({
  children,
  surface,
  showThemeSelect,
}: {
  children: ReactNode;
  surface: LoginSurface;
  showThemeSelect: boolean;
}) {
  return (
    <header className="flex h-16 min-w-0 shrink-0 items-center justify-between gap-3 px-4 sm:px-6">
      <Capsule surface={surface} className="pl-1.5 pr-4">
        {children}
      </Capsule>
      <Capsule surface={surface} className="shrink-0 gap-1 px-1">
        <LanguageMenu />
        {showThemeSelect && <ThemeSelect />}
      </Capsule>
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
      className={`flex h-10 min-w-0 items-center rounded-full shadow-surface [--field-radius:100%] [--radius:100%] ${style.className} ${className}`}
    >
      {children}
    </div>
  );
}
