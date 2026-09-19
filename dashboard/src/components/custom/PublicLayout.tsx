import { Outlet } from "@tanstack/react-router";
import { instanceName } from "@/components/custom/AppLayout";
import { AppToolbar } from "@/components/custom/AppToolbar";

export function PublicLayout() {
  return (
    <>
      <AppToolbar>
        <span className="truncate text-lg font-semibold">{instanceName}</span>
      </AppToolbar>
      <main className="mx-auto flex w-full min-w-0 max-w-5xl flex-col gap-6 px-4 pb-8 sm:px-6">
        <Outlet />
      </main>
    </>
  );
}
