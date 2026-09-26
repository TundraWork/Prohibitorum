import { createFileRoute, redirect } from "@tanstack/react-router";
import { sessionQueryOptions } from "@/api/queries";
import { AdminForwardAuthAppNew } from "@/pages/admin/forward-auth-apps/AdminForwardAuthAppNew";

/**
 * Registering a new forward-auth application is an administrator's step.
 *
 * A delegated manager reaches the list and the applications they were assigned,
 * but the server refuses creation to anyone who is not an administrator, so the
 * route refuses too rather than rendering a form that cannot be submitted. It
 * is the same test `_protected.admin._admin` makes for its whole subtree, made
 * here on its own because these routes hang one level up — a delegated manager
 * needs the rest of this directory.
 */
export const Route = createFileRoute(
  "/_protected/admin/forward-auth-apps_/new",
)({
  loader: async ({ context: { queryClient } }) => {
    const session = await queryClient.fetchQuery(sessionQueryOptions());
    if (session === null || session.role !== "admin") {
      throw redirect({ to: "/" });
    }
  },
  component: AdminForwardAuthAppNew,
});
