import { ChevronRight } from "lucide-react";
import { Children, type ReactNode } from "react";
import { ListSkeleton } from "@/components/custom/ListSkeleton";
import { useSectionPanel } from "@/components/custom/Section";
import { Frame, FramePanel } from "@/components/ui/frame";

/**
 * The console's list: one panel holding a row per record, with a rule between
 * rows. Under a `Section` it is the section's panel; alone it draws its own
 * frame. It is for the short lists a reader goes through one record at
 * a time — an account's passkeys, sessions, tokens — where column headers
 * would only repeat what each value already says. A directory an admin scans
 * down a column stays a `DataTable`.
 *
 * Each row names the record, shows a short line of detail under the name, and
 * keeps its actions at the trailing edge. The row wraps its detail line rather
 * than scrolling, so the list reads the same at every width.
 *
 * While `loading` holds, the panel stands in with skeleton rows; an empty list shows
 * `empty` in its place. `footer` sits under the last row, for an action over
 * the whole list.
 */
export function ItemList({
  label,
  title,
  loading = false,
  empty,
  footer,
  children,
}: {
  /** Names the list for assistive technology. */
  label: string;
  /**
   * A heading drawn in the card, for a list that is one block among several
   * on a page. A list that is the page's whole subject leaves it off: the
   * console header already names it.
   */
  title?: ReactNode;
  loading?: boolean;
  empty: ReactNode;
  footer?: ReactNode;
  children?: ReactNode;
}) {
  const rows = Children.toArray(children);
  const inSection = useSectionPanel();
  const panel = (
    <FramePanel className="divide-y overflow-hidden p-0">
      {title !== undefined && (
        <div className="px-4 py-3">
          <h2 className="font-semibold text-sm">{title}</h2>
        </div>
      )}
      {loading && rows.length === 0 ? (
        <ListSkeleton />
      ) : rows.length === 0 ? (
        empty
      ) : (
        <ul aria-label={label} className="flex flex-col divide-y">
          {rows.map((row, index) => (
            // `Children.toArray` has already keyed each row.
            <li key={(row as { key?: string }).key ?? index}>{row}</li>
          ))}
        </ul>
      )}
      {footer !== undefined && (
        <div className="flex justify-center p-3">{footer}</div>
      )}
    </FramePanel>
  );
  return inSection ? panel : <Frame>{panel}</Frame>;
}

/**
 * One record in an `ItemList`.
 *
 * `title` is the record's name and may carry a link; `badges` sit beside it
 * for states worth noticing at a glance (a role, "this device", "disabled").
 * `details` are the facts a table used to spread across columns, joined on one
 * muted line with a middle dot, so each value names itself rather than leaning
 * on a header. `actions` stay at the trailing edge at every width.
 *
 * A row with `onOpen` and no actions is a way into the record: the whole row
 * is pressable and ends with a chevron, so there is no separate "edit" button
 * saying the same thing.
 *
 * A `dimmed` row is a record kept but not in effect. Its name takes the muted
 * colour its details already use, and its actions stay at full strength. It
 * recedes by colour rather than by opacity: faded muted text falls well under
 * the contrast floor, and a kept record is still one the reader has to read.
 * The recession is decoration; whatever puts a row in that state says so in
 * words as well.
 */
export function ItemListRow({
  icon,
  title,
  badges,
  details,
  actions,
  link,
  dimmed = false,
  children,
}: {
  icon?: ReactNode;
  title: ReactNode;
  badges?: ReactNode;
  details?: readonly ReactNode[];
  actions?: ReactNode;
  /**
   * Render the row as a link to the record's own page. Given the row's
   * content and classes, it returns the link element (a router `Link`).
   */
  link?: (props: { className: string; children: ReactNode }) => ReactNode;
  dimmed?: boolean;
  /**
   * Drawn under the details: a row's own error, or a note after an action.
   * The row's dialogs can sit here as well, since they render elsewhere.
   */
  children?: ReactNode;
}) {
  const shown = (details ?? []).filter(
    (detail) => detail !== null && detail !== undefined && detail !== false,
  );
  const body = (
    <>
      {icon !== undefined && (
        <span className="grid size-9 shrink-0 place-items-center rounded-md bg-muted text-muted-foreground">
          {icon}
        </span>
      )}
      {/* The text keeps a floor, so a row whose actions carry a label moves
          them under the text on a narrow screen instead of squeezing it. */}
      <div className="flex min-w-40 flex-1 flex-col gap-0.5">
        <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
          <span
            data-dimmed={dimmed ? "true" : undefined}
            className="min-w-0 wrap-anywhere text-sm font-medium data-dimmed:text-muted-foreground"
          >
            {title}
          </span>
          {badges}
        </div>
        {shown.length > 0 && (
          // On a narrow screen the details stack one to a line: a wrapped
          // line would start with a stray separator.
          <p
            data-slot="item-details"
            className="flex flex-wrap gap-x-1.5 text-muted-foreground text-xs max-sm:flex-col"
          >
            {shown.map((detail, index) => (
              // Details are positional and never reordered.
              // biome-ignore lint/suspicious/noArrayIndexKey: see above
              <span key={index} className="wrap-anywhere">
                {index > 0 && (
                  <span aria-hidden="true" className="me-1.5 max-sm:hidden">
                    ·
                  </span>
                )}
                {detail}
              </span>
            ))}
          </p>
        )}
        {/* A row's dialogs live here too, and a closed dialog or an absent
            error renders nothing; `empty:hidden` drops the gap they would
            leave, which otherwise lifts the text off the actions' centre. */}
        <div className="mt-2 flex flex-col gap-2 empty:hidden">{children}</div>
      </div>
      {actions !== undefined && (
        <div className="ms-auto flex shrink-0 items-center gap-1">
          {actions}
        </div>
      )}
      {link !== undefined && (
        <ChevronRight
          size={16}
          aria-hidden="true"
          className="shrink-0 text-muted-foreground"
        />
      )}
    </>
  );
  const rowClass = "flex flex-wrap items-center gap-3 px-4 py-3";
  return link !== undefined ? (
    link({
      className: `${rowClass} outline-none transition-colors hover:bg-accent/50 focus-visible:bg-accent/50`,
      children: body,
    })
  ) : (
    <div className={rowClass}>{body}</div>
  );
}
