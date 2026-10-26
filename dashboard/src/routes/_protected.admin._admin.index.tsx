import { createFileRoute, redirect } from "@tanstack/react-router";

// The management area has no landing page of its own: its sections are the
// sidebar, so `/admin` opens the first of them.
export const Route = createFileRoute("/_protected/admin/_admin/")({
  beforeLoad: () => {
    throw redirect({ to: "/admin/users", replace: true });
  },
});
