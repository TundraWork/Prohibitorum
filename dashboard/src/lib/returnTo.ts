// Only allow navigation to a same-origin URL. Accepts absolute same-origin URLs
// and root-relative paths. Returns the safe URL or null.
export function safeReturnTo(raw: string | null): string | null {
  if (!raw) return null
  try {
    const u = new URL(raw, window.location.origin)
    return u.origin === window.location.origin ? u.toString() : null
  } catch {
    return null
  }
}
