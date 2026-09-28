/**
 * Leaves the console for `target` as a full document load: a server endpoint
 * that has to answer the request itself, or a page whose state must not carry
 * over. Its own module, so a test can stand in for the browser's navigation.
 */
export function loadDocument(target: string): void {
  window.location.assign(target);
}
