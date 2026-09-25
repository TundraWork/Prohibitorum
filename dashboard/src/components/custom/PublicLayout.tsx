import { Card } from "@heroui/react";
import { Outlet } from "@tanstack/react-router";
import { AppToolbar } from "@/components/custom/AppToolbar";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { PageFrame } from "@/components/custom/PageFrame";

export function PublicLayout() {
  const { name, backgroundUrl } = useInstanceBranding();
  return (
    <>
      {/* A custom background sits behind everything. The card in front of it
          stays HeroUI's opaque Card, so the text on it keeps its contrast
          whatever the picture is. */}
      {backgroundUrl !== undefined && (
        <img
          src={backgroundUrl}
          alt=""
          aria-hidden="true"
          className="pointer-events-none fixed inset-0 -z-10 size-full object-cover"
        />
      )}
      <div className="lg:fixed inset-x-0 top-[var(--app-sticky-offset)] z-0 bg-background">
        <AppToolbar>
          <span className="truncate text-lg font-semibold">{name}</span>
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
