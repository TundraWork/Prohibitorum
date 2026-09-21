import { Spinner, Table } from "@heroui/react";
import type { ReactNode } from "react";
import {
  TableActionCell,
  TableActionColumn,
} from "@/components/custom/TableActionCell";

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
}

/**
 * Thin wrapper over the HeroUI `Table` for the console's account-owned lists:
 * one column definition per table, a shared empty state and a loading row,
 * and horizontal scrolling on narrow screens instead of a second card layout.
 * A trailing column can be marked `pinned` to stay stuck to the right while the
 * rest scrolls (see `TableActionCell`).
 *
 * No sorting, paging or virtualisation: every list here is the signed-in
 * account's own handful of rows.
 */
export function DataTable<T>({
  label,
  columns,
  rows,
  rowId,
  loading = false,
  empty,
}: {
  label: string;
  columns: readonly TableColumn<T>[];
  rows: readonly T[];
  rowId: (row: T) => string | number;
  loading?: boolean;
  empty: ReactNode;
}) {
  // Cells never wrap: a wrapped timestamp or user agent makes rows different
  // heights and hides the column rhythm. The table scrolls sideways instead.
  const cellClass = (align: TableColumn<T>["align"]) =>
    align === "end"
      ? "text-end tabular-nums whitespace-nowrap"
      : "whitespace-nowrap";
  return (
    <Table.ScrollContainer
      // Past the console column's measure there is room to spare, so a wide
      // table takes the console's own width and gives it back to both gutters
      // instead of leaving them empty (`cqw` is that column's container; see
      // `main` in ConsoleLayout). Cells never wrap, so a table wider than its
      // column scrolls sideways instead of clipping.
      className="min-w-0 overflow-x-auto min-[1440px]:w-[100cqw] min-[1440px]:mx-[calc(50%_-_50cqw)]"
    >
      <Table aria-label={label} className="w-max min-w-full">
        <Table.Content>
          <Table.Header>
            {columns.map((column) =>
              column.pinned ? (
                <TableActionColumn key={column.id} id={column.id}>
                  {column.header}
                </TableActionColumn>
              ) : (
                <Table.Column
                  key={column.id}
                  id={column.id}
                  isRowHeader={columns[0]?.id === column.id}
                  className={cellClass(column.align)}
                >
                  {column.header}
                </Table.Column>
              ),
            )}
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
            {rows.map((row) => (
              <Table.Row key={rowId(row)} id={rowId(row)}>
                {columns.map((column) =>
                  column.pinned ? (
                    <TableActionCell key={column.id}>
                      {column.cell(row)}
                    </TableActionCell>
                  ) : (
                    <Table.Cell
                      key={column.id}
                      className={cellClass(column.align)}
                    >
                      {column.cell(row)}
                    </Table.Cell>
                  ),
                )}
              </Table.Row>
            ))}
          </Table.Body>
        </Table.Content>
      </Table>
    </Table.ScrollContainer>
  );
}
