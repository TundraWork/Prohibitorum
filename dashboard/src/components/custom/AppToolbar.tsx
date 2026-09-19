import type { ReactNode } from "react";
import { LanguageMenu } from "@/components/custom/LanguageMenu";
import { ThemeSelect } from "@/components/custom/ThemeSelect";

export function AppToolbar({ children }: { children: ReactNode }) {
  return (
    <header className="flex h-16 min-w-0 shrink-0 items-center gap-3 px-4 sm:px-6">
      <div className="flex min-w-0 flex-1 items-center gap-3">{children}</div>
      <div className="flex shrink-0 items-center gap-2">
        <LanguageMenu />
        <ThemeSelect />
      </div>
    </header>
  );
}
