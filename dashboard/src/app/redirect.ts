import type { RegisteredRouter } from "@tanstack/react-router";
import { loadDocument } from "@/app/load-document";

// A browser reads a tab, CR or LF in an address as nothing, so
// "/%09/example.org" arrives at "//example.org": a protocol-relative
// redirect to another host. One decode is not enough: %2509 becomes %09 at
// the router and the character again at the next decode, so the value is
// decoded until it stops changing and the fully decoded form is what gets
// checked. A backslash is refused anywhere in the path for the same reason
// (a browser normalises \ to /), and a NUL with it. Only the path is
// examined: a query stays at the endpoint this origin serves, so a state
// value carrying %, %5C or %0A is data, not a way off the site.
const unsafeAfterDecoding = /[\t\n\r\0\\]|%0[9ad]|%5c|%00/i;
const decodeLimit = 8;

/** The path portion (everything before '?'), decoded until it stops changing. */
function fullyDecodedPath(value: string): string {
  const end = value.indexOf("?");
  let current = end === -1 ? value : value.slice(0, end);
  for (let i = 0; i < decodeLimit; i += 1) {
    const next = decodeURIComponent(current);
    if (next === current) return current;
    current = next;
  }
  return current;
}

/** The path portion (everything before '?') as written. */
function pathPortion(value: string): string {
  const end = value.indexOf("?");
  return end === -1 ? value : value.slice(0, end);
}

/**
 * A path on this site. A value starting with `//` or `/\` is read by the
 * browser as another host, and a path carrying a tab, CR, LF, NUL or
 * backslash — including percent-encoded, however many layers deep —
 * collapses into one, so none of them is a path on this site. Only the path
 * portion before '?' is examined, for the reason above.
 */
export function isSitePath(value: string | undefined): value is string {
  if (value?.startsWith("/") !== true) return false;
  if (value.startsWith("//") || value.startsWith("/\\")) return false;
  if (unsafeAfterDecoding.test(pathPortion(value))) return false;
  try {
    return !unsafeAfterDecoding.test(fullyDecodedPath(value));
  } catch {
    // A path that cannot be decoded is malformed, not a path on this site.
    return false;
  }
}

/**
 * A path on this site, from a target that may also name this site's origin.
 * A plain path is checked as written; an absolute URL is parsed, must be
 * this site's, and its path is rebuilt from the parsed parts. Anything
 * off-site, unreadable, or whose path is not a site path is refused.
 * Navigate to the value returned here, never to the input it came from.
 */
export function sitePathOf(value: string | undefined): string | undefined {
  if (value === undefined || value === "") return undefined;
  if (value.startsWith("/")) return isSitePath(value) ? value : undefined;
  let target: URL;
  try {
    target = new URL(value);
  } catch {
    return undefined;
  }
  if (target.origin !== window.location.origin) return undefined;
  const path = target.pathname + target.search + target.hash;
  return isSitePath(path) ? path : undefined;
}

/**
 * Follows a redirect target the server has already validated. A target that
 * matches a dashboard route navigates client-side; anything else — server
 * endpoints such as `/oauth/authorize` that must answer the request — loads
 * as a full document navigation.
 */
export async function followRedirect(
  router: RegisteredRouter,
  target: string,
): Promise<void> {
  const url = new URL(target, window.location.origin);
  if (url.origin === window.location.origin) {
    const [, , route] = router.getMatchedRoutes(url.pathname);
    if (route) {
      await router.navigate({ href: url.pathname + url.search + url.hash });
      return;
    }
  }
  loadDocument(target);
}
