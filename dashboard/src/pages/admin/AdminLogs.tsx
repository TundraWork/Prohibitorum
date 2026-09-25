import {
  Alert,
  Chip,
  DateField,
  DateRangePicker,
  Label,
  ListBox,
  Popover,
  RangeCalendar,
  Select,
} from "@heroui/react";
import { parseDate } from "@internationalized/date";
import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import {
  CalendarRange,
  RotateCw,
  ScrollText,
  SlidersHorizontal,
} from "lucide-react";
import { type ReactNode, useState } from "react";
import { useCursorList } from "@/api/cursor-list";
import type { components } from "@/api/generated/schema";
import {
  accountQueryOptions,
  accountSearchQueryOptions,
  auditEventsListOptions,
} from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import {
  type EntityOption,
  EntityPicker,
} from "@/components/custom/EntityPicker";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import {
  type AuditRange,
  type AuditSearch,
  activeFilterCount,
  auditQuery,
  auditRanges,
} from "@/pages/admin/audit-filters";
import {
  type AuditEvent,
  type AuditFactor,
  auditEventKeys,
  auditEvents,
  auditFactorKeys,
  auditFactors,
  failureEvents,
  isAuditEvent,
  isAuditFactor,
} from "@/pages/admin/audit-vocabulary";
import { Route } from "@/routes/_protected.admin.logs";

type AuditEventView = components["schemas"]["AuditEventView"];
type Locale = ReturnType<typeof useLingui>["i18n"];

/**
 * The audit log: what happened, to whom, from where, newest first.
 *
 * Read-only, and a list the page exists for, so it sits on the page background
 * like the user directory. The filters live in the URL; the server does all the
 * filtering and pages by a cursor bound to those filters, so a preset range is
 * pinned to the moment it was chosen (see `auditQuery`) and moves on only when
 * the filters change or the reader refreshes.
 *
 * A range still being chosen — custom, with a day missing — is not sent: the
 * table stands empty and says what is missing.
 */
export function AdminLogs() {
  const { t, i18n } = useLingui();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const search = Route.useSearch();
  const [anchor, setAnchor] = useState(() => Date.now());

  const query = auditQuery(search, anchor);
  const listOptions = auditEventsListOptions(
    query.status === "ready" ? query.filters : {},
  );
  const list = useCursorList<AuditEventView>({
    queryKey: listOptions.queryKey,
    queryFn: listOptions.queryFn,
    enabled: query.status === "ready",
  });

  const setSearch = (patch: Partial<AuditSearch>) => {
    setAnchor(Date.now());
    void navigate({
      to: "/admin/logs",
      search: { ...search, ...patch },
      replace: true,
    });
  };

  const refresh = () => {
    if (search.range === "all" || search.range === "custom") {
      // Nothing in the question moves with the clock, so ask it again from
      // the first page.
      void queryClient.resetQueries({ queryKey: listOptions.queryKey });
    } else {
      setAnchor(Date.now());
    }
  };

  const count = activeFilterCount(search);

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end gap-2">
        <Select
          aria-label={t({ id: "admin.logs.range", message: "Time range" })}
          className="w-44"
          value={search.range}
          onChange={(key) => {
            if (typeof key === "string")
              setSearch({ range: key as AuditRange });
          }}
        >
          <Select.Trigger>
            <Select.Value />
            <Select.Indicator />
          </Select.Trigger>
          <Select.Popover>
            <ListBox>
              {auditRanges.map((range) => (
                <ListBox.Item
                  id={range}
                  key={range}
                  textValue={t(rangeLabels[range])}
                >
                  {t(rangeLabels[range])}
                  <ListBox.ItemIndicator />
                </ListBox.Item>
              ))}
            </ListBox>
          </Select.Popover>
        </Select>

        {search.range === "custom" && (
          <CustomRange
            from={search.from}
            to={search.to}
            onChange={(from, to) => setSearch({ from, to })}
          />
        )}

        <Popover>
          <Button variant="secondary">
            <SlidersHorizontal size={16} aria-hidden="true" />
            <Trans id="admin.logs.filter">Filter</Trans>
            {count > 0 && (
              <Chip color="accent" size="sm" variant="soft">
                {count}
              </Chip>
            )}
          </Button>
          <Popover.Content placement="bottom end" className="w-80">
            <Popover.Dialog>
              <AuditFilters search={search} onChange={setSearch} />
            </Popover.Dialog>
          </Popover.Content>
        </Popover>

        <Button
          isIconOnly
          variant="tertiary"
          isPending={list.loading}
          aria-label={t({ id: "admin.logs.refresh", message: "Refresh" })}
          onPress={refresh}
        >
          <RotateCw size={16} aria-hidden="true" />
        </Button>
      </div>

      {list.error !== null && list.error !== undefined && (
        <Alert status="danger" role="alert">
          <Alert.Indicator />
          <Alert.Content>
            <Alert.Title>
              <Trans id="admin.logs.error">
                The log could not be loaded. Try again.
              </Trans>
            </Alert.Title>
          </Alert.Content>
        </Alert>
      )}

      <DataTable
        label={t({ id: "admin.logs.table", message: "Audit events" })}
        columns={auditColumns(i18n)}
        rows={list.items}
        rowId={(event) => event.id}
        loading={list.loading}
        hasMore={list.hasMore}
        loadingMore={list.loadingMore}
        onLoadMore={list.loadMore}
        expandedRow={eventDetail}
        empty={
          query.status === "incomplete" ? (
            <TableEmptyState
              icon={
                <CalendarRange
                  size={18}
                  strokeWidth={1.75}
                  aria-hidden="true"
                />
              }
              title={
                <span role="status">
                  {query.reason === "missing-dates" ? (
                    <Trans id="admin.logs.range.incomplete">
                      Choose a start and an end date
                    </Trans>
                  ) : (
                    <Trans id="admin.logs.range.reversed">
                      The start date is after the end date
                    </Trans>
                  )}
                </span>
              }
            />
          ) : (
            <TableEmptyState
              icon={
                <ScrollText size={18} strokeWidth={1.75} aria-hidden="true" />
              }
              title={
                <Trans id="admin.logs.empty">No events in this range</Trans>
              }
            />
          )
        }
      />
    </div>
  );
}

const rangeLabels: Record<AuditRange, MessageDescriptor> = {
  "1h": msg({ id: "admin.logs.range.1h", message: "Last hour" }),
  "24h": msg({ id: "admin.logs.range.24h", message: "Last 24 hours" }),
  "7d": msg({ id: "admin.logs.range.7d", message: "Last 7 days" }),
  "30d": msg({ id: "admin.logs.range.30d", message: "Last 30 days" }),
  all: msg({ id: "admin.logs.range.all", message: "All time" }),
  custom: msg({ id: "admin.logs.range.custom", message: "Custom range" }),
};

/** A factor's name, or the value itself when the vocabulary does not know it. */
function factorName(i18n: Locale, factor: string): string {
  return isAuditFactor(factor) ? i18n._(auditFactors[factor]) : factor;
}

function eventName(i18n: Locale, event: string): string {
  return isAuditEvent(event) ? i18n._(auditEvents[event]) : event;
}

/**
 * The time an event happened, to the second. Audit records are compared
 * against other logs by the moment, so this is the one table on the console
 * that does not say "3 days ago".
 */
function formatInstant(i18n: Locale, value: string): string {
  return new Intl.DateTimeFormat(i18n.locale, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hourCycle: "h23",
  }).format(new Date(value));
}

function auditColumns(i18n: Locale): TableColumn<AuditEventView>[] {
  return [
    {
      id: "at",
      header: <Trans id="admin.logs.column.at">Time</Trans>,
      cell: (event) => (
        <time dateTime={event.at} className="tabular-nums">
          {formatInstant(i18n, event.at)}
        </time>
      ),
    },
    {
      id: "account",
      header: <Trans id="admin.logs.column.account">Account</Trans>,
      cell: (event) =>
        event.accountId !== undefined && event.accountUsername ? (
          <Link
            className="font-medium"
            params={{ id: String(event.accountId) }}
            search={{ tab: "profile" as const }}
            to="/admin/users/$id"
          >
            {event.accountUsername}
          </Link>
        ) : (
          "—"
        ),
    },
    {
      id: "factor",
      header: <Trans id="admin.logs.column.factor">Category</Trans>,
      cell: (event) => factorName(i18n, event.factor),
    },
    {
      id: "event",
      header: <Trans id="admin.logs.column.event">Event</Trans>,
      cell: (event) =>
        failureEvents.has(event.event) ? (
          <Chip color="danger" size="sm" variant="soft">
            {eventName(i18n, event.event)}
          </Chip>
        ) : (
          eventName(i18n, event.event)
        ),
    },
    {
      id: "ip",
      header: <Trans id="admin.logs.column.ip">IP</Trans>,
      cell: (event) => event.ip || "—",
    },
  ];
}

/**
 * What an event carries beyond its row: the browser that sent it, the address
 * (repeated, since a narrow screen may have scrolled that column away), and the
 * details the writing handler recorded. Nothing is drawn for a missing item,
 * and an event with none of them has no detail to open.
 */
function eventDetail(event: AuditEventView): ReactNode | null {
  const hasDetail =
    event.detail !== undefined && Object.keys(event.detail).length > 0;
  if (!event.userAgent && !event.ip && !hasDetail) return null;
  return (
    <dl className="grid grid-cols-[max-content_minmax(0,1fr)] gap-x-6 gap-y-2 py-1 text-sm max-sm:grid-cols-1 max-sm:gap-y-1">
      {event.userAgent && (
        <>
          <dt className="text-muted">
            <Trans id="admin.logs.detail.userAgent">User-Agent</Trans>
          </dt>
          <dd className="break-words max-sm:mb-1">{event.userAgent}</dd>
        </>
      )}
      {event.ip && (
        <>
          <dt className="text-muted">
            <Trans id="admin.logs.detail.ip">IP</Trans>
          </dt>
          <dd className="max-sm:mb-1">{event.ip}</dd>
        </>
      )}
      {hasDetail && (
        <>
          <dt className="text-muted">
            <Trans id="admin.logs.detail.detail">Details</Trans>
          </dt>
          <dd>
            <pre className="overflow-x-auto font-mono text-xs">
              {JSON.stringify(event.detail, null, 2)}
            </pre>
          </dd>
        </>
      )}
    </dl>
  );
}

/**
 * The days of a custom range, as one control beside the range select. It only
 * reports a range once both ends are set; until then the URL keeps whatever it
 * had, and the page says which part is missing.
 */
function CustomRange({
  from,
  to,
  onChange,
}: {
  from: string;
  to: string;
  onChange: (from: string, to: string) => void;
}) {
  const { t } = useLingui();
  const value = parseRange(from, to);
  const label = t({ id: "admin.logs.range.dates", message: "Dates" });
  return (
    <DateRangePicker
      aria-label={label}
      className="w-72"
      value={value}
      onChange={(next) => {
        if (next) onChange(next.start.toString(), next.end.toString());
        else onChange("", "");
      }}
    >
      <DateField.Group fullWidth>
        <DateField.Input slot="start">
          {(segment) => <DateField.Segment segment={segment} />}
        </DateField.Input>
        <DateRangePicker.RangeSeparator />
        <DateField.Input slot="end">
          {(segment) => <DateField.Segment segment={segment} />}
        </DateField.Input>
        <DateField.Suffix>
          <DateRangePicker.Trigger>
            <DateRangePicker.TriggerIndicator />
          </DateRangePicker.Trigger>
        </DateField.Suffix>
      </DateField.Group>
      <DateRangePicker.Popover>
        <RangeCalendar aria-label={label}>
          <RangeCalendar.Header>
            <RangeCalendar.YearPickerTrigger>
              <RangeCalendar.YearPickerTriggerHeading />
              <RangeCalendar.YearPickerTriggerIndicator />
            </RangeCalendar.YearPickerTrigger>
            <RangeCalendar.NavButton slot="previous" />
            <RangeCalendar.NavButton slot="next" />
          </RangeCalendar.Header>
          <RangeCalendar.Grid>
            <RangeCalendar.GridHeader>
              {(day) => (
                <RangeCalendar.HeaderCell>{day}</RangeCalendar.HeaderCell>
              )}
            </RangeCalendar.GridHeader>
            <RangeCalendar.GridBody>
              {(date) => <RangeCalendar.Cell date={date} />}
            </RangeCalendar.GridBody>
          </RangeCalendar.Grid>
          <RangeCalendar.YearPickerGrid>
            <RangeCalendar.YearPickerGridBody>
              {({ year }) => <RangeCalendar.YearPickerCell year={year} />}
            </RangeCalendar.YearPickerGridBody>
          </RangeCalendar.YearPickerGrid>
        </RangeCalendar>
      </DateRangePicker.Popover>
    </DateRangePicker>
  );
}

/** Both days as calendar dates, or no value while either is missing or invalid. */
function parseRange(from: string, to: string) {
  try {
    return from && to ? { start: parseDate(from), end: parseDate(to) } : null;
  } catch {
    return null;
  }
}

/**
 * The category, the event and the account, each server-side filters. The
 * account is found by searching the directory, and a chosen one keeps its name
 * even when the current search would not return it.
 */
function AuditFilters({
  search,
  onChange,
}: {
  search: AuditSearch;
  onChange: (patch: Partial<AuditSearch>) => void;
}) {
  const { t, i18n } = useLingui();
  const [accountSearch, setAccountSearch] = useState("");
  const candidates = useQuery(accountSearchQueryOptions(accountSearch));
  const chosen = useQuery({
    ...accountQueryOptions(search.account ?? 0),
    enabled: search.account !== undefined,
  });

  const options: EntityOption[] = (candidates.data?.items ?? []).map(
    (account) => ({
      id: String(account.id),
      label: account.username,
      ...(account.displayName ? { description: account.displayName } : {}),
    }),
  );
  if (
    chosen.data &&
    !options.some((option) => option.id === String(chosen.data.id))
  ) {
    options.push({ id: String(chosen.data.id), label: chosen.data.username });
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-3">
        <Select
          className="w-full"
          variant="secondary"
          placeholder={t({
            id: "admin.logs.filter.factor.any",
            message: "Any category",
          })}
          value={search.factor === "" ? null : search.factor}
          onChange={(key) =>
            onChange({ factor: (key ?? "") as AuditFactor | "" })
          }
        >
          <Label>
            <Trans id="admin.logs.filter.factor">Category</Trans>
          </Label>
          <Select.Trigger>
            <Select.Value />
            <Select.ClearButton />
            <Select.Indicator />
          </Select.Trigger>
          <Select.Popover>
            <ListBox>
              {auditFactorKeys.map((factor) => (
                <ListBox.Item
                  id={factor}
                  key={factor}
                  textValue={factorName(i18n, factor)}
                >
                  {factorName(i18n, factor)}
                  <ListBox.ItemIndicator />
                </ListBox.Item>
              ))}
            </ListBox>
          </Select.Popover>
        </Select>

        <Select
          className="w-full"
          variant="secondary"
          placeholder={t({
            id: "admin.logs.filter.event.any",
            message: "Any event",
          })}
          value={search.event === "" ? null : search.event}
          onChange={(key) =>
            onChange({ event: (key ?? "") as AuditEvent | "" })
          }
        >
          <Label>
            <Trans id="admin.logs.filter.event">Event</Trans>
          </Label>
          <Select.Trigger>
            <Select.Value />
            <Select.ClearButton />
            <Select.Indicator />
          </Select.Trigger>
          <Select.Popover>
            <ListBox>
              {auditEventKeys.map((event) => (
                <ListBox.Item
                  id={event}
                  key={event}
                  textValue={eventName(i18n, event)}
                >
                  {eventName(i18n, event)}
                  <ListBox.ItemIndicator />
                </ListBox.Item>
              ))}
            </ListBox>
          </Select.Popover>
        </Select>

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
          variant="secondary"
          value={search.account === undefined ? [] : [String(search.account)]}
          onValueChange={(next) => {
            const id = Number(next[0]);
            onChange({ account: next[0] === undefined ? undefined : id });
          }}
          onSearch={setAccountSearch}
          options={options}
          loading={candidates.isFetching}
        />
      </div>

      <div className="flex justify-end">
        <Button
          size="sm"
          variant="tertiary"
          isDisabled={activeFilterCount(search) === 0}
          onPress={() =>
            onChange({ factor: "", event: "", account: undefined })
          }
        >
          <Trans id="admin.logs.filter.clear">Clear filters</Trans>
        </Button>
      </div>
    </div>
  );
}
