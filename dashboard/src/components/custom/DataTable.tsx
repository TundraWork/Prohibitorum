import type { SortDescriptor } from "@heroui/react";
import { Spinner, Table } from "@heroui/react";
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
import { type ReactNode, useMemo, useState } from "react";
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
}) {
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

  return (
    <Table.ScrollContainer
      // The table keeps to the console column like every other block. Cells
      // never wrap, so a table wider than its column scrolls sideways instead
      // of clipping.
      className="min-w-0 overflow-x-auto"
    >
      <Table aria-label={label} className="w-max min-w-full">
        <Table.Content
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
            {table.getRowModel().rows.map((row) => (
              <Table.Row key={row.id} id={row.id}>
                {row.getAllCells().map((cell) => {
                  const column = byId.get(cell.column.id);
                  const content = flexRender(
                    cell.column.columnDef.cell,
                    cell.getContext(),
                  );
                  return column?.pinned ? (
                    <TableActionCell key={cell.id}>{content}</TableActionCell>
                  ) : (
                    <Table.Cell
                      key={cell.id}
                      className={cellClass(column?.align)}
                    >
                      {content}
                    </Table.Cell>
                  );
                })}
              </Table.Row>
            ))}
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
