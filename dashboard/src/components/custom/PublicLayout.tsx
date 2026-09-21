import { Card } from "@heroui/react";
import { Outlet } from "@tanstack/react-router";
import { instanceName } from "@/components/custom/AppLayout";
import { AppToolbar } from "@/components/custom/AppToolbar";
import { PageFrame } from "@/components/custom/PageFrame";

export function PublicLayout() {
  return (
    <>
      <div className="lg:fixed inset-x-0 top-[var(--app-sticky-offset)] z-0 bg-background">
        <AppToolbar>
          <span className="truncate text-lg font-semibold">{instanceName}</span>
        </AppToolbar>
      </div>
      <main className="mx-auto flex lg:min-h-[var(--app-viewport-height)] w-full min-w-0 max-w-[30rem] flex-col px-4">
        <PageFrame>
          <Card className="w-full p-6">
            <Card.Content>
              <Outlet />
            </Card.Content>
          </Card>
        </PageFrame>
      </main>
    </>
  );
}
