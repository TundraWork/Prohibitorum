import { OverlayScrollbarsComponent } from "overlayscrollbars-react";
import type { ComponentProps, ReactNode } from "react";

/** The theme class `os-theme-app` is defined in `styles/index.css`. */
const theme = "os-theme-app";

/**
 * A scroll container whose scrollbars are drawn by OverlayScrollbars rather
 * than by the platform, so the console's scrollbars are the same width and
 * colour on every platform and follow the page's light/dark theme.
 *
 * OverlayScrollbars keeps the element it is given and wraps the content in a
 * generated viewport inside it. The element stays the layout box — so sizing
 * and position classes belong on it — but it stops being the scroll container:
 * the viewport inside it scrolls. Two consequences worth knowing before using
 * this:
 *
 * - `flex-direction: column` on the element is overridden to `row` by the
 *   library's host rule, so a column of children has to live in an inner
 *   wrapper rather than on the scrolled element itself.
 * - `scroll-timeline` has to be declared on the generated viewport, not on the
 *   element, or a scroll-driven animation reading it never becomes active.
 *   `styles/index.css` does this for `data-table-scroll`.
 *
 * The axis option (`overflow` -> `y`) has to be stated for a horizontal-only
 * container: OverlayScrollbars defaults both axes to `scroll`, which would give
 * a table a vertical bar it has no use for.
 */
export function ScrollArea({
  children,
  className,
  options,
  ...props
}: { children: ReactNode } & Omit<
  ComponentProps<typeof OverlayScrollbarsComponent>,
  "children"
>) {
  return (
    <OverlayScrollbarsComponent
      className={className}
      options={{
        ...options,
        // The app's theme is not a caller concern: it is what makes these
        // scrollbars match the ones HeroUI draws, so it is merged in here.
        scrollbars: { theme, ...options?.scrollbars },
      }}
      {...props}
    >
      {children}
    </OverlayScrollbarsComponent>
  );
}
