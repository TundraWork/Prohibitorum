import { Card, Spinner } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { QueryErrorResetBoundary } from "@tanstack/react-query";
import { CatchBoundary } from "@tanstack/react-router";
import { type ReactNode, Suspense } from "react";
import { describeError } from "@/api/errors";
import { Button } from "@/components/custom/Button";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

/**
 * One titled block on a console page: the heading, whatever the heading row
 * carries on its trailing edge, and the block.
 *
 * The heading is drawn here rather than by a `Card`s own header for two
 * reasons. `Card.Title` renders an `h3` and takes no prop to change that, so a
 * card-owned heading would put the page's sections a level below the `h1` the
 * console header supplies. And the heading row carries an action, which
 * `Card.Header` — a plain `flex flex-col` — has no slot for.
 *
 * The heading sits on the page background, outside the card: a section that has
 * both a heading row and a list is one block, and giving it two surfaces would
 * make the list read as nested inside a second container.
 */
export function Section({
  title,
  action,
  children,
}: {
  title: ReactNode;
  /**
   * The block's primary action, on the trailing edge of the heading row. Most
   * sections have one; a section that is purely a list has none.
   */
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="flex flex-col gap-3">
      {/* The row is a button's height so a heading with an action and one
          without line up across sections: left to the text, the row would be
          half the height and its heading would sit higher than its
          neighbours'. */}
      <div className="flex min-h-10 items-center justify-between gap-4">
        <h2 className="text-sm font-medium text-foreground">{title}</h2>
        {action}
      </div>
      {children}
    </section>
  );
}

/**
 * Loading and failure for one section, so a slow or broken block says so in its
 * own place instead of replacing the page.
 *
 * Each section is its own boundary: the reads behind a block belong to that
 * block, and one that fails leaves the others readable. That is why a section's
 * data is fetched inside the section, not in a route loader.
 *
 * The boundary can draw the section's heading itself, from `title`, so the
 * heading is on screen from the first frame and stays there through the wait.
 * A fallback without it would leave the page headingless while the reads
 * resolve and then push everything below it down when they land, which is the
 * jump the heading exists to prevent.
 *
 * `title` is optional because not every boundary is a section. A tab panel
 * under `ConsoleTabs` already has its name on the strip, so it passes none and
 * the boundary draws the bare block instead.
 *
 * A retry clears the failed queries' error state before remounting the
 * children, so their suspense reads ask again.
 */
export function AsyncSection({
  resetKey,
  title,
  children,
}: {
  /** Identifies the boundary's content, so a change to it resets the error. */
  resetKey: string;
  /**
   * The section's heading, drawn whether or not the content has arrived.
   * Omitted where the boundary is not a section and something else already
   * names the block.
   */
  title?: ReactNode;
  children: ReactNode;
}) {
  const wrap = (content: ReactNode) =>
    title === undefined ? content : <Section title={title}>{content}</Section>;

  return (
    <QueryErrorResetBoundary>
      {({ reset: resetQueries }) => (
        <CatchBoundary
          getResetKey={() => resetKey}
          errorComponent={({ error, reset }) => (
            <SectionError
              title={title}
              error={error}
              onRetry={() => {
                resetQueries();
                reset();
              }}
            />
          )}
        >
          <Suspense fallback={wrap(<SectionPending />)}>{children}</Suspense>
        </CatchBoundary>
      )}
    </QueryErrorResetBoundary>
  );
}

/**
 * The block's own shape while its read is in flight: a card the height of a
 * short one, so the page settles where the content will land rather than
 * collapsing to nothing and growing again.
 */
function SectionPending() {
  return (
    <Card className="gap-0 p-0">
      <div className="flex items-center justify-center py-10">
        <Spinner size="md" />
      </div>
    </Card>
  );
}

function SectionError({
  title,
  error,
  onRetry,
}: {
  title: ReactNode;
  error: unknown;
  onRetry: () => void;
}) {
  const { t } = useLingui();
  return (
    <Section title={title}>
      <SurfaceAlert status="danger" role="alert">
        <SurfaceAlert.Indicator />
        <SurfaceAlert.Content>
          <SurfaceAlert.Title>
            <Trans id="section.failed">This section could not be loaded</Trans>
          </SurfaceAlert.Title>
          <SurfaceAlert.Description>
            {t(describeError(error))}
          </SurfaceAlert.Description>
          <Button
            className="mt-2 w-fit"
            size="sm"
            variant="danger"
            onPress={onRetry}
          >
            <Trans id="section.retry">Try again</Trans>
          </Button>
        </SurfaceAlert.Content>
      </SurfaceAlert>
    </Section>
  );
}
