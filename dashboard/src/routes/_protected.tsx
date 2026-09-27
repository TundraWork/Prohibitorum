import { createFileRoute, redirect } from "@tanstack/react-router";
import { clearSessionQueries, sessionQueryOptions } from "@/api/queries";
import { ConsoleLayout } from "@/components/custom/ConsoleLayout";
import {
  AppRouteError,
  ConsoleRouteNotFound,
} from "@/components/custom/RouteFeedback";

export const Route = createFileRoute("/_protected")({
  loader: async ({ context: { queryClient } }) => {
    const session = await queryClient.query(sessionQueryOptions());
    if (session === null) {
      await clearSessionQueries(queryClient);
      throw redirect({ to: "/login" });
    }
  },
  component: ConsoleLayout,
  // Its own failure leaves no console to draw in; an address under it that
  // has no page is drawn inside the console.
  errorComponent: AppRouteError,
  notFoundComponent: ConsoleRouteNotFound,
});
