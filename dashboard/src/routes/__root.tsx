import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, redirect } from "@tanstack/react-router";
import { publicConfigQueryOptions, sessionQueryOptions } from "@/api/queries";
import { maintenanceRedirect } from "@/app/maintenance-guard";
import { AppLayout } from "@/components/custom/AppLayout";
import { AppRouteError } from "@/components/custom/RouteFeedback";

export type RouterContext = { queryClient: QueryClient };

/** Reads started by the root for the pages being opened, by route id. */
export type StartedReads = Map<string, Promise<unknown>>;

declare module "@tanstack/react-router" {
  interface StaticDataRouteOption {
    /**
     * Starts the reads the page's loader will wait on, from the root as a
     * navigation begins, so they run beside the config read instead of after
     * it and every layout above the page. The loader finds them in
     * `startedReads` under its route id.
     */
    startReads?: (match: {
      queryClient: QueryClient;
      search: Record<string, unknown>;
      params: Record<string, string>;
    }) => Promise<unknown> | undefined;
  }
}

export const Route = createRootRouteWithContext<RouterContext>()({
  // Maintenance closes every page to everyone but an administrator, so it is
  // decided before any of them loads. The session is only asked for while
  // maintenance is on.
  beforeLoad: async ({ context: { queryClient }, location, matches }) => {
    // A page that knows what its loader will wait on starts it here, beside
    // the config read (`startReads`).
    const startedReads: StartedReads = new Map();
    for (const match of matches) {
      const reads = match.staticData.startReads?.({
        queryClient,
        search: match.search as Record<string, unknown>,
        params: match.params as Record<string, string>,
      });
      if (reads === undefined) continue;
      // A navigation that goes elsewhere leaves them unread.
      reads.catch(() => undefined);
      startedReads.set(match.routeId, reads);
    }
    const config = await queryClient.ensureQueryData(
      publicConfigQueryOptions(),
    );
    if (!config.maintenanceMode) return { startedReads };
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
    return { startedReads };
  },
  // The instance's name, icon and background are drawn by every layout and by
  // the document title, so they are in hand before anything paints.
  loader: ({ context: { queryClient } }) =>
    queryClient.ensureQueryData(publicConfigQueryOptions()),
  component: AppLayout,
  errorComponent: AppRouteError,
});
