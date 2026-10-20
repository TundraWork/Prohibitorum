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
 * so the columns sliding underneath never show through the cell. Its borders
 * would, being translucent, so `styles/index.css` makes the dividers opaque in
 * the colour they have over the frame, and repaints the outer corners of the
 * first and last rows, which are transparent outside the curve. A soft shadow
 * on the leading edge marks where the pinned column begins while columns remain
 * to scroll under it (`table-pinned`); the header cell has none, so the header
 * band stays whole.
 *
 * `justify-end` keeps a lone button or a small group against the pinned edge;
 * icon buttons are laid out with a one-step gap.
 */
export function TableActionCell({ children }: { children: ReactNode }) {
  return (
    <TableCell
      data-pinned="end"
      className="table-pinned sticky end-0 z-[1] w-px"
    >
      <div className="flex items-center justify-end gap-1">{children}</div>
    </TableCell>
  );
}

/**
 * The header cell that pairs with `TableActionCell`.
 *
 * The frame's band is translucent, so `data-pinned` headers repaint it over the
 * page colour (`styles/index.css`); otherwise headers scrolling under it show.
 * The cell takes the frame's inner corner, so the repaint does not poke out of
 * the rounded frame.
 */
export function TableActionColumn({ children }: { children: ReactNode }) {
  return (
    <TableHead
      scope="col"
      data-pinned="end"
      className="sticky end-0 z-[1] w-px rounded-se-2xl text-end"
    >
      {children}
    </TableHead>
  );
}
