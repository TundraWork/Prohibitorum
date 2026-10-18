import { useLingui } from "@lingui/react/macro";
import type { ColumnDef, RowData, SortingState } from "@tanstack/react-table";
import {
  createSortedRowModel,
  flexRender,
  rowSortingFeature,
  sortFn_alphanumeric,
  sortFn_basic,
  sortFn_datetime,
  sortFn_text,
  tableFeatures,
  useTable,
} from "@tanstack/react-table";
import { ChevronRight, ChevronsUpDown, MoveDown, MoveUp } from "lucide-react";
import {
  Fragment,
  type ReactNode,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
} from "react";
import { Button } from "@/components/custom/Button";
import { useSectionPanel } from "@/components/custom/Section";
import {
  TableActionCell,
  TableActionColumn,
} from "@/components/custom/TableActionCell";
import { Button as FlatButton } from "@/components/ui/button";
import { Frame, FrameHeader } from "@/components/ui/frame";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { cn } from "@/lib/utils";

/**
 * One feature set for the whole console: every table here sorts the same way,
 * and none of them filter, page or virtualise on the client — the server owns
 * all three (see `DataTable` below). Sorting is single-column: a header offers one order at a time.
 */
const features = tableFeatures({
  rowSortingFeature,
  sortFns: {
    alphanumeric: sortFn_alphanumeric,
    basic: sortFn_basic,
    datetime: sortFn_datetime,
    text: sortFn_text,
  },
  sortedRowModel: createSortedRowModel(),
});

export interface TableColumn<T> {
  /** Stable column identity, also the React key. */
  id: string;
  header: ReactNode;
  cell: (row: T) => ReactNode;
  /** Right-aligned cells suit timestamps and other short status values. */
  align?: "start" | "end";
  /**
   * Pin the column to the trailing edge of the scrolling table. Meant for a
   * single trailing action column: its cells stick to the right while the
   * other columns scroll under them, so the row's controls stay reachable on a
   * narrow screen. Rendered through `TableActionColumn` / `TableActionCell`.
   */
  pinned?: boolean;
  /**
   * The value this column sorts by, and with it the offer to sort at all. A
   * column without one is not sortable: an action column has nothing to order,
   * and neither does a column the server will not reorder.
   */
  sortValue?: (row: T) => string | number | boolean | null | undefined;
}

/**
 * The console's table: coss `Table` drawing what TanStack's column model
 * decides.
 *
 * TanStack owns the column definitions and the row model; the columns a page
 * writes are its own `{ id, header, cell }` shape, translated here, so a page
 * never has to hold a column helper. Outside a `Section` the table draws its
 * own `Frame`, with `toolbar` in the frame's header; inside one it is the
 * section's panel and draws no frame of its own.
 *
 * **The server remains the only source of filtering, sorting and paging.**
 * Sorting a column the server sorts is a matter of handing back `sorting` and
 * reissuing the query; `sortValue` is for a list that arrives whole, where
 * ordering the rows in hand is the whole job. No client-side filtering exists,
 * so a filter always means what the server matched, never "the rows already
 * loaded".
 *
 * `onLoadMore` puts a sentinel under the table that asks for the next cursor
 * page as it scrolls into view.
 *
 * `expandedRow` gives each row a detail it can open in place: the first column
 * carries a toggle, and the detail is a row of its own with one cell spanning
 * every column. A row whose detail is `null` has no toggle. Which rows are open
 * is the table's own state; it is not worth a place in the URL.
 */
export function DataTable<T extends RowData>({
  label,
  columns,
  rows,
  rowId,
  loading = false,
  empty,
  toolbar,
  sorting,
  onSortChange,
  hasMore = false,
  loadingMore = false,
  onLoadMore,
  expandedRow,
}: {
  label: string;
  columns: readonly TableColumn<T>[];
  rows: readonly T[];
  rowId: (row: T) => string | number;
  loading?: boolean;
  empty: ReactNode;
  /** The page's search and filters, drawn in the frame's header. */
  toolbar?: ReactNode;
  /** Controlled order, for a table the server sorts. */
  sorting?: SortingState;
  onSortChange?: (sorting: SortingState) => void;
  /** Whether a further cursor page exists behind `onLoadMore`. */
  hasMore?: boolean;
  /** A page is on its way; the sentinel shows progress rather than asking again. */
  loadingMore?: boolean;
  onLoadMore?: () => void;
  /** The detail a row opens to, or `null` for a row with nothing more to show. */
  expandedRow?: (row: T) => ReactNode | null;
}) {
  const { t } = useLingui();
  const baseId = useId();
  const inSection = useSectionPanel();
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(
    () => new Set(),
  );
  const definitions = useMemo<ColumnDef<typeof features, T, unknown>[]>(
    () =>
      columns.map((column) => {
        const sortValue = column.sortValue;
        return {
          id: column.id,
          header: () => column.header,
          cell: ({ row }) => column.cell(row.original),
          enableSorting: sortValue !== undefined,
          ...(sortValue === undefined
            ? {}
            : { accessorFn: (row: T) => sortValue(row) ?? "" }),
        } as ColumnDef<typeof features, T, unknown>;
      }),
    [columns],
  );

  // Uncontrolled unless the caller has an opinion: a list that arrives whole
  // sorts itself, and a server-sorted one hands its order down instead.
  const [ownSorting, setOwnSorting] = useState<SortingState>([]);
  const activeSorting = sorting ?? ownSorting;
  const changeSorting = (next: SortingState) => {
    if (onSortChange) onSortChange(next);
    else setOwnSorting(next);
  };
  // A column sorts ascending first, then flips: two states, as a header reads.
  const sortBy = (id: string) => {
    const current = activeSorting[0];
    changeSorting([{ id, desc: current?.id === id && !current.desc }]);
  };

  const table = useTable({
    columns: definitions,
    data: rows as T[],
    features,
    getRowId: (row) => String(rowId(row)),
    state: { sorting: activeSorting },
    onSortingChange: (updater) =>
      changeSorting(
        typeof updater === "function" ? updater(activeSorting) : updater,
      ),
  });

  // Cells never wrap: a wrapped timestamp or user agent makes rows different
  // heights and hides the column rhythm. The table scrolls sideways instead.
  const cellClass = (align: TableColumn<T>["align"]) =>
    align === "end" ? "text-end tabular-nums whitespace-nowrap" : undefined;

  const headers = table.getHeaderGroups()[0]?.headers ?? [];
  const byId = new Map(columns.map((column) => [column.id, column]));
  const treeColumn = expandedRow ? headers[0]?.id : undefined;
  const toggle = (id: string) =>
    setExpanded((open) => {
      const next = new Set(open);
      if (!next.delete(id)) next.add(id);
      return next;
    });

  const grid = (
    <Table
      variant="card"
      aria-label={label}
      aria-busy={loading || undefined}
      className="w-max min-w-full"
    >
      <TableHeader>
        <TableRow>
          {headers.map((header) => {
            const column = byId.get(header.id);
            const content = flexRender(
              header.column.columnDef.header,
              header.getContext(),
            );
            if (column?.pinned) {
              return (
                <TableActionColumn key={header.id}>{content}</TableActionColumn>
              );
            }
            const sortable = header.column.getCanSort();
            const direction =
              activeSorting[0]?.id === header.id
                ? activeSorting[0].desc
                  ? "descending"
                  : "ascending"
                : "none";
            const Icon =
              direction === "ascending"
                ? MoveUp
                : direction === "descending"
                  ? MoveDown
                  : ChevronsUpDown;
            return (
              <TableHead
                key={header.id}
                scope="col"
                aria-sort={sortable ? direction : undefined}
                className={cellClass(column?.align)}
              >
                {sortable ? (
                  <FlatButton
                    variant="ghost"
                    size="xs"
                    // Sortable headers keep the muted tone of plain ones; the
                    // ghost button's own hover, focus ring and disabled stay.
                    className={cn(
                      "text-muted-foreground hover:text-foreground",
                      column?.align === "end" ? "-me-2" : "-ms-2",
                    )}
                    onClick={() => sortBy(header.id)}
                  >
                    {content}
                    <Icon
                      aria-hidden="true"
                      className={direction === "none" ? "opacity-50" : ""}
                    />
                  </FlatButton>
                ) : (
                  content
                )}
              </TableHead>
            );
          })}
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.length === 0 && loading ? (
          // The rows have not arrived yet, so stand in for them with the rows
          // that are about to: one placeholder per cell, three rows deep. It
          // holds the table's own height, so the list does not jump when the
          // first page lands.
          Array.from({ length: 3 }).map((_, row) => (
            // Placeholders are positional and never reordered.
            // biome-ignore lint/suspicious/noArrayIndexKey: see above
            <TableRow key={row} aria-hidden="true">
              {columns.map((column) =>
                column.pinned ? (
                  <TableActionCell key={column.id}>
                    <Skeleton className="h-4 w-12" />
                  </TableActionCell>
                ) : (
                  <TableCell key={column.id}>
                    <Skeleton className="h-4 min-w-16" />
                  </TableCell>
                ),
              )}
            </TableRow>
          ))
        ) : rows.length === 0 ? (
          <TableRow>
            <TableCell colSpan={columns.length}>
              {/* Held at the leading edge and no wider than the frame, so the
                  message stays centred however wide the columns run. */}
              <div className="sticky start-2.5 w-[calc(100cqw-1.25rem)] whitespace-normal leading-normal">
                {empty}
              </div>
            </TableCell>
          </TableRow>
        ) : (
          table.getRowModel().rows.map((row) => {
            const detail = expandedRow ? expandedRow(row.original) : null;
            const open = detail !== null && expanded.has(row.id);
            const detailId = `${baseId}-${row.id}-detail`;
            const contentId = `${baseId}-${row.id}-cell`;
            const toggleId = `${baseId}-${row.id}-toggle`;
            return (
              <Fragment key={row.id}>
                <TableRow>
                  {row.getAllCells().map((cell) => {
                    const column = byId.get(cell.column.id);
                    const content = flexRender(
                      cell.column.columnDef.cell,
                      cell.getContext(),
                    );
                    if (column?.pinned) {
                      return (
                        <TableActionCell key={cell.id}>
                          {content}
                        </TableActionCell>
                      );
                    }
                    if (cell.column.id === treeColumn) {
                      return (
                        <TableCell
                          key={cell.id}
                          className={cellClass(column?.align)}
                        >
                          <span className="flex items-center gap-1">
                            {detail === null ? (
                              // Keeps the text of rows with and without a
                              // detail on one line.
                              <span
                                aria-hidden="true"
                                className="-ms-2 size-8 shrink-0 sm:size-7"
                              />
                            ) : (
                              <Button
                                id={toggleId}
                                aria-expanded={open}
                                aria-controls={open ? detailId : undefined}
                                aria-labelledby={`${toggleId} ${contentId}`}
                                aria-label={
                                  open
                                    ? t({
                                        id: "table.row.collapse",
                                        message: "Hide details",
                                      })
                                    : t({
                                        id: "table.row.expand",
                                        message: "Show details",
                                      })
                                }
                                onClick={() => toggle(row.id)}
                                variant="ghost"
                                size="icon-sm"
                                className="-ms-2"
                              >
                                <ChevronRight
                                  size={16}
                                  aria-hidden="true"
                                  className={`text-muted-foreground transition-transform duration-150 motion-reduce:transition-none ${
                                    open ? "rotate-90" : "rtl:rotate-180"
                                  }`}
                                />
                              </Button>
                            )}
                            <span id={contentId}>{content}</span>
                          </span>
                        </TableCell>
                      );
                    }
                    return (
                      <TableCell
                        key={cell.id}
                        className={cellClass(column?.align)}
                      >
                        {content}
                      </TableCell>
                    );
                  })}
                </TableRow>
                {open && (
                  <TableRow id={detailId}>
                    <TableCell
                      colSpan={columns.length}
                      className="whitespace-normal leading-normal"
                    >
                      {/* Held at the leading edge of the scrolling area and
                          no wider than it, so the detail stays readable
                          however far the columns are scrolled. */}
                      <div className="sticky start-2.5 w-[calc(100cqw-1.25rem)]">
                        {detail}
                      </div>
                    </TableCell>
                  </TableRow>
                )}
              </Fragment>
            );
          })
        )}
      </TableBody>
    </Table>
  );

  const more = onLoadMore !== undefined && hasMore && (
    <LoadMoreSentinel
      loading={loadingMore}
      onLoadMore={onLoadMore}
      rowCount={rows.length}
    />
  );
  // The container is also what a row's detail measures its width against, and
  // what the pinned column's shadow follows as it scrolls.
  const frameClass = "data-table @container min-w-0";
  if (inSection) {
    return (
      <div className={frameClass}>
        {toolbar !== undefined && <FrameHeader>{toolbar}</FrameHeader>}
        {grid}
        {more}
      </div>
    );
  }
  return (
    <Frame className={frameClass}>
      {toolbar !== undefined && <FrameHeader>{toolbar}</FrameHeader>}
      {grid}
      {more}
    </Frame>
  );
}

/**
 * Asks for the next cursor page when it scrolls into view.
 *
 * The observer is set up again only when the row count changes, so a page that
 * leaves the sentinel still on screen asks for the next one, while a failed
 * page (count unchanged) waits until the sentinel leaves and re-enters view.
 */
function LoadMoreSentinel({
  loading,
  onLoadMore,
  rowCount,
}: {
  loading: boolean;
  onLoadMore: () => void;
  rowCount: number;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const ask = useRef(onLoadMore);
  ask.current = onLoadMore;
  const busy = useRef(loading);
  busy.current = loading;
  // `rowCount` is not read: a new page re-arms the observer.
  // biome-ignore lint/correctness/useExhaustiveDependencies: see above
  useEffect(() => {
    const element = ref.current;
    if (!element || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(([entry]) => {
      if (entry?.isIntersecting && !busy.current) ask.current();
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, [rowCount]);
  return (
    <div ref={ref} className="flex min-h-8 justify-center py-2">
      {loading && <Spinner />}
    </div>
  );
}
