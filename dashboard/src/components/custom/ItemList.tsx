import { Card } from "@heroui/react";
import { ChevronRight } from "lucide-react";
import { Children, type ReactNode } from "react";
import { ListSkeleton } from "@/components/custom/ListSkeleton";

/**
 * The console's list: one HeroUI `Card` holding a row per record, with a rule
 * between rows. It is for the short lists a reader goes through one record at
 * a time — an account's passkeys, sessions, tokens — where column headers
 * would only repeat what each value already says. A directory an admin scans
 * down a column stays a `DataTable`.
 *
 * Each row names the record, shows a short line of detail under the name, and
 * keeps its actions at the trailing edge. The row wraps its detail line rather
 * than scrolling, so the list reads the same at every width.
 *
 * While `loading` holds, the card stands in with a spinner; an empty list shows
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
  return (
    <Card className="gap-0 p-0">
      {title !== undefined && (
        <Card.Header className="border-b border-separator px-4 py-3">
          <Card.Title render={(props) => <h2 {...props} />}>{title}</Card.Title>
        </Card.Header>
      )}
      {loading && rows.length === 0 ? (
        <ListSkeleton />
      ) : rows.length === 0 ? (
        empty
      ) : (
        <ul aria-label={label} className="flex flex-col">
          {rows.map((row, index) => (
            <li
              // `Children.toArray` has already keyed each row.
              key={(row as { key?: string }).key ?? index}
              className={index > 0 ? "border-t border-separator" : undefined}
            >
              {row}
            </li>
          ))}
        </ul>
      )}
      {footer !== undefined && (
        <div className="flex justify-center border-t border-separator p-3">
          {footer}
        </div>
      )}
    </Card>
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
 * A row with `onOpen` and no actions is a way into the record: the whole row
 * is pressable and ends with a chevron, so there is no separate "edit" button
 * saying the same thing.
 */
export function ItemListRow({
  icon,
  title,
  badges,
  details,
  actions,
  link,
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
        <span className="grid size-9 shrink-0 place-items-center rounded-[0.375rem] bg-default text-muted">
          {icon}
        </span>
      )}
      {/* The text keeps a floor, so a row whose actions carry a label moves
          them under the text on a narrow screen instead of squeezing it. */}
      <div className="flex min-w-40 flex-1 flex-col gap-0.5">
        <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
          <span className="min-w-0 wrap-anywhere text-sm font-medium text-foreground">
            {title}
          </span>
          {badges}
        </div>
        {shown.length > 0 && (
          // On a narrow screen the details stack one to a line: a wrapped
          // line would start with a stray separator.
          <p className="flex flex-wrap gap-x-1.5 text-xs text-muted max-sm:flex-col">
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
          className="shrink-0 text-muted"
        />
      )}
    </>
  );
  const rowClass = "flex flex-wrap items-center gap-3 px-4 py-3";
  return link !== undefined ? (
    link({
      className: `${rowClass} outline-none transition-colors hover:bg-default/40 focus-visible:bg-default/40`,
      children: body,
    })
  ) : (
    <div className={rowClass}>{body}</div>
  );
}
