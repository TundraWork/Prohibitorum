import { createFileRoute, redirect } from "@tanstack/react-router";
import { sessionQueryOptions } from "@/api/queries";

/**
 * The administrator-only part of the management area, with no path of its own.
 *
 * The outer `_protected.admin` gate admits admins and accounts that manage at
 * least one downstream application, because delegated managers need the
 * application pages. Everything else in the area — the user directory, user
 * groups, invitations, identity providers, logs, settings, and the landing page —
 * is for administrators alone, and lives under this second gate. Splitting them
 * is what lets the outer gate be that permissive without giving a delegated
 * manager anything they were not assigned.
 *
 * The two move together: a new page belongs under `_admin` unless a non-admin
 * application manager is meant to reach it, and the sidebar entry that leads
 * there is hidden on the same test.
 */
export const Route = createFileRoute("/_protected/admin/_admin")({
  loader: async ({ context: { queryClient } }) => {
    const session = await queryClient.fetchQuery(sessionQueryOptions());
    if (session === null || session.role !== "admin") {
      throw redirect({ to: "/" });
    }
  },
});
