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
 */
export function ConsoleCard({
  title,
  children,
  contentClassName,
}: {
  /** Omitted under a `Section`, which supplies the heading. */
  title?: ReactNode;
  /** Layout classes for the content column, such as its gap. */
  contentClassName?: string;
  children: ReactNode;
}) {
  const content = (
    <Card.Content
      className={contentClassName ? `max-w-lg ${contentClassName}` : "max-w-lg"}
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
