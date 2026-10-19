import { createFileRoute, redirect } from "@tanstack/react-router";
import { accountQueryOptions } from "@/api/queries";
import { AdminUser } from "@/pages/admin/users/AdminUser";
import { accountSearch } from "@/pages/console/tabs";

export const Route = createFileRoute("/_protected/admin/_admin/users_/$id")({
  validateSearch: accountSearch,
  // The page used to be three tabs; an old `?tab=` link lands on the section of
  // that name as a hash, which the router scrolls to like any other.
  beforeLoad: ({ search, params }) => {
    if (search.tab === undefined) return;
    throw redirect({
      to: "/admin/users/$id",
      params,
      search: {},
      hash: search.tab,
      replace: true,
    });
  },
  loader: ({ context: { queryClient }, params: { id } }) =>
    queryClient.ensureQueryData(accountQueryOptions(Number(id))),
  component: AdminUser,
});
