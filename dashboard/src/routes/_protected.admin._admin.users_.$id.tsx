import { createFileRoute, redirect } from "@tanstack/react-router";
import {
  accountCredentialsQueryOptions,
  accountIdentitiesQueryOptions,
  accountQueryOptions,
  accountSessionsQueryOptions,
  accountTokensQueryOptions,
} from "@/api/queries";
import { quietQuery } from "@/app/query-client";
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
  // The access lists sit above the sections a link may point at, so they load
  // first; a failed one is its section's to report, not the page's.
  loader: async ({ context: { queryClient }, params: { id } }) => {
    const accountId = Number(id);
    const [account] = await Promise.all([
      queryClient.ensureQueryData(accountQueryOptions(accountId)),
      queryClient.prefetchQuery(
        quietQuery(accountIdentitiesQueryOptions(accountId)),
      ),
      queryClient.prefetchQuery(
        quietQuery(accountCredentialsQueryOptions(accountId)),
      ),
      queryClient.prefetchQuery(
        quietQuery(accountSessionsQueryOptions(accountId)),
      ),
      queryClient.prefetchQuery(
        quietQuery(accountTokensQueryOptions(accountId)),
      ),
    ]);
    return account;
  },
  component: AdminUser,
});
