import { Skeleton, Table } from "@heroui/react";
import type { ReactNode } from "react";

export interface TableColumn<T> {
  /** Stable column identity, also the React key. */
  id: string;
  header: ReactNode;
  cell: (row: T) => ReactNode;
  /** Right-aligned cells suit timestamps and other short status values. */
  align?: "start" | "end";
}

/**
 * Thin wrapper over the HeroUI `Table` for the console's account-owned lists:
 * one column definition per table, a shared empty state and a loading skeleton,
 * and horizontal scrolling on narrow screens instead of a second card layout.
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
            {columns.map((column) => (
              <Table.Column
                key={column.id}
                id={column.id}
                isRowHeader={columns[0]?.id === column.id}
                className={cellClass(column.align)}
              >
                {column.header}
              </Table.Column>
            ))}
          </Table.Header>
          <Table.Body renderEmptyState={() => (loading ? <Skeleton /> : empty)}>
            {rows.map((row) => (
              <Table.Row key={rowId(row)} id={rowId(row)}>
                {columns.map((column) => (
                  <Table.Cell
                    key={column.id}
                    className={cellClass(column.align)}
                  >
                    {column.cell(row)}
                  </Table.Cell>
                ))}
              </Table.Row>
            ))}
          </Table.Body>
        </Table.Content>
      </Table>
    </Table.ScrollContainer>
  );
}
