/**
 * Frontend pagination types and helpers — mirrors the backend
 * pagination.Page[T] wire envelope:
 *
 *   {"items":[...],"nextCursor":"opaque-or-empty"}
 *
 * NextCursor is always present (never omitted), even on the final page where
 * it serializes as the empty string. This lets the UI branch on nextCursor
 * alone without a separate hasMore flag.
 */

export interface Page<T> {
  items: T[]
  nextCursor: string
}

export interface CursorPageResult<T> {
  items: T[]
  nextCursor: string
}

/**
 * unwrap safely extracts items + nextCursor from a possibly-undefined page
 * response. A missing response yields { items: [], nextCursor: '' }.
 */
export function unwrap<T>(page: Page<T> | undefined | null): CursorPageResult<T> {
  if (!page) return { items: [], nextCursor: '' }
  return { items: page.items ?? [], nextCursor: page.nextCursor ?? '' }
}

/**
 * emptyPage constructs a zero-item page with an empty cursor, matching the
 * backend's final/empty page serialization.
 */
export function emptyPage<T>(): Page<T> {
  return { items: [], nextCursor: '' }
}

/**
 * buildPagePath appends optional cursor and limit query params to a base API
 * path, preserving any existing query string. Empty/undefined values are
 * omitted so the URL stays clean.
 */
export function buildPagePath(
  base: string,
  opts: { cursor?: string; limit?: number },
): string {
  const url = new URL(base, 'http://localhost')
  if (opts.cursor) url.searchParams.set('cursor', opts.cursor)
  if (opts.limit != null) url.searchParams.set('limit', String(opts.limit))
  const query = url.searchParams.toString()
  // Strip the leading '/' from pathname (URL always adds it), then re-attach
  // the original path structure. We use the original base to preserve relative
  // vs absolute semantics, then append our params.
  const baseQueryIdx = base.indexOf('?')
  const cleanBase = baseQueryIdx >= 0 ? base.slice(0, baseQueryIdx) : base
  return query ? `${cleanBase}?${query}` : cleanBase
}
