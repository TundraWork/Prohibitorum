import {
  Chip,
  Label,
  ListBox,
  SearchField,
  Select,
  useOverlayState,
} from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, UserRound } from "lucide-react";
import { useCursorList } from "@/api/cursor-list";
import type { components } from "@/api/generated/schema";
import {
  accountsListOptions,
  identityProvidersQueryOptions,
} from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import {
  accountFilterQuery,
  advancedFilterIncomplete,
  type UserFilters,
} from "@/pages/admin/user-filters";
import { Route } from "@/routes/_protected.admin.users";

type Account = components["schemas"]["AccountView"];
type Locale = ReturnType<typeof useLingui>["i18n"];

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
  const { t, i18n } = useLingui();
  const navigate = useNavigate();
  const filters = Route.useSearch();
  const advanced = useOverlayState();

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
    <ConsoleCard title={<Trans id="admin.users.title">Users</Trans>}>
      <div className="flex flex-wrap items-end gap-2">
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
        <Button
          variant="secondary"
          onPress={() => advanced.setOpen(!advanced.isOpen)}
        >
          {advanced.isOpen ? (
            <ChevronDown size={16} aria-hidden="true" />
          ) : (
            <ChevronRight size={16} aria-hidden="true" />
          )}
          <Trans id="admin.users.filter.advanced">Identity filter</Trans>
        </Button>
        <Button onPress={() => void navigate({ to: "/admin/invitations/new" })}>
          <Trans id="admin.users.invite">Invite a user</Trans>
        </Button>
      </div>

      {advanced.isOpen && (
        <AdvancedIdentityFilter filters={filters} onChange={setFilter} />
      )}

      {list.error !== null && list.error !== undefined && (
        <SurfaceAlert status="danger" role="alert">
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>
              <Trans id="admin.users.error">
                The list could not be loaded. Try again.
              </Trans>
            </SurfaceAlert.Title>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      )}

      <DataTable
        label={t({ id: "admin.users.table", message: "Users" })}
        columns={userColumns(i18n, openAccount)}
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
    </ConsoleCard>
  );
}

function userColumns(
  i18n: Locale,
  open: (id: number) => void,
): TableColumn<Account>[] {
  const format = (value?: string) =>
    value === undefined
      ? "—"
      : new Intl.DateTimeFormat(i18n.locale, {
          dateStyle: "medium",
          timeStyle: "short",
        }).format(new Date(value));
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
      cell: (account) => format(account.lastSignInAt),
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
    <div className="flex flex-col gap-4 rounded-medium border border-separator p-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <Select
          className="w-full"
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

      <div className="flex flex-wrap gap-2">
        <Button
          variant="secondary"
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
