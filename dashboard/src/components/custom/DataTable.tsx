import type { Selection, SortDescriptor } from "@heroui/react";
import { Spinner, Table } from "@heroui/react";
import { useLingui } from "@lingui/react/macro";
import type {
  ColumnDef,
  RowData,
  SortingState,
  Updater,
} from "@tanstack/react-table";
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
import { ChevronRight } from "lucide-react";
import { type ReactNode, useMemo, useState } from "react";
import { Button } from "@/components/custom/Button";
import {
  TableActionCell,
  TableActionColumn,
} from "@/components/custom/TableActionCell";

/**
 * One feature set for the whole console: every table here sorts the same way,
 * and none of them filter, page or virtualise on the client — the server owns
 * all three (see `DataTable` below). Sorting is single-column because React
 * Aria's `SortDescriptor`, and therefore `Table.Content`, expresses one column.
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

/** React Aria and TanStack name the same two directions differently. */
function toSortDescriptor(sorting: SortingState): SortDescriptor | undefined {
  const first = sorting[0];
  if (!first) return undefined;
  return {
    column: first.id,
    direction: first.desc ? "descending" : "ascending",
  };
}

function toSortingState(descriptor: SortDescriptor): SortingState {
  return [
    {
      desc: descriptor.direction === "descending",
      id: descriptor.column as string,
    },
  ];
}

/**
 * The console's table: HeroUI's `Table` drawing what TanStack's column model
 * decides.
 *
 * HeroUI supplies the whole anatomy — `Table.Content`, `Table.Header`,
 * `Table.Column`, `Table.SortableColumnHeader`, `Table.Body`, `Table.Row`,
 * `Table.Cell`, `Table.LoadMore` — so nothing here reimplements a capability
 * the library already has. TanStack owns the column definitions and the row
 * model; the columns a page writes are its own `{ id, header, cell }` shape,
 * translated here, so a page never has to hold a column helper.
 *
 * **The server remains the only source of filtering, sorting and paging.**
 * Sorting a column the server sorts is a matter of handing back `sorting` and
 * reissuing the query; `sortValue` is for a list that arrives whole, where
 * ordering the rows in hand is the whole job. No client-side filtering exists,
 * so a filter always means what the server matched, never "the rows already
 * loaded".
 *
 * `onLoadMore` turns the foot of the table into a sentinel row that asks for
 * the next cursor page as it scrolls into view.
 *
 * `expandedRow` gives each row a detail it can open in place, through HeroUI's
 * expandable rows: the first column becomes the tree column and carries the
 * toggle, and the detail is a child row with one cell spanning every column.
 * A row whose detail is `null` has no toggle. Which rows are open is the
 * table's own state; it is not worth a place in the URL.
 */
export function DataTable<T extends RowData>({
  label,
  columns,
  rows,
  rowId,
  loading = false,
  empty,
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
  /** Controlled order, for a table the server sorts. */
  sorting?: SortingState;
  onSortChange?: (sorting: SortingState) => void;
  /** Whether a further cursor page exists behind `onLoadMore`. */
  hasMore?: boolean;
  /** A page is on its way; the sentinel row shows progress rather than asking again. */
  loadingMore?: boolean;
  onLoadMore?: () => void;
  /** The detail a row opens to, or `null` for a row with nothing more to show. */
  expandedRow?: (row: T) => ReactNode | null;
}) {
  const { t } = useLingui();
  const [expandedKeys, setExpandedKeys] = useState<Selection>(() => new Set());
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
  const handleSortingChange = (updater: Updater<SortingState>) => {
    const next =
      typeof updater === "function" ? updater(activeSorting) : updater;
    if (onSortChange) onSortChange(next);
    else setOwnSorting(next);
  };

  const table = useTable({
    columns: definitions,
    data: rows as T[],
    features,
    getRowId: (row) => String(rowId(row)),
    state: { sorting: activeSorting },
    onSortingChange: handleSortingChange,
  });

  // Cells never wrap: a wrapped timestamp or user agent makes rows different
  // heights and hides the column rhythm. The table scrolls sideways instead.
  const cellClass = (align: TableColumn<T>["align"]) =>
    align === "end"
      ? "text-end tabular-nums whitespace-nowrap"
      : "whitespace-nowrap";

  const headers = table.getHeaderGroups()[0]?.headers ?? [];
  const byId = new Map(columns.map((column) => [column.id, column]));
  const treeColumn = expandedRow ? headers[0]?.id : undefined;

  return (
    <Table.ScrollContainer
      // The table keeps to the console column like every other block. Cells
      // never wrap, so a table wider than its column scrolls sideways instead
      // of clipping. The container is also what a row's detail measures its
      // width against, and the scroll timeline the pinned column's shadow
      // follows (`data-table-scroll` in `styles/index.css`).
      className="data-table-scroll @container min-w-0 overflow-x-auto"
    >
      <Table aria-label={label} className="w-max min-w-full">
        <Table.Content
          {...(treeColumn === undefined
            ? {}
            : {
                treeColumn,
                expandedKeys,
                onExpandedChange: setExpandedKeys,
              })}
          sortDescriptor={toSortDescriptor(activeSorting)}
          onSortChange={(descriptor) =>
            table.setSorting(toSortingState(descriptor))
          }
        >
          <Table.Header>
            {headers.map((header) => {
              const column = byId.get(header.id);
              const content = flexRender(
                header.column.columnDef.header,
                header.getContext(),
              );
              if (column?.pinned) {
                return (
                  <TableActionColumn key={header.id} id={header.id}>
                    {content}
                  </TableActionColumn>
                );
              }
              const sortable = header.column.getCanSort();
              return (
                <Table.Column
                  key={header.id}
                  id={header.id}
                  isRowHeader={headers[0]?.id === header.id}
                  allowsSorting={sortable}
                  className={cellClass(column?.align)}
                >
                  {sortable
                    ? ({ sortDirection }) => (
                        <Table.SortableColumnHeader
                          sortDirection={sortDirection}
                        >
                          {content}
                        </Table.SortableColumnHeader>
                      )
                    : content}
                </Table.Column>
              );
            })}
          </Table.Header>
          <Table.Body
            renderEmptyState={() =>
              loading ? (
                // The rows have not arrived yet, so stand in for them with one
                // full-width row on the console's gray surface, which reads as
                // chrome rather than data.
                <div className="flex items-center justify-center bg-surface-secondary py-6">
                  <Spinner size="md" />
                </div>
              ) : (
                empty
              )
            }
          >
            {table.getRowModel().rows.map((row) => {
              const detail = expandedRow ? expandedRow(row.original) : null;
              return (
                <Table.Row key={row.id} id={row.id}>
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
                        <Table.Cell
                          key={cell.id}
                          className={cellClass(column?.align)}
                        >
                          {({ hasChildItems, isExpanded, isTreeColumn }) => (
                            <span className="flex items-center gap-1">
                              {hasChildItems && isTreeColumn && (
                                <Button
                                  isIconOnly
                                  size="sm"
                                  slot="chevron"
                                  variant="ghost"
                                  className="-ms-2"
                                  aria-label={
                                    isExpanded
                                      ? t({
                                          id: "table.row.collapse",
                                          message: "Hide details",
                                        })
                                      : t({
                                          id: "table.row.expand",
                                          message: "Show details",
                                        })
                                  }
                                >
                                  <ChevronRight
                                    size={16}
                                    aria-hidden="true"
                                    className={`text-muted transition-transform duration-150 motion-reduce:transition-none ${
                                      isExpanded
                                        ? "rotate-90"
                                        : "rtl:rotate-180"
                                    }`}
                                  />
                                </Button>
                              )}
                              {content}
                            </span>
                          )}
                        </Table.Cell>
                      );
                    }
                    return (
                      <Table.Cell
                        key={cell.id}
                        className={cellClass(column?.align)}
                      >
                        {content}
                      </Table.Cell>
                    );
                  })}
                  {detail !== null && (
                    <Table.Row id={`${row.id}:detail`}>
                      <Table.Cell
                        colSpan={columns.length}
                        className="whitespace-normal"
                      >
                        {/* Held at the leading edge of the scrolling area and
                            no wider than it, so the detail stays readable
                            however far the columns are scrolled. */}
                        <div className="sticky start-4 max-w-[calc(100cqw-2rem)]">
                          {detail}
                        </div>
                      </Table.Cell>
                    </Table.Row>
                  )}
                </Table.Row>
              );
            })}
            {onLoadMore !== undefined && hasMore && (
              <Table.LoadMore
                isLoading={loadingMore}
                scrollOffset={0}
                onLoadMore={onLoadMore}
              >
                <Table.LoadMoreContent>
                  <Spinner size="md" />
                </Table.LoadMoreContent>
              </Table.LoadMore>
            )}
          </Table.Body>
        </Table.Content>
      </Table>
    </Table.ScrollContainer>
  );
}
