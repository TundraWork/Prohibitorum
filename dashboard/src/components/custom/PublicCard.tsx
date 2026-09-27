import { Card } from "@heroui/react";
import type { ReactNode } from "react";
import { PageFrame } from "@/components/custom/PageFrame";

/**
 * The card the public pages sit on, centered in the window. `PublicLayout`
 * draws it under the toolbar; a page that fails before any layout can draw
 * uses it alone.
 */
export function PublicCard({ children }: { children: ReactNode }) {
  return (
    <main className="mx-auto flex lg:min-h-[var(--app-viewport-height)] w-full min-w-0 max-w-[30rem] flex-col px-4">
      <PageFrame>
        <Card className="w-full p-6">
          <Card.Content>{children}</Card.Content>
        </Card>
      </PageFrame>
    </main>
  );
}
