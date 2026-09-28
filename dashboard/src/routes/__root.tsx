import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, redirect } from "@tanstack/react-router";
import { publicConfigQueryOptions, sessionQueryOptions } from "@/api/queries";
import { maintenanceRedirect } from "@/app/maintenance-guard";
import { AppLayout } from "@/components/custom/AppLayout";
import { AppRouteError } from "@/components/custom/RouteFeedback";

export type RouterContext = { queryClient: QueryClient };

export const Route = createRootRouteWithContext<RouterContext>()({
  // Maintenance closes every page to everyone but an administrator, so it is
  // decided before any of them loads. The session is only asked for while
  // maintenance is on.
  beforeLoad: async ({ context: { queryClient }, location }) => {
    const config = await queryClient.ensureQueryData(
      publicConfigQueryOptions(),
    );
    if (!config.maintenanceMode) return;
    const session = await queryClient.ensureQueryData(sessionQueryOptions());
    if (
      maintenanceRedirect({
        maintenanceMode: config.maintenanceMode,
        session,
        pathname: location.pathname,
        search: location.search,
      })
    ) {
      throw redirect({ to: "/maintenance", replace: true });
    }
  },
  // The instance's name, icon and background are drawn by every layout and by
  // the document title, so they are in hand before anything paints.
  loader: ({ context: { queryClient } }) =>
    queryClient.ensureQueryData(publicConfigQueryOptions()),
  component: AppLayout,
  errorComponent: AppRouteError,
});
