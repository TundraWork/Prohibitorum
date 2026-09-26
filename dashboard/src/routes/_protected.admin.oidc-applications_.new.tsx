import { createFileRoute, redirect } from "@tanstack/react-router";
import { sessionQueryOptions } from "@/api/queries";
import { AdminOidcApplicationNew } from "@/pages/admin/oidc-applications/AdminOidcApplicationNew";

/**
 * Creating an application, for administrators alone.
 *
 * A delegated manager may change everything about the applications they were
 * assigned, but the server refuses them `POST /oidc-applications`: an
 * application is created with an access policy and a manager list the creator
 * has no way to set, so creating one is the administrator's step. The gate is
 * here rather than in `_protected.admin`, which admits managers by design.
 *
 * The redirect lands on the console home rather than back on this page's list:
 * the list would be the honest answer, but a manager has no way into this
 * route's guard except by typing it, and the home page names the sections they
 * do have.
 */
export const Route = createFileRoute(
  "/_protected/admin/oidc-applications_/new",
)({
  loader: async ({ context: { queryClient } }) => {
    const session = await queryClient.fetchQuery(sessionQueryOptions());
    if (session === null || session.role !== "admin") {
      throw redirect({ to: "/" });
    }
  },
  component: AdminOidcApplicationNew,
});
