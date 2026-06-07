/**
 * safeReturnTo — same-origin return-URL guard.
 *
 * Accepts a string (or undefined) from untrusted input (query params, API
 * responses) and returns a safe, relative path on the current origin.
 * Any value that could redirect the user off-origin is rejected in favour of '/'.
 *
 * Rules:
 *  - Must start with exactly one '/' (not '//' which is protocol-relative).
 *  - Must not contain a scheme character sequence (i.e. must not match /^[a-z][a-z0-9+\-.]*:/i).
 *  - When resolved via new URL(raw, origin) the resulting origin must equal window.location.origin.
 */
export function safeReturnTo(raw: string | undefined): string {
  if (!raw) return '/'

  // Reject anything that doesn't start with a single '/'
  // (catches absolute URLs, protocol-relative '//', scheme URLs like 'javascript:')
  if (!raw.startsWith('/') || raw.startsWith('//')) return '/'

  // Belt-and-suspenders: reject if there's a scheme-like pattern before the path
  // (catches any encoding tricks that might slip through the slash check)
  if (/^[a-z][a-z0-9+\-.]*:/i.test(raw)) return '/'

  // Resolve against current origin and verify the result stays on-origin.
  // In jsdom/vitest the origin is 'http://localhost', which is fine for tests.
  try {
    const resolved = new URL(raw, window.location.origin)
    if (resolved.origin !== window.location.origin) return '/'
    // Return the path + search + hash (no origin prefix)
    return resolved.pathname + resolved.search + resolved.hash
  } catch {
    return '/'
  }
}
