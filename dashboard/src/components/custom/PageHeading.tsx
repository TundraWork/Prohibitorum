import { type ReactNode, useCallback } from "react";

/**
 * A public page's title. It takes focus as it appears: moving between public
 * pages is a client-side navigation, so the browser leaves focus on the button
 * that was just pressed and has gone, and a screen reader would not announce
 * the new page. From the heading, the reader hears the page's question first
 * and Tab continues into its controls. It is not a control, so it is kept out
 * of the Tab order and draws no focus ring.
 */
export function PageHeading({ children }: { children: ReactNode }) {
  const focus = useCallback((node: HTMLHeadingElement | null) => {
    node?.focus();
  }, []);
  return (
    <h1
      ref={focus}
      tabIndex={-1}
      className="min-w-0 text-xl font-semibold wrap-anywhere outline-none"
    >
      {children}
    </h1>
  );
}
