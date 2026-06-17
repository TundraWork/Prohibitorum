/**
 * safeReturnTo — same-origin return-URL guard.
 *
 * Accepts a string (or undefined) from untrusted input (query params, API
 * responses) and returns a safe, same-origin path to navigate to. Any value
 * that could redirect the user off-origin is rejected in favour of '/'.
 *
 * Accepts both a relative path (`/oauth/authorize?…`, set by the SPA's auth
 * guard) AND a same-origin absolute URL (`https://idp.example/oauth/authorize?…`,
 * which the server emits when bouncing an unauthenticated user to /login) —
 * both resolve to the current origin. The result is always a relative path so
 * the caller's navigation can never be read as protocol-relative.
 *
 * Rejects:
 *  - protocol-relative `//evil.com`;
 *  - cross-origin absolute URLs (origin ≠ current origin);
 *  - non-http(s) schemes like `javascript:` / `data:` (opaque "null" origin);
 *  - anything that resolves to a `//…` path (e.g. the `/\evil.com` trick).
 */
export function safeReturnTo(raw: string | undefined): string {
  if (!raw) return '/'

  // Protocol-relative ('//evil.com') would navigate off-origin — reject outright.
  if (raw.startsWith('//')) return '/'

  try {
    // Resolve relative paths and absolute URLs alike against the current origin.
    // Same-origin is the security boundary: cross-origin values and non-http
    // schemes (javascript:/data:, whose origin is the opaque "null") fail here.
    const resolved = new URL(raw, window.location.origin)
    if (resolved.origin !== window.location.origin) return '/'

    const path = resolved.pathname + resolved.search + resolved.hash
    // Never emit a value that could be read as protocol-relative: a "/\evil.com"
    // input can stay on-origin but resolve to a "//evil.com" path.
    if (path.startsWith('//')) return '/'
    return path
  } catch {
    return '/'
  }
}
