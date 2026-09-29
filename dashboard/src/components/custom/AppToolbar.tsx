import type { ReactNode } from "react";
import { LanguageMenu } from "@/components/custom/LanguageMenu";
import { ThemeSelect } from "@/components/custom/ThemeSelect";

/**
 * The public pages' toolbar. The bar itself has no fill, so a custom
 * background runs from the top of the window to the bottom; what it carries
 * sits on two translucent capsules, which keep it readable on any picture.
 * It scrolls with the page, so the card never passes underneath it.
 */
export function AppToolbar({ children }: { children: ReactNode }) {
  return (
    <header className="flex h-16 min-w-0 shrink-0 items-center justify-between gap-3 px-4 sm:px-6">
      <Capsule className="pl-1.5 pr-4">{children}</Capsule>
      <Capsule className="shrink-0 gap-1 px-1">
        <LanguageMenu />
        <ThemeSelect />
      </Capsule>
    </header>
  );
}

function Capsule({
  className,
  children,
}: {
  className: string;
  children: ReactNode;
}) {
  return (
    <div
      className={`flex h-10 min-w-0 items-center rounded-full bg-surface/70 shadow-surface backdrop-blur-md ${className}`}
    >
      {children}
    </div>
  );
}
