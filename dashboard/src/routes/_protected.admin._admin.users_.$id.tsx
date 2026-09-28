import { createFileRoute } from "@tanstack/react-router";
import { accountQueryOptions } from "@/api/queries";
import { AdminUser } from "@/pages/admin/AdminUser";
import { accountTabs, tabSearch } from "@/pages/console/tabs";

export const Route = createFileRoute("/_protected/admin/_admin/users_/$id")({
  validateSearch: tabSearch(accountTabs),
  loader: ({ context: { queryClient }, params: { id } }) =>
    queryClient.ensureQueryData(accountQueryOptions(Number(id))),
  component: AdminUser,
});
