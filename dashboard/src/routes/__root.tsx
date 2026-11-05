import type { QueryClient } from "@tanstack/react-query";
import {
  createRootRouteWithContext,
  redirect,
  retainSearchParams,
} from "@tanstack/react-router";
import { z } from "zod";
import { publicConfigQueryOptions, sessionQueryOptions } from "@/api/queries";
import { maintenanceRedirect } from "@/app/maintenance-guard";
import { optionalSearchText } from "@/app/search-params";
import { settleTabAccount } from "@/app/tab-account-guard";
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
  // `u` names the account the tab acts as while the browser has several
  // (`settleTabAccount`), so every navigation inside the app keeps it.
  validateSearch: z.object({ u: optionalSearchText() }),
  search: { middlewares: [retainSearchParams(["u"])] },
  // Maintenance closes every page to everyone but an administrator, so it is
  // decided before any of them loads. The session is only asked for while
  // maintenance is on.
  beforeLoad: async ({ context: { queryClient }, location, matches }) => {
    const config = queryClient.ensureQueryData(publicConfigQueryOptions());
    // Awaited below; a navigation that leaves first leaves it unread.
    config.catch(() => undefined);
    // The pages' reads name the tab's account, so it is settled before any
    // of them goes out.
    await settleTabAccount(queryClient, location);
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
    const { maintenanceMode } = await config;
    if (!maintenanceMode) return { startedReads };
    const session = await queryClient.ensureQueryData(sessionQueryOptions());
    if (
      maintenanceRedirect({
        maintenanceMode,
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
