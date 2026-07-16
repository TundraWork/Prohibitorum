/**
 * Time formatting helpers for admin lists.
 * - relativeTime: compact past-relative ("3h ago"); future clamps to "just now".
 *   English-literal (English-first; i18n of relative units deferred with zh).
 * - formatDateTime: absolute locale string — use for FUTURE times (expiries),
 *   where relative reads wrong.
 */
export function relativeTime(iso: string | null | undefined, now: number = Date.now()): string {
  if (!iso) return '—'
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return '—'
  const s = Math.floor(Math.max(0, now - t) / 1000)
  if (s < 60) return 'just now'
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  const d = Math.floor(h / 24)
  if (d < 30) return `${d}d ago`
  const mo = Math.floor(d / 30)
  if (d < 365) return `${mo}mo ago`
  return `${Math.floor(d / 365)}y ago`
}

export function formatDateTime(iso: string | null | undefined, locale?: Intl.LocalesArgument): string {
  if (!iso) return '—'
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return '—'
  return new Date(t).toLocaleString(locale)
}
