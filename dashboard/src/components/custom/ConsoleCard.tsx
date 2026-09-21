import { Card } from "@heroui/react";
import type { ReactNode } from "react";

/**
 * The console's standard card: a heading naming the block, then the block,
 * capped at the console's content measure. Heading level and measure live here
 * rather than at each call site.
 */
export function ConsoleCard({
  title,
  children,
  contentClassName,
}: {
  title: ReactNode;
  /** Layout classes for the content column, such as its gap. */
  contentClassName?: string;
  children: ReactNode;
}) {
  return (
    <Card>
      <Card.Header>
        <Card.Title render={(props) => <h2 {...props} />}>{title}</Card.Title>
      </Card.Header>
      <Card.Content
        className={
          contentClassName ? `max-w-lg ${contentClassName}` : "max-w-lg"
        }
      >
        {children}
      </Card.Content>
    </Card>
  );
}
