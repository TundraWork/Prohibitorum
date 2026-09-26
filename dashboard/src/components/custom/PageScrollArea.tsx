import { OverlayScrollbars } from "overlayscrollbars";
import { useEffect } from "react";

/**
 * Draws the page's own scrollbar with OverlayScrollbars.
 *
 * The page scrolls on the document itself — `body` is `min-h-screen` and the
 * shell only sets a minimum height — so this is the one scroll area that is not
 * an element the app renders, and `ScrollArea` cannot wrap it. The library
 * takes the `body` element and makes `html` its viewport, which is why the
 * sticky header and rail keep working: nothing the app positions is wrapped or
 * moved, and `window.scrollY`/`window.scrollTo` remain the way to scroll.
 *
 * Rendered once by `AppLayout`, so every route has it. The theme class comes
 * from `styles/index.css`, the same one `ScrollArea` uses.
 */
export function PageScrollArea() {
  useEffect(() => {
    const instance = OverlayScrollbars(document.body, {
      scrollbars: { theme: "os-theme-app" },
    });
    return () => {
      instance.destroy();
    };
  }, []);

  return null;
}
