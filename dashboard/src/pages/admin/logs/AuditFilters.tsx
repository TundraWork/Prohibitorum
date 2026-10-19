import { Trans, useLingui } from "@lingui/react/macro";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { accountQueryOptions, accountSearchQueryOptions } from "@/api/queries";
import { quietQuery } from "@/app/query-client";
import {
  type EntityOption,
  EntityPicker,
} from "@/components/custom/EntityPicker";
import { SectionFailure } from "@/components/custom/Section";
import { Field, FieldLabel } from "@/components/ui/field";
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { AuditSearch } from "@/pages/admin/logs/audit-filters";
import {
  auditEventKeys,
  auditEventName,
  auditFactorKeys,
  auditFactorName,
  isAuditEvent,
  isAuditFactor,
} from "@/pages/admin/logs/audit-vocabulary";

/** The category and event controls' own choice for "no filter", which is not a value. */
const anyValue = "__any__";

/**
 * The category, the event and the account, each filtered by the server.
 *
 * The vocabulary does not say which events belong to which category, so the
 * event list is the same whatever the category. The directory is read when the
 * account list opens, not when the filter does, and a chosen account keeps its
 * name even when the current search would not list it.
 */
export function AuditFilters({
  search,
  onChange,
}: {
  search: AuditSearch;
  onChange: (patch: Partial<AuditSearch>) => void;
}) {
  const { t, i18n } = useLingui();
  const [accountSearch, setAccountSearch] = useState("");
  const [searching, setSearching] = useState(false);
  const candidates = useQuery(
    quietQuery({
      ...accountSearchQueryOptions(accountSearch),
      enabled: searching,
    }),
  );
  const chosen = useQuery(
    quietQuery({
      ...accountQueryOptions(Number(search.account ?? 0)),
      enabled: search.account !== undefined,
    }),
  );

  const options: EntityOption[] = (candidates.data?.items ?? []).map(
    (account) => ({
      id: String(account.id),
      label: account.displayName || account.username,
      ...(account.displayName ? { identifier: `@${account.username}` } : {}),
    }),
  );
  if (
    chosen.data &&
    !options.some((option) => option.id === String(chosen.data.id))
  ) {
    options.push({
      id: String(chosen.data.id),
      label: chosen.data.displayName || chosen.data.username,
      ...(chosen.data.displayName
        ? { identifier: `@${chosen.data.username}` }
        : {}),
    });
  }

  return (
    <div className="flex flex-col gap-3">
      {candidates.isError && (
        <SectionFailure
          title={
            <Trans id="admin.logs.filter.account.failed">
              The accounts could not be loaded
            </Trans>
          }
          error={candidates.error}
          onRetry={() => void candidates.refetch()}
        />
      )}

      <Field>
        <FieldLabel>
          <Trans id="admin.logs.filter.factor">Category</Trans>
        </FieldLabel>
        <Select
          value={search.factor === "" ? anyValue : search.factor}
          onValueChange={(next) => {
            if (next === anyValue) onChange({ factor: "" });
            else if (typeof next === "string" && isAuditFactor(next))
              onChange({ factor: next });
          }}
        >
          <SelectTrigger>
            <SelectValue>
              {(value: string) =>
                value === anyValue
                  ? t({
                      id: "admin.logs.filter.factor.any",
                      message: "Any category",
                    })
                  : auditFactorName(i18n, value)
              }
            </SelectValue>
          </SelectTrigger>
          <SelectPopup alignItemWithTrigger={false}>
            <SelectItem value={anyValue}>
              <Trans id="admin.logs.filter.factor.any">Any category</Trans>
            </SelectItem>
            {auditFactorKeys.map((factor) => (
              <SelectItem key={factor} value={factor}>
                {auditFactorName(i18n, factor)}
              </SelectItem>
            ))}
          </SelectPopup>
        </Select>
      </Field>

      <Field>
        <FieldLabel>
          <Trans id="admin.logs.filter.event">Event</Trans>
        </FieldLabel>
        <Select
          value={search.event === "" ? anyValue : search.event}
          onValueChange={(next) => {
            if (next === anyValue) onChange({ event: "" });
            else if (typeof next === "string" && isAuditEvent(next))
              onChange({ event: next });
          }}
        >
          <SelectTrigger>
            <SelectValue>
              {(value: string) =>
                value === anyValue
                  ? t({
                      id: "admin.logs.filter.event.any",
                      message: "Any event",
                    })
                  : auditEventName(i18n, value)
              }
            </SelectValue>
          </SelectTrigger>
          <SelectPopup alignItemWithTrigger={false}>
            <SelectItem value={anyValue}>
              <Trans id="admin.logs.filter.event.any">Any event</Trans>
            </SelectItem>
            {auditEventKeys.map((event) => (
              <SelectItem key={event} value={event}>
                {auditEventName(i18n, event)}
              </SelectItem>
            ))}
          </SelectPopup>
        </Select>
      </Field>

      <EntityPicker
        label={<Trans id="admin.logs.filter.account">Account</Trans>}
        searchLabel={t({
          id: "admin.logs.filter.account.search",
          message: "Search accounts",
        })}
        placeholder={t({
          id: "admin.logs.filter.account.any",
          message: "Any account",
        })}
        value={search.account === undefined ? [] : [search.account]}
        onValueChange={(next) => onChange({ account: next[0] })}
        onSearch={(query) => {
          setSearching(true);
          setAccountSearch(query);
        }}
        onOpenChange={(open) => open && setSearching(true)}
        options={options}
        loading={candidates.isFetching}
      />
    </div>
  );
}
