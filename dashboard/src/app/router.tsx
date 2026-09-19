import type { QueryClient } from "@tanstack/react-query";
import type { Router } from "@tanstack/react-router";
import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  lazyRouteComponent,
  redirect,
} from "@tanstack/react-router";
import { parseReturnTo } from "@/api/auth";
import {
  authStatusQueryOptions,
  clearSessionQueries,
  publicConfigQueryOptions,
  sessionQueryOptions,
} from "@/api/queries";
import { AppLayout } from "@/components/custom/AppLayout";
import { ConsoleLayout } from "@/components/custom/ConsoleLayout";
import { PreviewLayout } from "@/components/custom/PreviewLayout";
import { PublicLayout } from "@/components/custom/PublicLayout";
import {
  RouteError,
  RouteNotFound,
  RoutePending,
} from "@/components/custom/RouteFeedback";
import { ApiPreview } from "@/routes/ApiPreview";
import { Console } from "@/routes/Console";
import { Login } from "@/routes/Login";
import { Preview } from "@/routes/Preview";

type RouterContext = { queryClient: QueryClient };
const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: AppLayout,
});
const protectedRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "protected",
  loader: async ({ context: { queryClient } }) => {
    const session = await queryClient.fetchQuery(sessionQueryOptions());
    if (session === null) {
      await clearSessionQueries(queryClient);
      throw redirect({ to: "/login", replace: true });
    }
  },
  component: ConsoleLayout,
});
const indexRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "/",
  component: Console,
});
const publicRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "public",
  component: PublicLayout,
});
const loginRoute = createRoute({
  getParentRoute: () => publicRoute,
  path: "/login",
  loader: async ({ context: { queryClient }, location, cause }) => {
    // A mounted login flow may be displaying newly issued recovery codes.
    if (cause === "stay") return;
    const [, , session] = await Promise.all([
      queryClient.ensureQueryData(publicConfigQueryOptions()),
      queryClient.ensureQueryData(authStatusQueryOptions()),
      queryClient.fetchQuery(sessionQueryOptions()),
    ]);
    let returnTo: string | undefined;
    try {
      returnTo = parseReturnTo(location.searchStr, window.location.origin);
    } catch {
      return;
    }
    if (session !== null && returnTo === undefined) {
      throw redirect({ to: "/", replace: true });
    }
  },
  component: Login,
});
const previewRoute = createRoute({
  getParentRoute: () => publicRoute,
  id: "preview",
  component: PreviewLayout,
});
const componentsRoute = createRoute({
  getParentRoute: () => previewRoute,
  path: "/preview/components",
  component: Preview,
});
const apiRoute = createRoute({
  getParentRoute: () => previewRoute,
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
        getParentRoute: () => previewRoute,
        path: "/__dev/forms",
        component: lazyRouteComponent(() => import("@/routes/DevForms")),
      }),
    ]
  : [];
const routeTree = rootRoute.addChildren([
  protectedRoute.addChildren([indexRoute]),
  publicRoute.addChildren([
    loginRoute,
    previewRoute.addChildren([componentsRoute, apiRoute, ...devRoutes]),
  ]),
]);

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
