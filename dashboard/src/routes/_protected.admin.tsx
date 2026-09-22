import { createFileRoute, redirect } from "@tanstack/react-router";
import { sessionQueryOptions } from "@/api/queries";

/**
 * The management area, behind the console's session check and one more gate on
 * top of it: the account has to be an admin.
 *
 * The check runs in the loader rather than the component so a non-admin is
 * never handed the markup at all — typing `/admin/users` directly lands back on
 * the console home, exactly as if the sidebar entry had not been drawn. The
 * sidebar hides the group for the same reason; this is the half that holds when
 * the URL is not the sidebar's.
 *
 * `fetchQuery` rather than `ensureQueryData`: the role decides what the whole
 * subtree may do, so it is worth the one request to be sure of it.
 */
export const Route = createFileRoute("/_protected/admin")({
  loader: async ({ context: { queryClient } }) => {
    const session = await queryClient.fetchQuery(sessionQueryOptions());
    if (session === null || session.role !== "admin") {
      throw redirect({ to: "/" });
    }
  },
});
