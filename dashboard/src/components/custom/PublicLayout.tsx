import { Outlet } from "@tanstack/react-router";
import { AppToolbar } from "@/components/custom/AppToolbar";
import { InstanceIdentity } from "@/components/custom/InstanceIdentity";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { PublicCard } from "@/components/custom/PublicCard";
import { RouteLayoutContext } from "@/components/custom/RouteFeedback";

export function PublicLayout() {
  const { backgroundUrl } = useInstanceBranding();
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
      <AppToolbar>
        <InstanceIdentity />
      </AppToolbar>
      <PublicCard belowToolbar>
        <RouteLayoutContext value="public">
          <Outlet />
        </RouteLayoutContext>
      </PublicCard>
    </>
  );
}
