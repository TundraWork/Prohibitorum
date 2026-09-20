import { Outlet } from "@tanstack/react-router";
import { instanceName } from "@/components/custom/AppLayout";
import { AppToolbar } from "@/components/custom/AppToolbar";

export function PublicLayout() {
  return (
    <>
      <div className="lg:fixed inset-x-0 top-0 z-0 bg-background">
        <AppToolbar>
          <span className="truncate text-lg font-semibold">{instanceName}</span>
        </AppToolbar>
      </div>
      <main className="mx-auto flex lg:min-h-dvh w-full min-w-0 max-w-5xl flex-col px-4 sm:px-6">
        <Outlet />
      </main>
    </>
  );
}
