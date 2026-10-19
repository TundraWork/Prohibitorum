import { Trans, useLingui } from "@lingui/react/macro";
import { QueryErrorResetBoundary } from "@tanstack/react-query";
import { CatchBoundary } from "@tanstack/react-router";
import {
  createContext,
  isValidElement,
  type ReactNode,
  Suspense,
  useCallback,
  useContext,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { describeError } from "@/api/errors";
import { Button } from "@/components/custom/Button";
import { useRegisterSection } from "@/components/custom/SectionRegistry";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import {
  Frame,
  FrameDescription,
  FrameFooter,
  FrameHeader,
  FramePanel,
  FrameTitle,
} from "@/components/ui/frame";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

const slug = (text: string): string | undefined =>
  text
    .toLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, "-")
    .replace(/^-+|-+$/g, "") || undefined;

/** A text title, or the message id of a `<Trans id>` title (stable across locales). */
const slugOf = (title: ReactNode): string | undefined => {
  if (typeof title === "string") return slug(title);
  if (isValidElement<{ id?: unknown }>(title)) {
    const id = title.props.id;
    return typeof id === "string" ? slug(id) : undefined;
  }
  return undefined;
};

/**
 * Lets a panel inside a `Section` say it is the section's panel. Absent
 * outside a section, where the same components draw their own frame.
 */
const SectionFrameContext = createContext<(() => () => void) | null>(null);

/**
 * Whether the caller sits in a `Section` and is its panel.
 *
 * A `ConsoleCard` or `ItemList` that reads `true` renders a bare `FramePanel`
 * and the section supplies the frame. Pass `enabled: false` for one that draws
 * its own frame, so it does not take the section's panel.
 */
export function useSectionPanel(enabled = true): boolean {
  const register = useContext(SectionFrameContext);
  useLayoutEffect(
    () => (enabled ? register?.() : undefined),
    [register, enabled],
  );
  return register !== null && enabled;
}

/**
 * One titled block on a console page, drawn as a coss `Frame`: a muted band
 * carrying the title, an optional description and the block's primary action,
 * the content on a white panel, and an optional footer band.
 *
 * The title is an `h2`, a level under the console header's `h1`. A
 * `ConsoleCard` or `ItemList` in the content is the panel itself; anything else
 * is put on one panel here, so a section never holds a card inside a card.
 */
export function Section({
  id,
  title,
  icon,
  description,
  action,
  footer,
  tone = "default",
  dirty = false,
  children,
}: {
  /** Anchor for a link to the block; defaults to a slug of a text `title`. */
  id?: string;
  title: ReactNode;
  /** Drawn before the title text, muted. */
  icon?: ReactNode;
  description?: ReactNode;
  /** The block's primary action, on the trailing edge of the header. */
  action?: ReactNode;
  /** Rendered on the muted band under the panel. */
  footer?: ReactNode;
  /** `danger` tints the frame for destructive actions. */
  tone?: "default" | "danger";
  /** Marks unsaved edits; shown by the outline rail and the unsaved bar. */
  dirty?: boolean;
  children: ReactNode;
}) {
  const [panels, setPanels] = useState(0);
  const register = useCallback(() => {
    setPanels((count) => count + 1);
    return () => setPanels((count) => count - 1);
  }, []);
  const ownPanel = panels === 0;
  const sectionId = id ?? slugOf(title);
  const heading = useRef<HTMLHeadingElement>(null);
  // The rail names the section by its rendered heading, whatever `title` is.
  const [headingText, setHeadingText] = useState("");
  useLayoutEffect(() => {
    setHeadingText(heading.current?.textContent?.trim() ?? "");
  });
  useRegisterSection({
    id: sectionId ?? "",
    title: sectionId === undefined ? "" : headingText,
    dirty,
    tone: tone === "danger" ? "danger" : undefined,
  });

  return (
    <SectionFrameContext.Provider value={register}>
      <section
        id={sectionId}
        data-dirty={dirty ? "true" : undefined}
        // Clear of the sticky console header when a link scrolls here.
        className="scroll-mt-[calc(var(--app-sticky-offset)+5rem)]"
      >
        <Frame
          data-tone={tone === "danger" ? "danger" : undefined}
          className={tone === "danger" ? "bg-destructive/6" : undefined}
        >
          <FrameHeader className="min-h-9 gap-3 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
            <div className="flex min-w-0 flex-col gap-1">
              <FrameTitle
                className={cn(
                  "flex items-center gap-2",
                  tone === "danger" && "text-destructive-foreground",
                )}
              >
                {icon !== undefined && (
                  <span
                    data-slot="section-icon"
                    aria-hidden="true"
                    className="text-muted-foreground *:size-4"
                  >
                    {icon}
                  </span>
                )}
                {/* Focusable so "Jump to it" can land on the heading. */}
                <h2 ref={heading} tabIndex={-1} className="outline-none">
                  {title}
                </h2>
              </FrameTitle>
              {description !== undefined && (
                <FrameDescription className="text-pretty">
                  {description}
                </FrameDescription>
              )}
            </div>
            {action}
          </FrameHeader>
          {/* One element type in both states, so a child that claims the panel
              is restyled rather than remounted. */}
          <FramePanel
            {...(ownPanel
              ? { className: "flex flex-col gap-3" }
              : {
                  "data-slot": "section-body",
                  className:
                    "flex flex-col rounded-none border-0 bg-transparent p-0 shadow-none before:hidden",
                })}
          >
            {children}
          </FramePanel>
          {footer !== undefined && (
            <FrameFooter className="text-muted-foreground text-sm">
              {footer}
            </FrameFooter>
          )}
        </Frame>
      </section>
    </SectionFrameContext.Provider>
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
  icon,
  description,
  id,
  footer,
  dirty,
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
  /** The section's icon and description, drawn with the heading in every state so it does not change when the content lands. */
  icon?: ReactNode;
  description?: ReactNode;
  /** Passed to the `Section` drawn in every state. */
  id?: string;
  footer?: ReactNode;
  dirty?: boolean;
  children: ReactNode;
}) {
  const sectionProps = { id, icon, description, footer, dirty };
  const pending =
    title === undefined ? (
      <Frame>
        <FramePanel>
          <SectionPending />
        </FramePanel>
      </Frame>
    ) : (
      <Section title={title} {...sectionProps}>
        <SectionPending />
      </Section>
    );

  return (
    <QueryErrorResetBoundary>
      {({ reset: resetQueries }) => (
        <CatchBoundary
          getResetKey={() => resetKey}
          errorComponent={({ error, reset }) => (
            <SectionError
              title={title}
              sectionProps={sectionProps}
              error={error}
              onRetry={() => {
                resetQueries();
                reset();
              }}
            />
          )}
        >
          <Suspense fallback={pending}>{children}</Suspense>
        </CatchBoundary>
      )}
    </QueryErrorResetBoundary>
  );
}

/**
 * The block's own shape while its read is in flight: rows about to arrive
 * rather than a spinner, so the page settles where the content will land.
 *
 * The bars are `aria-hidden`: placeholders have nothing to announce.
 */
function SectionPending() {
  return (
    <div aria-hidden="true" className="flex flex-col gap-3">
      <Skeleton className="h-4 w-2/5" />
      <Skeleton className="h-4 w-full" />
      <Skeleton className="h-4 w-4/5" />
    </div>
  );
}

function SectionError({
  title,
  sectionProps,
  error,
  onRetry,
}: {
  title: ReactNode;
  sectionProps: {
    id?: string;
    icon?: ReactNode;
    description?: ReactNode;
    footer?: ReactNode;
    dirty?: boolean;
  };
  error: unknown;
  onRetry: () => void;
}) {
  const alert = <SectionFailure error={error} onRetry={onRetry} />;
  if (title === undefined) {
    return (
      <Frame>
        <FramePanel>{alert}</FramePanel>
      </Frame>
    );
  }
  return (
    <Section title={title} {...sectionProps}>
      {alert}
    </Section>
  );
}

/** What a block that could not be read says, with the way to ask again. */
export function SectionFailure({
  error,
  onRetry,
}: {
  error: unknown;
  onRetry: () => void;
}) {
  const { t } = useLingui();
  return (
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
          onClick={onRetry}
          variant="destructive"
          size="sm"
          className="mt-2 w-fit"
        >
          <Trans id="section.retry">Try again</Trans>
        </Button>
      </SurfaceAlert.Content>
    </SurfaceAlert>
  );
}
