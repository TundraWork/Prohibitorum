import type { ReactNode } from "react";
import { TableCell, TableHead } from "@/components/ui/table";

/**
 * The pinned action cell shared by the console's tables.
 *
 * A table wider than its column scrolls sideways (see `DataTable`); this cell
 * sticks to the container's trailing edge, so the row actions stay reachable
 * however far the columns are scrolled. Its header twin is `TableActionColumn`,
 * and `DataTable` renders both for a column marked `pinned`.
 *
 * The card table paints every cell opaque and follows the row's hover itself,
 * so the columns sliding underneath never show through. A soft shadow on the
 * leading edge marks where the pinned column begins while there are columns
 * still to scroll under it (`table-pinned` in `styles/index.css`).
 *
 * `justify-end` keeps a lone button or a small group against the pinned edge;
 * icon buttons are laid out with a one-step gap.
 */
export function TableActionCell({ children }: { children: ReactNode }) {
  return (
    <TableCell
      data-pinned="end"
      className="table-pinned sticky end-0 z-[1] bg-clip-border"
    >
      <div className="flex items-center justify-end gap-1">{children}</div>
    </TableCell>
  );
}

/**
 * The header cell that pairs with `TableActionCell`.
 *
 * The frame's band is translucent, so the pinned header repaints it as a solid
 * mix over the page; otherwise the headers scrolling under it would show.
 */
export function TableActionColumn({ children }: { children: ReactNode }) {
  return (
    <TableHead
      scope="col"
      data-pinned="end"
      className="table-pinned sticky end-0 z-[1] bg-(--table-band) text-end"
    >
      {children}
    </TableHead>
  );
}
