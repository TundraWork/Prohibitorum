import { createFileRoute } from "@tanstack/react-router";
import { clientIpQueryOptions, publicConfigQueryOptions } from "@/api/queries";
import { AdminSettings } from "@/pages/admin/AdminSettings";
import { settingsTab } from "@/pages/console/tabs";

export const Route = createFileRoute("/_protected/admin/settings")({
  validateSearch: (search: Record<string, unknown>) => ({
    tab: settingsTab(search.tab),
  }),
  loaderDeps: ({ search: { tab } }) => ({ tab }),
  // The name, maintenance notice and images come from `/config`; only the
  // network tab reads a setting `/config` does not publish, so its request
  // waits until that tab is the one being opened.
  loader: ({ context: { queryClient }, deps: { tab } }) =>
    Promise.all([
      queryClient.ensureQueryData(publicConfigQueryOptions()),
      tab === "network"
        ? queryClient.ensureQueryData(clientIpQueryOptions())
        : undefined,
    ]),
  component: AdminSettings,
});
