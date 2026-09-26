import { createFileRoute } from "@tanstack/react-router";
import { AdminSettings } from "@/pages/admin/AdminSettings";

// No loader: `/config` is already loaded by the root route, and every other
// read belongs to one section and is made by that section's panel.
export const Route = createFileRoute("/_protected/admin/_admin/settings")({
  component: AdminSettings,
});
