import { createFileRoute, redirect } from "@tanstack/react-router";
import { clearSessionQueries, sessionQueryOptions } from "@/api/queries";
import { ConsoleLayout } from "@/components/custom/ConsoleLayout";
import {
  AppRouteError,
  ConsoleRouteNotFound,
} from "@/components/custom/RouteFeedback";

export const Route = createFileRoute("/_protected")({
  loader: async ({ context: { queryClient }, location }) => {
    const session = await queryClient.query(sessionQueryOptions());
    if (session === null) {
      await clearSessionQueries(queryClient);
      // Signing in brings the reader back to the page they asked for.
      throw redirect({ to: "/login", search: { return_to: location.href } });
    }
  },
  component: ConsoleLayout,
  // Its own failure leaves no console to draw in; an address under it that
  // has no page is drawn inside the console.
  errorComponent: AppRouteError,
  notFoundComponent: ConsoleRouteNotFound,
});
