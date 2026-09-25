import { createFileRoute } from "@tanstack/react-router";
import { AdminSettings } from "@/pages/admin/AdminSettings";
import { settingsTab } from "@/pages/console/tabs";

// No loader: `/config` is already loaded by the root route, and every other
// read belongs to one tab and is made by that tab's panel (see `ConsoleTabs`).
export const Route = createFileRoute("/_protected/admin/settings")({
  validateSearch: (search: Record<string, unknown>) => ({
    tab: settingsTab(search.tab),
  }),
  component: AdminSettings,
});
