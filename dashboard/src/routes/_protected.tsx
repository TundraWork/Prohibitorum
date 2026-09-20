import { createFileRoute, redirect } from "@tanstack/react-router";
import { clearSessionQueries, sessionQueryOptions } from "@/api/queries";
import { ConsoleLayout } from "@/components/custom/ConsoleLayout";

export const Route = createFileRoute("/_protected")({
  loader: async ({ context: { queryClient } }) => {
    const session = await queryClient.query(sessionQueryOptions());
    if (session === null) {
      await clearSessionQueries(queryClient);
      throw redirect({ to: "/login" });
    }
  },
  component: ConsoleLayout,
});
