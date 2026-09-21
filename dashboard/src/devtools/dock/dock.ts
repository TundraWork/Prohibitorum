/**
 * Keeps the open devtools panel from covering the console.
 *
 * The shell docks its panel to an edge of the viewport as a fixed overlay, so an
 * open panel hides whatever the page renders underneath. This watch measures the
 * open panel and narrows the page to the space that is left, the way a browser's
 * own docked devtools behave: the full-height regions end at the panel, and the
 * page keeps a matching gutter so nothing scrolls permanently out of reach.
 *
 * The shell hands a plugin `devtoolsOpen` only while that plugin's own pane is
 * open, so the panel is watched through the DOM instead: the shell marks it with
 * `data-testid="tanstack-devtools-panel"` and `data-open`, and gives it the
 * height it is asking for.
 *
 * Detaching the panel opens it in a separate picture-in-picture document, where
 * it covers nothing; no panel is found in this document then, so the page keeps
 * its full size.
 */

const panelSelector = '[data-testid="tanstack-devtools-panel"]';

const viewportHeightProperty = "--app-viewport-height";
const stickyOffsetProperty = "--app-sticky-offset";
const gutterTopProperty = "--app-gutter-top";
const gutterBottomProperty = "--app-gutter-bottom";

/** How much of the viewport an open panel takes, and from which edge. */
export interface Dock {
  location: "top" | "bottom";
  height: number;
}

/**
 * The dock an open panel of this box implies, or `null` for a panel that takes
 * no room — closed, or not laid out yet. The panel is anchored to whichever edge
 * its centre is nearer, which holds for any height below the shell's own 90%
 * ceiling.
 */
export function describeDock(
  panel: { top: number; height: number },
  viewportHeight: number,
): Dock | null {
  const height = Math.round(panel.height);
  if (height <= 0 || viewportHeight <= 0) {
    return null;
  }
  const location =
    panel.top + panel.height / 2 < viewportHeight / 2 ? "top" : "bottom";
  return { location, height };
}

let installed: (() => void) | undefined;

/**
 * Watches for the shell's panel and makes room for it while it is open, and
 * returns a function that stops the watch and restores the full-size page.
 *
 * A second install replaces the first, so a hot reload cannot leave two watches
 * fighting over the same properties.
 */
export function installDevtoolsDock(): () => void {
  installed?.();

  const resize = new ResizeObserver(() => sync());
  let observed: HTMLElement | null = null;

  function sync(): void {
    const panel = document.querySelector<HTMLElement>(panelSelector);
    if (panel !== observed) {
      if (observed) resize.unobserve(observed);
      if (panel) resize.observe(panel);
      observed = panel;
    }
    applyDock(
      panel !== null && panel.dataset.open === "true"
        ? describeDock(panel.getBoundingClientRect(), window.innerHeight)
        : null,
    );
  }

  const mutations = new MutationObserver(sync);
  mutations.observe(document.body, {
    childList: true,
    subtree: true,
    attributes: true,
    attributeFilter: ["data-open", "class", "style"],
  });
  window.addEventListener("resize", sync);
  sync();

  const stopWatching = () => {
    if (installed !== stopWatching) {
      return;
    }
    installed = undefined;
    mutations.disconnect();
    resize.disconnect();
    window.removeEventListener("resize", sync);
    applyDock(null);
  };
  installed = stopWatching;
  return stopWatching;
}

/**
 * Writes the page's share of the viewport. An undocked page removes the
 * properties instead of writing the defaults, so `index.css` stays the single
 * definition of what a page looks like without devtools.
 */
function applyDock(dock: Dock | null): void {
  const style = document.documentElement.style;
  if (dock === null) {
    style.removeProperty(viewportHeightProperty);
    style.removeProperty(stickyOffsetProperty);
    style.removeProperty(gutterTopProperty);
    style.removeProperty(gutterBottomProperty);
    return;
  }
  const size = `${dock.height}px`;
  const atTop = dock.location === "top";
  style.setProperty(viewportHeightProperty, `calc(100dvh - ${size})`);
  style.setProperty(stickyOffsetProperty, atTop ? size : "0px");
  style.setProperty(gutterTopProperty, atTop ? size : "0px");
  style.setProperty(gutterBottomProperty, atTop ? "0px" : size);
}

if (import.meta.hot) {
  import.meta.hot.dispose(() => installed?.());
}
