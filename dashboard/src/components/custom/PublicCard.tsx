import { Card } from "@heroui/react";
import type { ReactNode } from "react";
import type { LoginCardPosition } from "@/api/raw-paths";
import { publicCardPlacement } from "@/components/custom/login-appearance/card-position";
import type { SurfaceStyle } from "@/components/custom/login-appearance/surface-style";
import { PageFrame } from "@/components/custom/PageFrame";

/**
 * The card the public pages sit on, centered in the window. `PublicLayout`
 * draws it under the toolbar and passes `belowToolbar`, the card's surface
 * and `position` from the sign-in page's settings, and `after` — the wallpaper
 * credit, which sits under the card on a narrow screen. On the left or the
 * right, from `lg`, the card lines up with the toolbar's capsules. A page that
 * fails before any layout can draw uses it alone, centred, opaque and without
 * a credit, keeping the toolbar's height empty above it so the card lands in
 * the same place.
 */
export function PublicCard({
  children,
  belowToolbar = false,
  surface,
  position = "center",
  after,
}: {
  children: ReactNode;
  belowToolbar?: boolean;
  surface?: SurfaceStyle;
  position?: LoginCardPosition;
  after?: ReactNode;
}) {
  return (
    <main
      className={`mx-auto flex w-full min-w-0 max-w-[30rem] flex-col px-4 ${publicCardPlacement({ position })} ${belowToolbar ? "" : "pt-16"}`}
    >
      <PageFrame>
        <Card
          className={`w-full p-6 sm:p-8 ${surface?.className ?? ""}`}
          style={surface?.style}
        >
          <Card.Content>{children}</Card.Content>
        </Card>
        {after}
      </PageFrame>
    </main>
  );
}
