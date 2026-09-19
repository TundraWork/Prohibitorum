import { Outlet } from "@tanstack/react-router";
import { instanceName } from "@/components/custom/AppLayout";
import { LanguageSelect } from "@/components/custom/LanguageSelect";
import { ThemeToggle } from "@/components/custom/ThemeToggle";

export function PublicLayout() {
  return (
    <>
      <header className="mx-auto flex w-full max-w-5xl flex-wrap items-center justify-between gap-4 px-4 py-6 sm:px-6">
        <span className="text-lg font-semibold wrap-anywhere">
          {instanceName}
        </span>
        <div className="flex flex-wrap items-center gap-3">
          <LanguageSelect />
          <ThemeToggle />
        </div>
      </header>
      <main className="mx-auto flex w-full min-w-0 max-w-5xl flex-col gap-6 px-4 pb-8 sm:px-6">
        <Outlet />
      </main>
    </>
  );
}
