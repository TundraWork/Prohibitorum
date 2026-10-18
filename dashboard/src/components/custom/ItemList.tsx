import { ChevronRight } from "lucide-react";
import {
  Children,
  type CSSProperties,
  createContext,
  isValidElement,
  type ReactNode,
  useContext,
} from "react";
import { ListSkeleton } from "@/components/custom/ListSkeleton";
import { useSectionPanel } from "@/components/custom/Section";
import {
  Frame,
  FrameHeader,
  FramePanel,
  FrameTitle,
} from "@/components/ui/frame";
import { cn } from "@/lib/utils";

type ListColumns = { badge?: boolean; meta?: boolean };

/** Whether a badge or meta prop is set: `null` counts as absent. */
// `false` is absent too, so `badge={cond && <Badge />}` reserves no column.
const given = (value: unknown) =>
  value !== undefined && value !== null && value !== false;

const ListColumnsContext = createContext<ListColumns>({});

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
 * While `loading` holds, the panel stands in with skeleton rows; an empty list shows
 * `empty` in its place. When any row has a `badge` or `meta`, every row of the
 * list reserves that column, so the values line up down the list; `columns`
 * sets it explicitly for rows that are not direct `ItemListRow` children. `footer` sits under the last row, for an action over
 * the whole list.
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
  /** Columns every row reserves; detected from the rows' props when unset. */
  columns?: ListColumns;
  children?: ReactNode;
}) {
  const rows = Children.toArray(children);
  const has = (key: "badge" | "meta") =>
    rows.some(
      (row) =>
        isValidElement<ListColumns & Record<string, unknown>>(row) &&
        given(row.props[key]),
    );
  const reserved: ListColumns = {
    badge: columns?.badge ?? has("badge"),
    meta: columns?.meta ?? has("meta"),
  };
  const columnsAttr =
    [reserved.badge && "badge", reserved.meta && "meta"]
      .filter(Boolean)
      .join(" ") || undefined;
  const inSection = useSectionPanel(title === undefined);
  const panel = (
    <FramePanel className="divide-y overflow-hidden p-0">
      {loading && rows.length === 0 ? (
        <ListSkeleton />
      ) : rows.length === 0 ? (
        empty
      ) : (
        <ListColumnsContext.Provider value={reserved}>
          <ul
            aria-label={label}
            data-columns={columnsAttr}
            className="flex flex-col divide-y"
          >
            {rows.map((row, index) => (
              // `Children.toArray` has already keyed each row.
              <li key={(row as { key?: string }).key ?? index}>{row}</li>
            ))}
          </ul>
        </ListColumnsContext.Provider>
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
 * last used. They sit in fixed tracks (110px, 140px) shared by the whole list.
 * Below `sm` they join the details under the name, in the text column.
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
  /**
   * Drawn under the details: a row's own error, or a note after an action.
   * The row's dialogs can sit here as well, since they render elsewhere.
   */
  children?: ReactNode;
}) {
  const reserved = useContext(ListColumnsContext);
  const hasBadge = given(badge);
  const hasMeta = given(meta);
  const wantBadge = hasBadge || reserved.badge === true;
  const wantMeta = hasMeta || reserved.meta === true;
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
      <div
        className={cn(
          "flex min-w-40 flex-1 flex-col gap-0.5",
          // The badge and meta drop to their own line below `sm`, so the text
          // gives up some of its floor to keep the actions beside it.
          (hasBadge || hasMeta) && "max-sm:min-w-28",
        )}
      >
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
          // Each separator trails the detail before it, so a wrapped line never
          // starts with one; on a narrow screen the details stack and drop them.
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
            leave, which otherwise lifts the text off the actions' centre. */}
        <div className="mt-2 flex flex-col gap-2 empty:hidden">{children}</div>
      </div>
      {(hasBadge || hasMeta) && (
        <div
          data-slot="item-extras"
          className={cn(
            "max-sm:order-last max-sm:flex max-sm:basis-full max-sm:flex-wrap max-sm:items-center max-sm:gap-x-3 max-sm:gap-y-1 sm:contents",
            icon !== undefined && "max-sm:ps-12",
          )}
        >
          {wantBadge && (
            <div
              data-slot="item-badge"
              className="flex items-center max-sm:empty:hidden sm:justify-self-start"
            >
              {badge}
            </div>
          )}
          {wantMeta && (
            <span
              data-slot="item-meta"
              className="text-muted-foreground text-xs sm:whitespace-nowrap sm:text-sm"
            >
              {meta}
            </span>
          )}
        </div>
      )}
      {!(hasBadge || hasMeta) && (wantBadge || wantMeta) && (
        <>
          {wantBadge && (
            <div data-slot="item-badge" className="max-sm:hidden" />
          )}
          {wantMeta && <span data-slot="item-meta" className="max-sm:hidden" />}
        </>
      )}
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
  // From `sm` the grid has a track for each part the row has, plus the badge
  // and meta tracks the whole list reserves, so rows line up.
  const columns = [
    icon !== undefined && "auto",
    "minmax(0,1fr)",
    wantBadge && "110px",
    wantMeta && "140px",
    actions !== undefined && "auto",
    link !== undefined && "auto",
  ]
    .filter(Boolean)
    .join(" ");
  const rowStyle = { "--item-columns": columns } as CSSProperties;
  const content = (
    <div
      className="flex flex-wrap items-center gap-3 sm:grid sm:grid-cols-(--item-columns)"
      style={rowStyle}
    >
      {body}
    </div>
  );
  const rowClass = "block px-4 py-3";
  return link !== undefined ? (
    link({
      className: `${rowClass} outline-none transition-colors hover:bg-accent/50 focus-visible:bg-accent/50`,
      children: content,
    })
  ) : (
    <div className={rowClass}>{content}</div>
  );
}
