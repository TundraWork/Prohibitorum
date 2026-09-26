import { createFileRoute, redirect } from "@tanstack/react-router";
import {
  forwardAuthAppQueryOptions,
  forwardAuthAppsListOptions,
  managedApplicationsQueryOptions,
  sessionQueryOptions,
} from "@/api/queries";
import { AdminForwardAuthApp } from "@/pages/admin/forward-auth-apps/AdminForwardAuthApp";

/**
 * One forward-auth application's settings, addressed by Client ID.
 *
 * The Client ID is what the server names the application by everywhere — the
 * access policy, the icon URL, the manager list, the router configuration an
 * operator pastes — so it is the route segment rather than an id the console
 * hands out.
 *
 * ## Why this gate is not the management area's
 *
 * The page hangs off `_protected.admin`, not `_protected.admin._admin`, because
 * a delegated manager reaches it. Being admitted to the area is not enough on
 * its own, though: the area admits anyone who manages *some* application, and
 * this route is about one. So the loader asks the narrower question here —
 * administrator, or this specific application in the account's list — and sends
 * an account that manages only, say, an OIDC application home rather than
 * letting it read a page the server would answer 404 on anyway.
 *
 * `managedApplicationsQueryOptions` only says whether the account has *an*
 * application of each kind, so for the non-admin branch the list itself is the
 * check. That is one request against an endpoint the page's own sections read
 * from anyway, and its answer is what the guard needs: the rows the server
 * returned are the rows this account may act on.
 *
 * The application is read in the loader because every section draws from it —
 * the general form's saved values, the icon, the projection, and the host the
 * proxy snippet is generated from. Each section then reads whatever else it
 * needs for itself.
 */
export const Route = createFileRoute(
  "/_protected/admin/forward-auth-apps_/$clientId",
)({
  loader: async ({ context: { queryClient }, params: { clientId } }) => {
    const session = await queryClient.fetchQuery(sessionQueryOptions());
    if (session === null) {
      throw redirect({ to: "/login" });
    }
    if (session.role !== "admin") {
      const managed = await queryClient.fetchQuery(
        managedApplicationsQueryOptions(),
      );
      if (!managed.forwardAuth) {
        throw redirect({ to: "/" });
      }
      const list = await queryClient.fetchQuery(forwardAuthAppsListOptions());
      const mine = (list.items ?? []).some((app) => app.clientId === clientId);
      if (!mine) {
        throw redirect({ to: "/" });
      }
    }
    await queryClient.ensureQueryData(forwardAuthAppQueryOptions(clientId));
  },
  component: AdminForwardAuthApp,
});
