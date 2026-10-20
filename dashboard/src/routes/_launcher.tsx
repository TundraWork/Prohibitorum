import { createFileRoute, redirect } from "@tanstack/react-router";
import { clearSessionQueries, sessionQueryOptions } from "@/api/queries";
import { LauncherLayout } from "@/components/custom/LauncherLayout";
import { AppRouteError } from "@/components/custom/RouteFeedback";

/** The signed-in home, outside the console: see `LauncherLayout`. */
export const Route = createFileRoute("/_launcher")({
  loader: async ({ context: { queryClient }, location }) => {
    const session = await queryClient.query(sessionQueryOptions());
    if (session === null) {
      await clearSessionQueries(queryClient);
      // Signing in brings the reader back to the page they asked for.
      throw redirect({ to: "/login", search: { return_to: location.href } });
    }
  },
  component: LauncherLayout,
  errorComponent: AppRouteError,
});
