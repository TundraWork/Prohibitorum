/**
 * Full-page navigation seam.
 *
 * Isolates `window.location.assign` so flows that must LEAVE the SPA — OIDC
 * consent redirects (which may target a cross-origin relying party), federation
 * hand-off, post-auth return_to — are trivially mockable in tests and have a
 * single, greppable call site.
 *
 * This is a hard navigation: the browser issues a fresh request (so updated
 * session cookies are sent) and the SPA is torn down. Do NOT route these
 * through vue-router.
 */
export function hardRedirect(url: string): void {
  window.location.assign(url)
}
