/**
 * Favicon management — keeps the browser tab icon in sync with the instance
 * branding.
 *
 * The static `<link rel="icon" href="/branding/icon">` in index.html points at
 * a URL that is IDENTICAL for the built-in default and any custom upload, so the
 * browser's (sticky, per-URL) favicon cache keeps serving the first one it saw.
 * We replace the link with a cache-busted URL (`/branding/icon?v=<etag>`) so a
 * changed icon yields a changed URL and is refetched instead of served stale.
 * Removing + re-appending the element (rather than mutating href) is the
 * reliable cross-browser way to force the refetch.
 */
export function setFavicon(href: string): void {
  if (typeof document === 'undefined') return
  document.querySelectorAll('link[rel="icon"]').forEach((el) => el.remove())
  const link = document.createElement('link')
  link.rel = 'icon'
  link.type = 'image/png'
  link.href = href
  document.head.appendChild(link)
}
