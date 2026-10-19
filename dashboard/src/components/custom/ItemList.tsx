import { ChevronRight } from "lucide-react";
import { Children, type CSSProperties, type ReactNode } from "react";
import { ListSkeleton } from "@/components/custom/ListSkeleton";
import { useSectionPanel } from "@/components/custom/Section";
import {
  Frame,
  FrameHeader,
  FramePanel,
  FrameTitle,
} from "@/components/ui/frame";

type ListColumns = { actions?: boolean };

/** Whether a badge or meta prop is set: `null` and `false` count as absent. */
const given = (value: unknown) =>
  value !== undefined && value !== null && value !== false;

/**
 * The console's list: one panel holding a row per record, with a rule between
 * rows. Untitled under a `Section` it is the section's panel; otherwise it
 * draws its own frame, with `title` as the header. It is for the short lists a reader goes through one record at
 * a time — an account's passkeys, sessions, tokens — where column headers
 * would only repeat what each value already says. A directory an admin scans
 * down a column stays a `DataTable`.
 *
 * Each row names the record, shows a short line of detail under the name, and
 * keeps its actions at the trailing edge. The row wraps its detail line rather
 * than scrolling, so the list reads the same at every width.
 *
 * While `loading` holds, the panel stands in with skeleton rows; an empty list
 * shows `empty` in its place. The list is one CSS grid and each row a subgrid
 * of it, so every column (icon, badge, meta, actions) is as wide as its widest
 * cell and lines up down the list whatever the rows hold. `columns.actions`
 * keeps a floor under the actions column for a list whose rows may all be
 * without actions. `footer` sits under the last row, for an action over the
 * whole list.
 */
export function ItemList({
  label,
  title,
  loading = false,
  empty,
  footer,
  columns,
  children,
}: {
  /** Names the list for assistive technology. */
  label: string;
  /**
   * The list's own frame header, for a list that is one block among several
   * on a page. Leave it off for a list the console header already names.
   */
  title?: ReactNode;
  loading?: boolean;
  empty: ReactNode;
  footer?: ReactNode;
  /** Tracks to keep open whatever the rows hold. */
  columns?: ListColumns;
  children?: ReactNode;
}) {
  const rows = Children.toArray(children);
  const inSection = useSectionPanel(title === undefined);
  const panel = (
    <FramePanel className="divide-y overflow-hidden p-0">
      {loading && rows.length === 0 ? (
        <ListSkeleton />
      ) : rows.length === 0 ? (
        empty
      ) : (
        <ul
          aria-label={label}
          style={
            {
              "--item-actions": columns?.actions
                ? "minmax(5.5rem,auto)"
                : "auto",
            } as CSSProperties
          }
          className="grid grid-cols-[1rem_auto_minmax(0,1fr)_auto_auto_var(--item-actions)_auto_1rem] [&>li:not(:first-child)]:border-t"
        >
          {rows.map((row, index) => (
            <li
              // `Children.toArray` has already keyed each row.
              key={(row as { key?: string }).key ?? index}
              className="col-span-full grid grid-cols-subgrid"
            >
              {row}
            </li>
          ))}
        </ul>
      )}
      {footer !== undefined && (
        <div className="flex justify-center p-3">{footer}</div>
      )}
    </FramePanel>
  );
  if (inSection) return panel;
  return (
    <Frame>
      {title !== undefined && (
        <FrameHeader>
          <FrameTitle>
            <h2>{title}</h2>
          </FrameTitle>
        </FrameHeader>
      )}
      {panel}
    </Frame>
  );
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
 * `badge` and `meta` are the row's own columns from `sm`, the badge for a state
 * worth seeing at a glance and the meta for a short fact such as when it was
 * last used. They take the list's shared tracks, so they line up down the
 * list. Below `sm` the name is followed by its details and then by that line,
 * so a handle or slug stays with the name it identifies.
 *
 * A row that is a description with one labelled action — a setting, a danger
 * row — sets `stackActions`: below `sm` the action drops under the text,
 * left-aligned at its natural width, rather than squeezing the description
 * into the space beside it. A row of icon buttons or a menu keeps them beside
 * the name.
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
  badge,
  meta,
  details,
  actions,
  link,
  dimmed = false,
  stackActions = false,
  children,
}: {
  icon?: ReactNode;
  title: ReactNode;
  badges?: ReactNode;
  badge?: ReactNode;
  meta?: ReactNode;
  details?: readonly ReactNode[];
  actions?: ReactNode;
  /**
   * Render the row as a link to the record's own page. Given the row's
   * content and classes, it returns the link element (a router `Link`).
   */
  link?: (props: { className: string; children: ReactNode }) => ReactNode;
  dimmed?: boolean;
  /** Below `sm`, put `actions` under the text instead of beside it. */
  stackActions?: boolean;
  /**
   * Drawn under the details: a row's own error, or a note after an action.
   * The row's dialogs can sit here as well, since they render elsewhere.
   */
  children?: ReactNode;
}) {
  const hasBadge = given(badge);
  const hasMeta = given(meta);
  const shown = (details ?? []).filter(
    (detail) => detail !== null && detail !== undefined && detail !== false,
  );
  const hasExtras = hasBadge || hasMeta;
  const hasUnder = shown.length > 0 || given(children);
  const stacked = stackActions && actions !== undefined;
  // Below `sm` the name, the details and the badge and meta line are stacked
  // rows of the text column, in that order; the icon and actions span however
  // many exist, and stacked actions are a row of their own after them.
  const textRows = 1 + (hasUnder ? 1 : 0) + (hasExtras ? 1 : 0);
  const rowStyle = {
    "--item-span": textRows + (stacked ? 1 : 0),
    "--item-extras": hasUnder ? 3 : 2,
    "--item-actions-row": textRows + 1,
  } as CSSProperties;
  const body = (
    <>
      {icon !== undefined && (
        <span
          style={rowStyle}
          className="col-start-2 row-start-1 me-3 grid size-9 place-items-center rounded-md bg-muted text-muted-foreground max-sm:row-span-(--item-span)"
        >
          {icon}
        </span>
      )}
      {/* The name and details are one block from `sm` and stacked grid rows
          below it, so the badge and meta line can sit between them. */}
      <div
        data-slot="item-text"
        className="contents sm:col-start-3 sm:row-start-1 sm:flex sm:min-w-0 sm:flex-col sm:gap-0.5"
      >
        <div className="col-start-3 row-start-1 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
          <span
            data-dimmed={dimmed ? "true" : undefined}
            className="min-w-0 wrap-anywhere text-sm font-medium data-dimmed:text-muted-foreground"
          >
            {title}
          </span>
          {badges}
        </div>
        {hasUnder && (
          <div
            style={rowStyle}
            className="col-start-3 row-start-2 flex min-w-0 flex-col gap-0.5 max-sm:mt-0.5"
          >
            {shown.length > 0 && (
              // Each separator trails the detail before it, so a wrapped line
              // never starts with one; on a narrow screen the details stack
              // and drop them.
              <p
                data-slot="item-details"
                className="flex flex-wrap gap-x-1.5 text-muted-foreground text-xs max-sm:flex-col"
              >
                {shown.map((detail, index) => (
                  // Details are positional and never reordered.
                  // biome-ignore lint/suspicious/noArrayIndexKey: see above
                  <span key={index} className="wrap-anywhere">
                    {detail}
                    {index < shown.length - 1 && (
                      <span aria-hidden="true" className="ms-1.5 max-sm:hidden">
                        ·
                      </span>
                    )}
                  </span>
                ))}
              </p>
            )}
            {/* A row's dialogs live here too, and a closed dialog or an absent
                error renders nothing; `empty:hidden` drops the gap they would
                leave. */}
            <div className="mt-2 flex flex-col gap-2 empty:hidden">
              {children}
            </div>
          </div>
        )}
      </div>
      {hasExtras && (
        <div
          data-slot="item-extras"
          style={rowStyle}
          className="col-start-3 row-start-(--item-extras) mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 sm:contents max-sm:[&_[data-slot=badge]]:h-4.5 max-sm:[&_[data-slot=badge]]:text-xs"
        >
          {hasBadge && (
            <div
              data-slot="item-badge"
              className="flex items-center sm:col-start-4 sm:row-start-1 sm:ms-3 sm:justify-self-start"
            >
              {badge}
            </div>
          )}
          {hasMeta && (
            <span
              data-slot="item-meta"
              className="text-muted-foreground text-xs sm:col-start-5 sm:row-start-1 sm:ms-3 sm:whitespace-nowrap sm:text-sm"
            >
              {meta}
            </span>
          )}
        </div>
      )}
      {actions !== undefined && (
        <div
          data-slot="item-actions"
          data-stacked={stacked ? "true" : undefined}
          style={rowStyle}
          className={
            stacked
              ? "col-start-6 row-start-1 ms-3 flex items-center gap-1 justify-self-end max-sm:col-start-3 max-sm:row-start-(--item-actions-row) max-sm:ms-0 max-sm:mt-3 max-sm:justify-self-start"
              : "col-start-6 row-start-1 ms-3 flex items-center gap-1 justify-self-end max-sm:row-span-(--item-span)"
          }
        >
          {actions}
        </div>
      )}
      {link !== undefined && (
        <ChevronRight
          size={16}
          aria-hidden="true"
          style={rowStyle}
          className="col-start-7 row-start-1 ms-3 text-muted-foreground max-sm:row-span-(--item-span)"
        />
      )}
    </>
  );
  const rowClass = "col-span-full grid grid-cols-subgrid items-center py-3";
  return link !== undefined ? (
    link({
      className: `${rowClass} outline-none transition-colors hover:bg-accent/50 focus-visible:bg-accent/50`,
      children: body,
    })
  ) : (
    <div className={rowClass}>{body}</div>
  );
}
