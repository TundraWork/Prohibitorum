import type { QueryClient } from "@tanstack/react-query";
import type { Router } from "@tanstack/react-router";
import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  lazyRouteComponent,
} from "@tanstack/react-router";
import {
  authStatusQueryOptions,
  publicConfigQueryOptions,
} from "@/api/queries";
import { AppLayout } from "@/components/custom/AppLayout";
import {
  RouteError,
  RouteNotFound,
  RoutePending,
} from "@/components/custom/RouteFeedback";
import { ApiPreview } from "@/routes/ApiPreview";
import { Preview } from "@/routes/Preview";

type RouterContext = { queryClient: QueryClient };
const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: AppLayout,
});
const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: Preview,
});
const apiRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/preview/api",
  loader: async ({ context: { queryClient } }) => {
    await Promise.all([
      queryClient.ensureQueryData(publicConfigQueryOptions()),
      queryClient.ensureQueryData(authStatusQueryOptions()),
    ]);
  },
  component: ApiPreview,
});
const devRoutes = import.meta.env.DEV
  ? [
      createRoute({
        getParentRoute: () => rootRoute,
        path: "/__dev/forms",
        component: lazyRouteComponent(() => import("@/routes/DevForms")),
      }),
    ]
  : [];
const routeTree = rootRoute.addChildren([indexRoute, apiRoute, ...devRoutes]);

export function createAppRouter(context: RouterContext) {
  return createRouter({
    routeTree,
    context,
    defaultPreload: "intent",
    defaultPreloadStaleTime: 0,
    defaultPendingMs: 0,
    defaultPendingMinMs: 0,
    defaultPendingComponent: RoutePending,
    defaultErrorComponent: RouteError,
    defaultNotFoundComponent: RouteNotFound,
    scrollRestoration: true,
  });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: Router<typeof routeTree>;
  }
}
