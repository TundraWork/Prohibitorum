import { createRouter } from "@tanstack/react-router";
import {
  PublicPending,
  RouteError,
  RouteNotFound,
} from "@/components/custom/RouteFeedback";
import type { RouterContext } from "@/routes/__root";
import { routeTree } from "@/routeTree.gen";

export function createAppRouter(context: RouterContext) {
  return createRouter({
    routeTree,
    context,
    defaultPreload: "intent",
    defaultPreloadStaleTime: 0,
    defaultPendingMs: 350,
    defaultPendingMinMs: 120,
    defaultPendingComponent: PublicPending,
    defaultErrorComponent: RouteError,
    defaultNotFoundComponent: RouteNotFound,
    scrollRestoration: true,
  });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter>;
  }
}
