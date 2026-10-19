import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useQuery } from "@tanstack/react-query";
import { describeError } from "@/api/errors";
import { identityProvidersQueryOptions } from "@/api/queries";
import { quietQuery } from "@/app/query-client";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { Button } from "@/components/ui/button";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  advancedFilterIncomplete,
  identityFilterSet,
  isMatchOperator,
  type MatchOperator,
  type UserFilters,
} from "@/pages/admin/users/user-filters";

/** The provider control's own choice for "no provider", which is not a slug. */
const anyProvider = "__any__";

const operatorLabels = {
  exact: msg({ id: "admin.users.filter.match.exact", message: "Exact" }),
  prefix: msg({
    id: "admin.users.filter.match.prefix",
    message: "Starts with",
  }),
  contains: msg({
    id: "admin.users.filter.match.contains",
    message: "Contains",
  }),
} satisfies Record<MatchOperator, unknown>;

/**
 * The identity filter: provider, field, match and value.
 *
 * Providers and their searchable fields come from `GET /identity-providers`,
 * and only the operators the address accepts are offered. A partly filled
 * filter is kept in the address but not sent, since the server rejects it.
 */
export function AccountIdentityFilter({
  filters,
  onChange,
}: {
  filters: UserFilters;
  onChange: (patch: Partial<UserFilters>) => void;
}) {
  const { t, i18n } = useLingui();
  const providers = useQuery(quietQuery(identityProvidersQueryOptions()));
  const providerList = providers.data?.items ?? [];
  const selected = providerList.find(
    (provider) => provider.slug === filters.provider,
  );
  const fields = selected?.searchFields ?? [];
  const operators = (
    fields.find((field) => field.key === filters.field)?.operators ?? []
  ).filter(isMatchOperator);

  const providerName = (slug: string) =>
    providerList.find((provider) => provider.slug === slug)?.displayName ??
    slug;

  return (
    <div className="flex flex-col gap-4">
      {providers.isError && (
        <SurfaceAlert status="danger" role="alert">
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>
              <Trans id="admin.users.filter.providers-failed">
                The providers could not be loaded
              </Trans>
            </SurfaceAlert.Title>
            <SurfaceAlert.Description>
              {t(describeError(providers.error))}
            </SurfaceAlert.Description>
            <Button
              onClick={() => void providers.refetch()}
              variant="destructive"
              size="sm"
              className="mt-2 w-fit"
            >
              <Trans id="section.retry">Try again</Trans>
            </Button>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      )}
      <div className="flex flex-col gap-3">
        <Field disabled={providers.isPending}>
          <FieldLabel>
            <Trans id="admin.users.filter.providerLabel">Provider</Trans>
          </FieldLabel>
          <Select
            value={filters.provider === "" ? anyProvider : filters.provider}
            disabled={providers.isPending}
            onValueChange={(next) => {
              if (typeof next !== "string") return;
              // The fields belong to the provider, so everything after it resets.
              onChange({
                provider: next === anyProvider ? "" : next,
                field: "",
                value: "",
                match: "",
              });
            }}
          >
            <SelectTrigger>
              <SelectValue>
                {(value: string) =>
                  value === anyProvider
                    ? t({
                        id: "admin.users.filter.provider",
                        message: "Any provider",
                      })
                    : providerName(value)
                }
              </SelectValue>
            </SelectTrigger>
            <SelectPopup alignItemWithTrigger={false}>
              <SelectItem value={anyProvider}>
                <Trans id="admin.users.filter.provider">Any provider</Trans>
              </SelectItem>
              {providerList.map((provider) => (
                <SelectItem key={provider.slug} value={provider.slug}>
                  {provider.displayName}
                </SelectItem>
              ))}
            </SelectPopup>
          </Select>
        </Field>

        <Field disabled={filters.provider === ""}>
          <FieldLabel>
            <Trans id="admin.users.filter.fieldLabel">Field</Trans>
          </FieldLabel>
          <Select
            value={filters.field === "" ? null : filters.field}
            disabled={filters.provider === ""}
            onValueChange={(next) => {
              if (typeof next === "string")
                onChange({ field: next, value: "", match: "" });
            }}
          >
            <SelectTrigger>
              <SelectValue>
                {(value: string | null) =>
                  value === null ? (
                    <span className="text-muted-foreground">
                      <Trans id="admin.users.filter.field">
                        Choose a field
                      </Trans>
                    </span>
                  ) : (
                    <span className="font-mono">{value}</span>
                  )
                }
              </SelectValue>
            </SelectTrigger>
            <SelectPopup alignItemWithTrigger={false}>
              {fields.map((field) => (
                <SelectItem key={field.key} value={field.key}>
                  <span className="font-mono">{field.key}</span>
                </SelectItem>
              ))}
            </SelectPopup>
          </Select>
        </Field>

        <Field disabled={filters.field === ""}>
          <FieldLabel>
            <Trans id="admin.users.filter.matchLabel">Match</Trans>
          </FieldLabel>
          <Select
            value={filters.match === "" ? null : filters.match}
            disabled={filters.field === ""}
            onValueChange={(next) => {
              if (typeof next === "string" && isMatchOperator(next))
                onChange({ match: next });
            }}
          >
            <SelectTrigger>
              <SelectValue>
                {(value: string | null) =>
                  value === null ? (
                    <span className="text-muted-foreground">
                      <Trans id="admin.users.filter.match">
                        Choose a match
                      </Trans>
                    </span>
                  ) : isMatchOperator(value) ? (
                    i18n._(operatorLabels[value])
                  ) : (
                    value
                  )
                }
              </SelectValue>
            </SelectTrigger>
            <SelectPopup alignItemWithTrigger={false}>
              {operators.map((operator) => (
                <SelectItem key={operator} value={operator}>
                  {i18n._(operatorLabels[operator])}
                </SelectItem>
              ))}
            </SelectPopup>
          </Select>
        </Field>

        <Field>
          <FieldLabel>
            <Trans id="admin.users.filter.value">Value to match</Trans>
          </FieldLabel>
          <Input
            value={filters.value}
            autoComplete="off"
            onChange={(event) => onChange({ value: event.target.value })}
          />
        </Field>
      </div>

      {advancedFilterIncomplete(filters) && (
        <p className="text-muted-foreground text-xs" role="status">
          <Trans id="admin.users.filter.incomplete">
            Fill in the provider, field, match and value to apply this filter.
          </Trans>
        </p>
      )}

      <div className="flex justify-end">
        <Button
          disabled={!identityFilterSet(filters)}
          onClick={() =>
            onChange({ provider: "", field: "", value: "", match: "" })
          }
          variant="ghost"
          size="sm"
        >
          <Trans id="admin.users.filter.clear">Clear the filter</Trans>
        </Button>
      </div>
    </div>
  );
}
