import { Table } from "@heroui/react";
import type { ReactNode } from "react";

/**
 * The pinned action cell shared by the console's tables.
 *
 * A table wider than its column scrolls sideways inside `Table.ScrollContainer`
 * (see `DataTable`); this cell sticks to the container's trailing edge, so the
 * row actions stay reachable however far the columns are scrolled. Its header
 * twin is `TableActionColumn`, and `DataTable` renders both for a column marked
 * `pinned`.
 *
 * The cell keeps an opaque background (`table-action-cell` in `styles/index.css`).
 * HeroUI tints a hovered row's cells with the translucent `bg-surface/40`;
 * applied here it would both read as no change and let the columns sliding
 * underneath show through, so the same result is painted as a solid mix of the
 * surface and the body's own backdrop. A soft shadow on the leading edge marks
 * where the pinned column begins.
 *
 * `justify-end` keeps a lone button or a small group against the pinned edge;
 * icon buttons are laid out with a one-step gap.
 */
export function TableActionCell({ children }: { children: ReactNode }) {
  return (
    <Table.Cell className="table-action-cell sticky end-0 z-[1] bg-surface shadow-[-8px_0_8px_-8px_rgb(0_0_0/0.15)]">
      <div className="flex items-center justify-end gap-1">{children}</div>
    </Table.Cell>
  );
}

/** The header cell that pairs with `TableActionCell`. */
export function TableActionColumn({
  id,
  children,
}: {
  id: string;
  children: ReactNode;
}) {
  return (
    <Table.Column
      id={id}
      className="sticky end-0 z-[1] bg-surface-secondary text-end shadow-[-8px_0_8px_-8px_rgb(0_0_0/0.15)]"
    >
      {children}
    </Table.Column>
  );
}
