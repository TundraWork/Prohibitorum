import { createFileRoute } from "@tanstack/react-router";
import { profileTab } from "@/pages/console/tabs";
import { Profile } from "@/pages/Profile";

export const Route = createFileRoute("/_protected/profile")({
  validateSearch: (search: Record<string, unknown>) => ({
    tab: profileTab(search.tab),
  }),
  component: Profile,
});
