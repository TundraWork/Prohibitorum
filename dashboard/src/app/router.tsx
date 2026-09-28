import type { RegisteredRouter, RouterHistory } from "@tanstack/react-router";
import { createRouter } from "@tanstack/react-router";
import {
  AppRouteNotFound,
  PublicPending,
  RouteError,
} from "@/components/custom/RouteFeedback";
import type { RouterContext } from "@/routes/__root";
import { routeTree } from "@/routeTree.gen";

/** The app's router. A test passes its own history; the app uses the browser's. */
export function createAppRouter(
  context: RouterContext,
  history?: RouterHistory,
) {
  return createRouter({
    routeTree,
    context,
    ...(history ? { history } : {}),
    defaultPreload: "intent",
    defaultPreloadStaleTime: 0,
    defaultPendingMs: 350,
    defaultPendingMinMs: 120,
    defaultPendingComponent: PublicPending,
    // A page draws its failure in the layout it sits in (see `RouteError`).
    // An address no layout claims is not found at the root.
    defaultErrorComponent: RouteError,
    defaultNotFoundComponent: AppRouteNotFound,
    scrollRestoration: true,
  });
}

/**
 * Runs a navigation the app starts itself with the route pending component
 * suppressed.
 *
 * The pending component covers the first paint, when the page is still empty.
 * A navigation the user triggers from inside the app is different: the control
 * that started it already reports its own progress (a submitting form, a
 * pressed button), so trading the page for a spinner only takes live content
 * away. Wrap those calls; leave a route change that is the user's only
 * feedback unwrapped.
 *
 * TanStack Router reads the threshold off `router.options` at the moment it
 * decides to paint pending, which is what lets this swap the value around the
 * call.
 */
export async function withRouterSkipLoading<T>(
  router: RegisteredRouter,
  run: () => Promise<T>,
): Promise<T> {
  const { defaultPendingMs } = router.options;
  router.options.defaultPendingMs = Number.POSITIVE_INFINITY;
  try {
    return await run();
  } finally {
    router.options.defaultPendingMs = defaultPendingMs;
  }
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter>;
  }
}
