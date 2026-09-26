import { createFileRoute, redirect } from "@tanstack/react-router";
import {
  managedApplicationsQueryOptions,
  samlAppQueryOptions,
  sessionQueryOptions,
} from "@/api/queries";
import { AdminSamlApplication } from "@/pages/admin/saml-applications/AdminSamlApplication";

/**
 * One SAML application's settings, addressed by the server's own integer id.
 *
 * A SAML application has no identifier the administrator chose: the Entity ID is
 * a property of the record rather than its name, and the console never asks for
 * one, so the route segment is the id the server assigned.
 *
 * ## Why this gate is not the management area's
 *
 * The page hangs off `_protected.admin` rather than `_protected.admin._admin`,
 * because a delegated manager reaches it. Admission to the area is not enough on
 * its own: the area admits anyone who manages *some* application, and this route
 * is about one kind. So the loader asks the narrower question here —
 * administrator, or a SAML application in the account's own list — and sends
 * someone who manages only, say, a forward-auth application home rather than
 * letting them open a page the server would answer 404 on anyway.
 *
 * The application is read in the loader because every section draws from it: the
 * general form, the projection form, the metadata lists and the danger rows.
 * Each section then reads whatever else it needs — the access panel, the icon —
 * for itself.
 */
export const Route = createFileRoute(
  "/_protected/admin/saml-applications_/$id",
)({
  loader: async ({ context: { queryClient }, params: { id } }) => {
    // The segment is typed by hand as easily as it is linked to, so a
    // non-numeric one is refused here: `NaN` would otherwise be sent as a path
    // parameter and come back as a validation error rather than "no such page".
    const applicationId = Number(id);
    if (!Number.isInteger(applicationId) || applicationId <= 0) {
      throw redirect({ to: "/" });
    }
    const session = await queryClient.fetchQuery(sessionQueryOptions());
    if (session === null) {
      throw redirect({ to: "/login" });
    }
    if (session.role !== "admin") {
      const managed = await queryClient.fetchQuery(
        managedApplicationsQueryOptions(),
      );
      if (!managed.saml) {
        throw redirect({ to: "/" });
      }
    }
    await queryClient.ensureQueryData(samlAppQueryOptions(applicationId));
  },
  component: AdminSamlApplication,
});
