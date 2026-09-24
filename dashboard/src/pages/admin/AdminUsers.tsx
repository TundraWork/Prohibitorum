import {
  Alert,
  Chip,
  Label,
  ListBox,
  Popover,
  SearchField,
  Select,
} from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { SlidersHorizontal, UserRound } from "lucide-react";
import { useCursorList } from "@/api/cursor-list";
import type { components } from "@/api/generated/schema";
import {
  accountsListOptions,
  identityProvidersQueryOptions,
} from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import {
  accountFilterQuery,
  advancedFilterIncomplete,
  type UserFilters,
} from "@/pages/admin/user-filters";
import { Route } from "@/routes/_protected.admin.users";

type Account = components["schemas"]["AccountView"];

/**
 * The account directory.
 *
 * The list is server-paged by cursor and its filters live in the URL, so the
 * rows always answer the question in the address bar. Loading more accumulates
 * rows in the query cache and deliberately does not touch the URL: a link into
 * the middle of a result set is a link to an answer with its question missing.
 *
 * `GET /accounts` supports the free-text search and the advanced identity
 * filter, and nothing else, so user creation goes through an invitation — the
 * server creates the account when the invite is redeemed, and there is no form
 * here that pretends otherwise.
 */
export function AdminUsers() {
  const { t } = useLingui();
  const navigate = useNavigate();
  const filters = Route.useSearch();

  const query = accountFilterQuery(filters);
  const list = useCursorList<Account>({
    queryKey: ["admin", "accounts", query],
    queryFn: ({ cursor, signal }) =>
      accountsListOptions(query).queryFn({ cursor, signal }),
  });

  const openAccount = (id: number) => {
    void navigate({
      to: "/admin/users/$id",
      params: { id: String(id) },
      search: { tab: "profile" as const },
    });
  };

  const setFilter = (patch: Partial<UserFilters>) => {
    void navigate({
      to: "/admin/users",
      search: { ...filters, ...patch },
      replace: true,
    });
  };

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end gap-2">
        <Button onPress={() => void navigate({ to: "/admin/invitations/new" })}>
          <Trans id="admin.users.invite">Invite a user</Trans>
        </Button>
        <SearchField
          aria-label={t({ id: "admin.users.search", message: "Search users" })}
          className="min-w-56 flex-1"
          value={filters.q}
          onChange={(value) => setFilter({ q: value })}
        >
          <SearchField.Group>
            <SearchField.SearchIcon />
            <SearchField.Input
              placeholder={t({
                id: "admin.users.search.placeholder",
                message: "Username or display name",
              })}
            />
          </SearchField.Group>
        </SearchField>
        <Popover>
          <Button variant="secondary">
            <SlidersHorizontal size={16} aria-hidden="true" />
            <Trans id="admin.users.filter.advanced">Identity filter</Trans>
            {filters.provider !== "" && (
              <Chip color="accent" size="sm" variant="soft">
                <Trans id="admin.users.filter.on">On</Trans>
              </Chip>
            )}
          </Button>
          <Popover.Content placement="bottom end" className="w-80">
            <Popover.Dialog>
              <AdvancedIdentityFilter filters={filters} onChange={setFilter} />
            </Popover.Dialog>
          </Popover.Content>
        </Popover>
      </div>

      {list.error !== null && list.error !== undefined && (
        <Alert status="danger" role="alert">
          <Alert.Indicator />
          <Alert.Content>
            <Alert.Title>
              <Trans id="admin.users.error">
                The list could not be loaded. Try again.
              </Trans>
            </Alert.Title>
          </Alert.Content>
        </Alert>
      )}

      <DataTable
        label={t({ id: "admin.users.table", message: "Users" })}
        columns={userColumns(openAccount)}
        rows={list.items}
        rowId={(account) => account.id}
        loading={list.loading}
        hasMore={list.hasMore}
        loadingMore={list.loadingMore}
        onLoadMore={list.loadMore}
        empty={
          <TableEmptyState
            icon={<UserRound size={18} strokeWidth={1.75} aria-hidden="true" />}
            title={<Trans id="admin.users.empty">No users match</Trans>}
          />
        }
      />
    </div>
  );
}

function userColumns(open: (id: number) => void): TableColumn<Account>[] {
  return [
    {
      id: "username",
      header: <Trans id="admin.users.column.username">Username</Trans>,
      cell: (account) => (
        <Link
          className="font-medium"
          params={{ id: String(account.id) }}
          search={{ tab: "profile" as const }}
          to="/admin/users/$id"
        >
          {account.username}
        </Link>
      ),
    },
    {
      id: "displayName",
      header: <Trans id="admin.users.column.displayName">Display name</Trans>,
      cell: (account) => account.displayName || "—",
    },
    {
      id: "role",
      header: <Trans id="admin.users.column.role">Role</Trans>,
      cell: (account) =>
        account.role === "admin" ? (
          <Chip color="accent" size="sm" variant="soft">
            <Trans id="admin.users.role.admin">Admin</Trans>
          </Chip>
        ) : (
          <Trans id="admin.users.role.user">User</Trans>
        ),
    },
    {
      id: "state",
      header: <Trans id="admin.users.column.state">State</Trans>,
      cell: (account) =>
        account.disabled ? (
          <Chip color="danger" size="sm" variant="soft">
            <Trans id="admin.users.state.disabled">Disabled</Trans>
          </Chip>
        ) : (
          <Chip color="success" size="sm" variant="soft">
            <Trans id="admin.users.state.enabled">Enabled</Trans>
          </Chip>
        ),
    },
    {
      align: "end",
      id: "lastSignInAt",
      header: <Trans id="admin.users.column.lastSignIn">Last signed in</Trans>,
      cell: (account) =>
        account.lastSignInAt === undefined ? (
          "—"
        ) : (
          <RelativeTime value={account.lastSignInAt} />
        ),
    },
    {
      id: "actions",
      header: <Trans id="admin.column.actions">Actions</Trans>,
      cell: (account) => (
        <Button size="sm" variant="secondary" onPress={() => open(account.id)}>
          <Trans id="admin.users.edit">Edit</Trans>
        </Button>
      ),
      pinned: true,
    },
  ];
}

/**
 * The advanced identity filter: which provider, which of its searchable fields,
 * which operator, and the value to compare.
 *
 * The provider and field vocabularies come from `GET /identity-providers`,
 * which publishes the fields each provider can be searched on and the operators
 * each accepts. Nothing is hardcoded here, so a provider added on the server
 * appears in this filter without a matching change in the dashboard.
 *
 * A partly filled filter is reported as unfinished rather than sent: the server
 * rejects an incomplete one, so the list keeps showing its previous result and
 * says why.
 */
function AdvancedIdentityFilter({
  filters,
  onChange,
}: {
  filters: UserFilters;
  onChange: (patch: Partial<UserFilters>) => void;
}) {
  const { t } = useLingui();
  const providers = useQuery(identityProvidersQueryOptions());
  const providerList = providers.data?.items ?? [];
  const selected = providerList.find(
    (provider) => provider.slug === filters.provider,
  );
  const fields = selected?.searchFields ?? [];
  const operators = (
    fields.find((field) => field.key === filters.field)?.operators ?? []
  ).map((operator) => ({ operator }));

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-3">
        <Select
          className="w-full"
          variant="secondary"
          isDisabled={providers.isPending}
          placeholder={t({
            id: "admin.users.filter.provider",
            message: "Any provider",
          })}
          value={filters.provider === "" ? null : filters.provider}
          onChange={(key) =>
            // The field list belongs to the provider, so both reset together.
            onChange({
              provider: String(key ?? ""),
              field: "",
              value: "",
              match: "",
            })
          }
        >
          <Label>
            <Trans id="admin.users.filter.providerLabel">Provider</Trans>
          </Label>
          <Select.Trigger>
            <Select.Value />
            <Select.Indicator />
          </Select.Trigger>
          <Select.Popover>
            <ListBox>
              {providerList.map((provider) => (
                <ListBox.Item
                  id={provider.slug}
                  key={provider.slug}
                  textValue={provider.displayName}
                >
                  {provider.displayName}
                  <ListBox.ItemIndicator />
                </ListBox.Item>
              ))}
            </ListBox>
          </Select.Popover>
        </Select>

        <Select
          className="w-full"
          variant="secondary"
          isDisabled={filters.provider === ""}
          placeholder={t({
            id: "admin.users.filter.field",
            message: "Choose a field",
          })}
          value={filters.field === "" ? null : filters.field}
          onChange={(key) =>
            onChange({ field: String(key ?? ""), value: "", match: "" })
          }
        >
          <Label>
            <Trans id="admin.users.filter.fieldLabel">Field</Trans>
          </Label>
          <Select.Trigger>
            <Select.Value />
            <Select.Indicator />
          </Select.Trigger>
          <Select.Popover>
            <ListBox>
              {fields.map((field) => (
                <ListBox.Item
                  id={field.key}
                  key={field.key}
                  textValue={field.key}
                >
                  {field.key}
                  <ListBox.ItemIndicator />
                </ListBox.Item>
              ))}
            </ListBox>
          </Select.Popover>
        </Select>

        <Select
          className="w-full"
          variant="secondary"
          isDisabled={filters.field === ""}
          placeholder={t({
            id: "admin.users.filter.match",
            message: "Choose a match",
          })}
          value={filters.match === "" ? null : filters.match}
          onChange={(key) =>
            onChange({ match: String(key ?? "") as UserFilters["match"] })
          }
        >
          <Label>
            <Trans id="admin.users.filter.matchLabel">Match</Trans>
          </Label>
          <Select.Trigger>
            <Select.Value />
            <Select.Indicator />
          </Select.Trigger>
          <Select.Popover>
            <ListBox>
              {operators.map(({ operator }) => (
                <ListBox.Item id={operator} key={operator} textValue={operator}>
                  {operator}
                  <ListBox.ItemIndicator />
                </ListBox.Item>
              ))}
            </ListBox>
          </Select.Popover>
        </Select>

        <SearchField
          variant="secondary"
          aria-label={t({
            id: "admin.users.filter.value",
            message: "Value to match",
          })}
          value={filters.value}
          onChange={(value) => onChange({ value })}
        >
          <SearchField.Group>
            <SearchField.SearchIcon />
            <SearchField.Input
              placeholder={t({
                id: "admin.users.filter.valuePlaceholder",
                message: "Value",
              })}
            />
          </SearchField.Group>
        </SearchField>
      </div>

      {advancedFilterIncomplete(filters) && (
        <p className="text-xs text-muted" role="status">
          <Trans id="admin.users.filter.incomplete">
            Fill in the provider, field, match and value to apply this filter.
          </Trans>
        </p>
      )}

      <div className="flex justify-end">
        <Button
          size="sm"
          variant="tertiary"
          isDisabled={filters.provider === ""}
          onPress={() =>
            onChange({ provider: "", field: "", value: "", match: "" })
          }
        >
          <Trans id="admin.users.filter.clear">Clear the filter</Trans>
        </Button>
      </div>
    </div>
  );
}
