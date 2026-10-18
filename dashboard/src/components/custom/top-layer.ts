let container: HTMLElement | undefined;

/**
 * A body-level element every coss dialog and toast portals into. react-aria
 * overlays still on the page make everything outside them inert, except what
 * sits in an element marked `data-react-aria-top-layer`.
 */
export function getTopLayerContainer(): HTMLElement {
  if (!container?.isConnected) {
    container = document.createElement("div");
    container.setAttribute("data-react-aria-top-layer", "");
    document.body.append(container);
  }
  return container;
}
