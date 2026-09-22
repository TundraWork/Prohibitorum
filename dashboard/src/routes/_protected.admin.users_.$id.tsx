import { createFileRoute } from "@tanstack/react-router";
import { AdminUser } from "@/pages/admin/AdminUser";
import { accountTab } from "@/pages/console/tabs";

export const Route = createFileRoute("/_protected/admin/users_/$id")({
  validateSearch: (search: Record<string, unknown>) => ({
    tab: accountTab(search.tab),
  }),
  component: AdminUser,
});
