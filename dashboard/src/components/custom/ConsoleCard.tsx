import { Card } from "@heroui/react";
import type { ReactNode } from "react";

/**
 * The console's standard card: an optional heading naming the block, then the
 * block, capped at the console's content measure. Heading level and measure
 * live here rather than at each call site.
 *
 * A card under a `Section` passes no title: the section above it names the
 * block on the page background, and a heading here would sit a level below the
 * section's while saying the same thing. A card that stands on its own — a
 * dialog's content, or a page whose block has no section — names itself.
 *
 * ## Why the measure is a choice and not a constant
 *
 * The default suits a form read top to bottom: one value per line, and a line
 * short enough to scan without moving the head. Two kinds of block do not fit
 * that shape and are worse for being held to it — one that lays a record out in
 * columns the eye compares down the page (a table's worth of small fields), and
 * one whose rows have to line up with each other. Both are `wide`, which hands
 * the block the whole column the lists beside it already take, so a page does
 * not show two content widths side by side.
 */
export function ConsoleCard({
  title,
  children,
  wide = false,
  contentClassName,
}: {
  /** Omitted under a `Section`, which supplies the heading. */
  title?: ReactNode;
  /**
   * Let the block use the card's full measure instead of the reading column,
   * for content laid out in columns rather than read as lines.
   */
  wide?: boolean;
  /** Layout classes for the content column, such as its gap. */
  contentClassName?: string;
  children: ReactNode;
}) {
  const measure = wide ? "max-w-none" : "max-w-lg";
  const content = (
    <Card.Content
      className={contentClassName ? `${measure} ${contentClassName}` : measure}
    >
      {children}
    </Card.Content>
  );

  if (title === undefined) return <Card>{content}</Card>;

  return (
    <Card>
      <Card.Header>
        <Card.Title render={(props) => <h2 {...props} />}>{title}</Card.Title>
      </Card.Header>
      {content}
    </Card>
  );
}
