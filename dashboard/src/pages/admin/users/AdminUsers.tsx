import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { Link, useNavigate } from "@tanstack/react-router";
import { Pencil, Plus, SearchIcon, UserRound } from "lucide-react";
import { useCursorList } from "@/api/cursor-list";
import type { components } from "@/api/generated/schema";
import { accountsListOptions } from "@/api/queries";
import { quietQuery } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { EntityCell } from "@/components/custom/EntityCell";
import { FilterPopover } from "@/components/custom/FilterPopover";
import { InlineFailure } from "@/components/custom/InlineFailure";
import { APP_DATA_COLUMN } from "@/components/custom/ListCells";
import { PageHeader } from "@/components/custom/PageHeader";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import { Badge } from "@/components/ui/badge";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group";
import { Route } from "@/routes/_protected.admin._admin.users";
import { AccountIdentityFilter } from "./AccountIdentityFilter";
import {
  accountFilterQuery,
  filtersSet,
  identityFilterSet,
  type UserFilters,
} from "./user-filters";

type Account = components["schemas"]["AccountView"];

const openAccountMessage = msg({
  id: "admin.users.open",
  message: "Open {name}",
});

/** What a search and an identity filter both reset to. */
const noFilters = {
  q: "",
  provider: "",
  field: "",
  value: "",
  match: "",
  role: "",
  state: "",
} satisfies UserFilters;

/**
 * The account directory.
 *
 * The list is server-paged by cursor and its filters live in the URL, so the
 * rows always answer the question in the address bar. Loading more accumulates
 * rows in the query cache and deliberately does not touch the URL: a link into
 * the middle of a result set is a link to an answer with its question missing.
 *
 * `GET /accounts` supports the free-text search and the advanced identity
 * filter, and nothing else, so user creation goes through an invitation: the
 * server creates the account when the invite is redeemed, and there is no form
 * here that pretends otherwise.
 *
 * The toolbar sits above the table rather than inside its frame, so a failed
 * read leaves the search and the filters within reach: they are what a reader
 * changes to get a different answer.
 */
export function AdminUsers() {
  const { t, i18n } = useLingui();
  const navigate = useNavigate();
  const filters = Route.useSearch();

  const query = accountFilterQuery(filters);
  const list = useCursorList<Account>(
    quietQuery({
      queryKey: ["admin", "accounts", query],
      queryFn: ({ cursor, signal }) =>
        accountsListOptions(query).queryFn({
          signal,
          ...(cursor === undefined ? {} : { cursor }),
        }),
    }),
  );

  const setFilters = (patch: Partial<UserFilters>) => {
    void navigate({
      to: "/admin/users",
      search: { ...filters, ...patch },
      replace: true,
    });
  };
  const clearFilters = () => setFilters(noFilters);

  const columns: TableColumn<Account>[] = [
    {
      id: "account",
      header: <Trans id="admin.users.column.account">User</Trans>,
      cell: (account) => (
        <EntityCell
          shape="round"
          iconUrl={account.avatarUrl}
          name={account.displayName || account.username}
          identifier={`@${account.username}`}
          link={{
            to: "/admin/users/$id",
            params: { id: String(account.id) },
          }}
          dimmed={account.disabled}
          marks={
            account.role === "admin" ? (
              <Badge variant="info" size="sm">
                <Trans id="object.status.admin">Admin</Trans>
              </Badge>
            ) : null
          }
        />
      ),
    },
    {
      id: "lastSignInAt",
      width: APP_DATA_COLUMN,
      header: <Trans id="admin.users.column.lastSignIn">Last signed in</Trans>,
      cell: (account) =>
        account.lastSignInAt === undefined ? (
          <span className="text-muted-foreground">
            <Trans id="admin.users.never">Never</Trans>
          </span>
        ) : (
          <RelativeTime value={account.lastSignInAt} />
        ),
    },
    {
      align: "end",
      id: "actions",
      header: "",
      cell: (account) => (
        <Button
          aria-label={i18n._({
            ...openAccountMessage,
            values: { name: account.displayName || account.username },
          })}
          onClick={() =>
            void navigate({
              to: "/admin/users/$id",
              params: { id: String(account.id) },
            })
          }
          variant="ghost"
          size="icon-sm"
        >
          <Pencil size={16} aria-hidden="true" />
        </Button>
      ),
      pinned: true,
    },
  ];

  const failed = list.error !== null && list.error !== undefined;
  // What the server was actually asked: an unfinished identity filter is kept
  // in the address but not sent, so it cannot be the reason a list is empty.
  const narrowed = Object.keys(query).length > 0;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={<Trans id="admin.users.page.title">Users</Trans>}
        description={
          <Trans id="admin.users.page.description">
            The accounts that can sign in to this instance. Invite someone to
            add an account.
          </Trans>
        }
        actions={
          <Button render={<Link to="/admin/invitations/new" />}>
            <Plus aria-hidden="true" />
            <Trans id="admin.users.invite">Invite a user</Trans>
          </Button>
        }
      />

      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center gap-2">
          <InputGroup className="min-w-56 flex-1 sm:max-w-sm">
            <InputGroupAddon>
              <SearchIcon aria-hidden="true" />
            </InputGroupAddon>
            <InputGroupInput
              type="search"
              value={filters.q}
              autoComplete="off"
              onChange={(event) => setFilters({ q: event.target.value })}
              placeholder={t({
                id: "admin.users.search.placeholder",
                message: "Username or display name",
              })}
              aria-label={t({
                id: "admin.users.search",
                message: "Search users",
              })}
            />
          </InputGroup>
          <FilterPopover count={identityFilterSet(filters) ? 1 : 0}>
            <AccountIdentityFilter filters={filters} onChange={setFilters} />
          </FilterPopover>
          {filtersSet(filters) && (
            <Button variant="ghost" onClick={clearFilters}>
              <Trans id="admin.users.filters.clear">Clear filters</Trans>
            </Button>
          )}
        </div>

        {failed && <InlineFailure error={list.error} />}

        {/* A failed first read has no rows to show, and an empty table under
            the failure would say there are none. */}
        {(!failed || list.items.length > 0) && (
          <DataTable
            label={t({ id: "admin.users.table", message: "Users" })}
            columns={columns}
            rows={list.items}
            rowId={(account) => account.id}
            loading={list.loading}
            hasMore={list.hasMore}
            loadingMore={list.loadingMore}
            onLoadMore={list.loadMore}
            empty={
              narrowed ? (
                <TableEmptyState
                  icon={
                    <UserRound
                      size={18}
                      strokeWidth={1.75}
                      aria-hidden="true"
                    />
                  }
                  title={
                    <Trans id="admin.users.empty.filtered">
                      No account matches these filters
                    </Trans>
                  }
                  action={
                    <Button variant="outline" onClick={clearFilters}>
                      <Trans id="admin.users.filters.clear">
                        Clear filters
                      </Trans>
                    </Button>
                  }
                />
              ) : (
                <TableEmptyState
                  icon={
                    <UserRound
                      size={18}
                      strokeWidth={1.75}
                      aria-hidden="true"
                    />
                  }
                  title={<Trans id="admin.users.empty">No accounts yet</Trans>}
                  description={
                    <Trans id="admin.users.empty.description">
                      Invite someone and their account appears here once they
                      accept.
                    </Trans>
                  }
                />
              )
            }
          />
        )}
      </div>
    </div>
  );
}
