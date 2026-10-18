import { type RefObject, useEffect, useState } from "react";

/**
 * The element hosting `anchorRef`'s popups, when the anchor sits inside a
 * modal or popover built on react-aria (marked `data-rac`). react-aria overlays
 * make the rest of the page inert and close on outside presses, so a popup
 * portaled to <body> would be unusable there; portaling into the host keeps it
 * live. coss dialogs and toasts are not matched: their transformed popup would
 * become the popup's containing block.
 */
export function useHostOverlayContainer(
  anchorRef: RefObject<HTMLElement | null>,
): HTMLElement | undefined {
  const [container, setContainer] = useState<HTMLElement | undefined>();
  useEffect(() => {
    const host = anchorRef.current?.closest<HTMLElement>(
      '[role="dialog"][data-rac]',
    );
    setContainer(host ?? undefined);
  }, [anchorRef]);
  return container;
}
