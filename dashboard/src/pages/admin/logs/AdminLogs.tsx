import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { CalendarRange, RotateCw, ScrollText } from "lucide-react";
import { type ReactNode, useState } from "react";
import { useCursorList } from "@/api/cursor-list";
import type { components } from "@/api/generated/schema";
import { auditEventsListOptions } from "@/api/queries";
import { quietQuery } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { FilterPopover } from "@/components/custom/FilterPopover";
import { JsonBlock } from "@/components/custom/JsonBlock";
import { PageHeader } from "@/components/custom/PageHeader";
import { LaterPageFailure, SectionFailure } from "@/components/custom/Section";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { AuditDateRange } from "@/pages/admin/logs/AuditDateRange";
import { AuditFilters } from "@/pages/admin/logs/AuditFilters";
import {
  type AuditRange,
  type AuditSearch,
  activeFilterCount,
  auditQuery,
  auditRanges,
} from "@/pages/admin/logs/audit-filters";
import {
  auditEventName,
  auditFactorName,
  failureEvents,
  isAuditEvent,
  isAuditFactor,
} from "@/pages/admin/logs/audit-vocabulary";
import { Route } from "@/routes/_protected.admin._admin.logs";

type AuditEventView = components["schemas"]["AuditEventView"];
type Locale = ReturnType<typeof useLingui>["i18n"];

const rangeLabels: Record<AuditRange, MessageDescriptor> = {
  "1h": msg({ id: "admin.logs.range.1h", message: "Last hour" }),
  "24h": msg({ id: "admin.logs.range.24h", message: "Last 24 hours" }),
  "7d": msg({ id: "admin.logs.range.7d", message: "Last 7 days" }),
  "30d": msg({ id: "admin.logs.range.30d", message: "Last 30 days" }),
  all: msg({ id: "admin.logs.range.all", message: "All time" }),
  custom: msg({ id: "admin.logs.range.custom", message: "Custom range" }),
};

const noFilters = { factor: "", event: "", account: undefined } as const;

/**
 * The audit log: what happened, to whom, from where, newest first.
 *
 * Read-only, and the list the page exists for. The filters live in the URL; the
 * server does all the filtering and pages by a cursor bound to those filters,
 * so a preset range is pinned to the moment it was chosen (see `auditQuery`) and
 * moves on only when the filters change or the reader refreshes. The toolbar
 * sits outside the table's frame so a failed read leaves the filters within
 * reach.
 *
 * A range still being chosen — custom, with a day missing — is not sent: the
 * table stands empty and says what is missing.
 */
export function AdminLogs() {
  const { t, i18n } = useLingui();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const search = Route.useSearch();
  // A preset reaches back from the moment its filters were chosen, so the
  // anchor moves with the address rather than ahead of it, which would ask
  // once more for the filters being left.
  const searchKey = JSON.stringify(search);
  const [stored, setAnchored] = useState({ key: searchKey, at: Date.now() });
  let anchored = stored;
  if (stored.key !== searchKey) {
    anchored = { key: searchKey, at: Date.now() };
    setAnchored(anchored);
  }
  const anchor = anchored.at;

  const query = auditQuery(search, anchor);
  const ready = query.status === "ready";
  const listOptions = auditEventsListOptions(ready ? query.filters : {});
  const list = useCursorList<AuditEventView>({
    ...quietQuery({
      queryKey: listOptions.queryKey,
      queryFn: listOptions.queryFn,
    }),
    enabled: ready,
  });
  // An unfinished range asks for nothing, so rows cached under the unfiltered
  // question must not stand in for it.
  const rows = ready ? list.items : [];

  const setSearch = (patch: Partial<AuditSearch>) => {
    void navigate({
      to: "/admin/logs",
      search: { ...search, ...patch },
      replace: true,
    });
  };
  const clearFilters = () => setSearch(noFilters);

  const refresh = () => {
    if (search.range === "all" || search.range === "custom") {
      // Nothing in the question moves with the clock, so ask it again from
      // the first page.
      void queryClient.resetQueries({ queryKey: listOptions.queryKey });
    } else {
      setAnchored({ key: searchKey, at: Date.now() });
    }
  };

  const count = activeFilterCount(search);
  const failed = ready && list.error !== null && list.error !== undefined;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={<Trans id="admin.logs.page.title">Logs</Trans>}
        description={
          <Trans id="admin.logs.page.description">
            Sign-ins, changes and other security events recorded on this
            instance, newest first.
          </Trans>
        }
      />

      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center gap-2">
          <Select
            value={search.range}
            onValueChange={(next) => {
              if (typeof next === "string")
                setSearch({ range: next as AuditRange });
            }}
          >
            <SelectTrigger
              aria-label={t({ id: "admin.logs.range", message: "Time range" })}
              className="w-44"
            >
              <SelectValue>
                {(value: AuditRange) => i18n._(rangeLabels[value])}
              </SelectValue>
            </SelectTrigger>
            <SelectPopup alignItemWithTrigger={false}>
              {auditRanges.map((range) => (
                <SelectItem key={range} value={range}>
                  {i18n._(rangeLabels[range])}
                </SelectItem>
              ))}
            </SelectPopup>
          </Select>

          {search.range === "custom" && (
            <AuditDateRange
              from={search.from}
              to={search.to}
              onChange={(from, to) => setSearch({ from, to })}
            />
          )}

          <FilterPopover count={count} align="start">
            <AuditFilters search={search} onChange={setSearch} />
          </FilterPopover>

          {count > 0 && (
            <Button variant="ghost" onClick={clearFilters}>
              <Trans id="admin.logs.filter.clear">Clear filters</Trans>
            </Button>
          )}

          <Button
            pending={list.loading}
            aria-label={t({ id: "admin.logs.refresh", message: "Refresh" })}
            onClick={refresh}
            variant="ghost"
            size="icon"
            className="sm:ms-auto"
          >
            <RotateCw size={16} aria-hidden="true" />
          </Button>
        </div>

        {failed && rows.length === 0 && (
          <SectionFailure
            error={list.error}
            onRetry={list.refetch}
            title={
              <Trans id="admin.logs.error">
                The audit log could not be loaded.
              </Trans>
            }
          />
        )}

        {/* A failed first read has no rows to show, and an empty table under
            the failure would say there are none. */}
        {(!failed || rows.length > 0) && (
          <DataTable
            label={t({ id: "admin.logs.table", message: "Audit events" })}
            columns={auditColumns(i18n)}
            rows={rows}
            rowId={(event) => event.id}
            loading={ready && list.loading}
            hasMore={ready && list.hasMore}
            loadingMore={list.loadingMore}
            onLoadMore={list.loadMore}
            expandedRow={(event) => eventDetail(i18n, event)}
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
                    <ScrollText
                      size={18}
                      strokeWidth={1.75}
                      aria-hidden="true"
                    />
                  }
                  title={
                    <Trans id="admin.logs.empty">No events in this range</Trans>
                  }
                  {...(count > 0
                    ? {
                        action: (
                          <Button variant="outline" onClick={clearFilters}>
                            <Trans id="admin.logs.filter.clear">
                              Clear filters
                            </Trans>
                          </Button>
                        ),
                      }
                    : {})}
                />
              )
            }
          />
        )}
        {failed && rows.length > 0 && (
          <LaterPageFailure
            error={list.error}
            onRetry={list.loadMore}
            title={
              <Trans id="admin.logs.more_failed">
                The rest of the audit log could not be loaded.
              </Trans>
            }
          />
        )}
      </div>
    </div>
  );
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

/** A value the vocabulary does not know, kept exactly as the server sent it. */
function Raw({ children }: { children: string }) {
  return (
    <span data-raw="" className="font-mono text-xs">
      {children}
    </span>
  );
}

/** An event's account as the list names it: the username, else the number. */
function accountText(event: AuditEventView): string {
  return event.accountUsername
    ? `@${event.accountUsername}`
    : `#${event.accountId}`;
}

/** An event's name, a failure drawn as a red badge and an unknown one as sent. */
function eventLabel(i18n: Locale, event: AuditEventView): ReactNode {
  if (!isAuditEvent(event.event)) return <Raw>{event.event}</Raw>;
  const name = auditEventName(i18n, event.event);
  return failureEvents.has(event.event) ? (
    <Badge variant="error" data-tone="failure">
      {name}
    </Badge>
  ) : (
    name
  );
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
      // A fixed track like the lists elsewhere, so a long username truncates
      // here instead of pushing the address out of the frame.
      width: "w-56 max-w-56",
      header: <Trans id="admin.logs.column.account">Account</Trans>,
      cell: (event) =>
        event.accountId === undefined ? (
          <span className="text-muted-foreground">
            <Trans id="admin.logs.no-account">No account</Trans>
          </span>
        ) : (
          <Link
            className="block truncate font-mono text-xs hover:underline"
            title={accountText(event)}
            params={{ id: String(event.accountId) }}
            to="/admin/users/$id"
          >
            {accountText(event)}
          </Link>
        ),
    },
    {
      id: "factor",
      header: <Trans id="admin.logs.column.factor">Category</Trans>,
      cell: (event) =>
        isAuditFactor(event.factor) ? (
          auditFactorName(i18n, event.factor)
        ) : (
          <Raw>{event.factor}</Raw>
        ),
    },
    {
      id: "event",
      header: <Trans id="admin.logs.column.event">Event</Trans>,
      cell: (event) => eventLabel(i18n, event),
    },
    {
      id: "ip",
      header: <Trans id="admin.logs.column.ip">IP</Trans>,
      cell: (event) =>
        event.ip ? (
          <span className="font-mono text-xs">{event.ip}</span>
        ) : (
          <span className="text-muted-foreground">—</span>
        ),
    },
  ];
}

/**
 * What an event carries beyond its row: the browser that sent it, the address
 * and the details the writing handler recorded. Below `sm` the event and the
 * address are repeated first, since their columns are the ones scrolled away
 * and the event is what says whether the row is a failure; from `sm` up they
 * stay in their columns. Nothing is drawn for a missing item, and an event with
 * none of them has no detail to open.
 */
function eventDetail(i18n: Locale, event: AuditEventView): ReactNode | null {
  const hasDetail =
    event.detail !== undefined && Object.keys(event.detail).length > 0;
  if (!event.userAgent && !event.ip && !hasDetail) return null;
  return (
    <dl className="grid grid-cols-[max-content_minmax(0,1fr)] items-baseline gap-x-6 gap-y-2 py-1 text-sm max-sm:grid-cols-1 max-sm:gap-y-1">
      <dt className="text-muted-foreground sm:hidden">
        <Trans id="admin.logs.column.event">Event</Trans>
      </dt>
      <dd className="sm:hidden max-sm:mb-1">
        {isAuditFactor(event.factor)
          ? auditFactorName(i18n, event.factor)
          : event.factor}{" "}
        {eventLabel(i18n, event)}
      </dd>
      {event.userAgent && (
        <>
          <dt className="text-muted-foreground">
            <Trans id="admin.logs.detail.userAgent">User-Agent</Trans>
          </dt>
          <dd className="break-words max-sm:mb-1">{event.userAgent}</dd>
        </>
      )}
      {event.ip && (
        <>
          <dt className="text-muted-foreground sm:hidden">
            <Trans id="admin.logs.detail.ip">IP</Trans>
          </dt>
          <dd className="font-mono text-xs sm:hidden max-sm:mb-1">
            {event.ip}
          </dd>
        </>
      )}
      {hasDetail && (
        <>
          <dt className="text-muted-foreground">
            <Trans id="admin.logs.detail.detail">Details</Trans>
          </dt>
          <dd className="min-w-0">
            <JsonBlock json={JSON.stringify(event.detail)} />
          </dd>
        </>
      )}
    </dl>
  );
}
