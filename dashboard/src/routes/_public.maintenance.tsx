import { createFileRoute, redirect } from "@tanstack/react-router";
import { publicConfigQueryOptions, sessionQueryOptions } from "@/api/queries";
import { MaintenancePage } from "@/pages/public/Maintenance";

export const Route = createFileRoute("/_public/maintenance")({
  // Read afresh: a page that says the service is down has to be right about it.
  loader: async ({ context: { queryClient } }) => {
    const [config] = await Promise.all([
      queryClient.query(publicConfigQueryOptions()),
      queryClient.ensureQueryData(sessionQueryOptions()),
    ]);
    if (!config.maintenanceMode) throw redirect({ to: "/" });
  },
  component: MaintenancePage,
});
