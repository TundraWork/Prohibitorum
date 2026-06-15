/**
 * formatUserAgent — produce a short human-readable device label from a
 * User-Agent string, e.g. "Chrome on Windows", "Safari on iPhone".
 *
 * Detection order matters:
 *   - Edge before Chrome (Edg/ token appears in Chrome-base Edge UA)
 *   - Chrome before Safari (Safari token appears in Chrome UAs too)
 *   - iPhone/iPad before macOS (iOS UAs include "Mac OS X" sub-string)
 */
export function formatUserAgent(ua: string | undefined | null): string {
  if (!ua || !ua.trim()) return 'Unknown device'

  const s = ua.trim()

  // ---- Browser detection ----
  let browser = ''
  if (/\bEdg\//.test(s) || /\bEdgA\//.test(s) || /\bEdgHTML\//.test(s)) {
    browser = 'Edge'
  } else if (/\bChrome\//.test(s) || /\bCriOS\//.test(s)) {
    browser = 'Chrome'
  } else if (/\bFirefox\//.test(s) || /\bFxiOS\//.test(s)) {
    browser = 'Firefox'
  } else if (/\bSafari\//.test(s) && /\bVersion\//.test(s)) {
    browser = 'Safari'
  }

  // ---- OS detection ----
  let os = ''
  if (/\biPhone/.test(s)) {
    os = 'iPhone'
  } else if (/\biPad/.test(s)) {
    os = 'iPad'
  } else if (/\bAndroid/.test(s)) {
    os = 'Android'
  } else if (/Windows NT/.test(s)) {
    os = 'Windows'
  } else if (/Mac OS X/.test(s) || /\bmacOS\b/.test(s)) {
    os = 'macOS'
  } else if (/\bLinux\b/.test(s)) {
    os = 'Linux'
  }

  if (browser && os) return `${browser} on ${os}`
  if (browser) return browser
  if (os) return `Browser on ${os}`

  // Fallback: first non-whitespace token, truncated to 40 chars
  const token = s.split(/[\s/]/)[0] ?? s
  return token.length > 40 ? token.slice(0, 40) : token
}
