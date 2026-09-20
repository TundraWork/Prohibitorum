import { createFileRoute } from "@tanstack/react-router";
import { securityTab } from "@/pages/console/tabs";
import { Security } from "@/pages/Security";

export const Route = createFileRoute("/_protected/security")({
  validateSearch: (search: Record<string, unknown>) => ({
    tab: securityTab(search.tab),
  }),
  component: Security,
});
