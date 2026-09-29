import { Card } from "@heroui/react";
import type { ReactNode } from "react";
import { PageFrame } from "@/components/custom/PageFrame";

/**
 * The card the public pages sit on, centered in the window. `PublicLayout`
 * draws it under the toolbar and passes `belowToolbar`; a page that fails
 * before any layout can draw uses it alone, keeping the toolbar's height
 * empty above it so the card lands in the same place.
 */
export function PublicCard({
  children,
  belowToolbar = false,
}: {
  children: ReactNode;
  belowToolbar?: boolean;
}) {
  return (
    <main
      className={`mx-auto flex w-full min-w-0 max-w-[30rem] flex-col px-4 ${belowToolbar ? "" : "pt-16"}`}
    >
      <PageFrame>
        <Card className="w-full p-6 sm:p-8">
          <Card.Content>{children}</Card.Content>
        </Card>
      </PageFrame>
    </main>
  );
}
