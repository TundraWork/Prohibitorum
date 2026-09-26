import { createFileRoute } from "@tanstack/react-router";
import { AdminForwardAuthApps } from "@/pages/admin/forward-auth-apps/AdminForwardAuthApps";

export const Route = createFileRoute("/_protected/admin/forward-auth-apps/")({
  component: AdminForwardAuthApps,
});
