import { type RefObject, useEffect, useState } from "react";

/**
 * The element hosting `anchorRef`'s popups, when the anchor sits inside a
 * modal or popover that is not a coss overlay. react-aria overlays make the
 * rest of the page inert and close on outside presses, so a popup portaled to
 * <body> would be unusable there; portaling into the host keeps it live.
 */
export function useHostOverlayContainer(
  anchorRef: RefObject<HTMLElement | null>,
): HTMLElement | undefined {
  const [container, setContainer] = useState<HTMLElement | undefined>();
  useEffect(() => {
    const host = anchorRef.current?.closest<HTMLElement>('[role="dialog"]');
    setContainer(host ?? undefined);
  }, [anchorRef]);
  return container;
}
