import { createFileRoute, redirect } from "@tanstack/react-router";
import {
  managedApplicationsQueryOptions,
  sessionQueryOptions,
} from "@/api/queries";

/**
 * The management area's first gate: the account either administers the instance
 * or manages at least one downstream application.
 *
 * Delegated application management is the reason this is not simply "admin". The
 * server lets an account manage the applications it was assigned to — reading
 * them, changing their configuration, restricting their access, rotating their
 * secrets — without making it an administrator, and an account that can do all
 * that needs the pages to do it on. Everything that is admin-only still sits
 * behind the second gate inside (`_protected.admin._admin`).
 *
 * The check runs in the loader rather than the component, so an account with
 * neither role is never handed the markup: typing `/admin/oidc-applications`
 * directly lands back on the console home, exactly as if the sidebar entry had
 * not been drawn.
 *
 * `fetchQuery` rather than `ensureQueryData`: this decides what the whole subtree
 * may do, so it is worth the request to be sure of it.
 */
export const Route = createFileRoute("/_protected/admin")({
  loader: async ({ context: { queryClient } }) => {
    const session = await queryClient.fetchQuery(sessionQueryOptions());
    if (session === null) {
      throw redirect({ to: "/login" });
    }
    if (session.role === "admin") return;
    const managed = await queryClient.fetchQuery(
      managedApplicationsQueryOptions(),
    );
    if (!managed.oidc && !managed.saml && !managed.forwardAuth) {
      throw redirect({ to: "/" });
    }
  },
});
