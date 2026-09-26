import { createFileRoute, redirect } from "@tanstack/react-router";
import { sessionQueryOptions } from "@/api/queries";
import { AdminSamlApplicationNew } from "@/pages/admin/saml-applications/AdminSamlApplicationNew";

/**
 * Registering a SAML application, for administrators alone.
 *
 * A delegated manager may change everything about the applications they were
 * assigned, but the server refuses them `POST /saml-applications`: an
 * application arrives with an access policy and a manager list that its creator
 * cannot set, so creating one is the administrator's step. The gate therefore
 * lives here rather than in `_protected.admin`, which admits managers by design.
 *
 * The redirect lands on the console home rather than back on the list: the list
 * would be the honest answer, but the only way a manager reaches this route is
 * by typing it, and the home page names the sections they do have.
 */
export const Route = createFileRoute(
  "/_protected/admin/saml-applications_/new",
)({
  loader: async ({ context: { queryClient } }) => {
    const session = await queryClient.fetchQuery(sessionQueryOptions());
    if (session === null || session.role !== "admin") {
      throw redirect({ to: "/" });
    }
  },
  component: AdminSamlApplicationNew,
});
