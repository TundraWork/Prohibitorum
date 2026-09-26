import { createFileRoute } from "@tanstack/react-router";
import { accountQueryOptions } from "@/api/queries";
import { AdminUser } from "@/pages/admin/AdminUser";
import { accountTab } from "@/pages/console/tabs";

export const Route = createFileRoute("/_protected/admin/_admin/users_/$id")({
  validateSearch: (search: Record<string, unknown>) => ({
    tab: accountTab(search.tab),
  }),
  loader: ({ context: { queryClient }, params: { id } }) =>
    queryClient.ensureQueryData(accountQueryOptions(Number(id))),
  component: AdminUser,
});
