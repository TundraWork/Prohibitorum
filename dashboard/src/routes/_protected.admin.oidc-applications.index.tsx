import { createFileRoute } from "@tanstack/react-router";
import { AdminOidcApplications } from "@/pages/admin/oidc-applications/AdminOidcApplications";

/**
 * Every OIDC application this account may see.
 *
 * The route carries no loader of its own: the list is not part of the first
 * paint the guard needs, and `_protected.admin` has already decided that this
 * account belongs in the management area at all. The page's own gate is the
 * list itself — the endpoint answers only with the applications the caller was
 * assigned, so an account that reaches here without being an administrator sees
 * exactly what it was given.
 */
export const Route = createFileRoute("/_protected/admin/oidc-applications/")({
  component: AdminOidcApplications,
});
