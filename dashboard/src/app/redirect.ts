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
 * matches a dashboard route navigates client-side. A protocol hop is asked
 * where it leads first, so that when it answers with one of the dashboard's
 * pages — authorize with the consent page, the sign-in page or the error
 * page — that page opens in place, and the public layout, its background and
 * its card stay on screen instead of the whole app loading again. Anything
 * else, a hop that leads to an application's own address included, loads as
 * a full document navigation.
 */
export async function followRedirect(
  router: RegisteredRouter,
  target: string,
): Promise<void> {
  let next = target;
  for (let hop = 0; hop <= hopLimit; hop += 1) {
    const url = new URL(next, window.location.origin);
    if (url.origin !== window.location.origin) break;
    const [, , route] = router.getMatchedRoutes(url.pathname);
    if (route) {
      await router.navigate({ href: url.pathname + url.search + url.hash });
      return;
    }
    if (!followablePaths.has(url.pathname) || hop === hopLimit) break;
    const location = await nextHop(url);
    if (location === undefined) break;
    next = location;
  }
  loadDocument(next);
}

/**
 * The server's protocol hops that, asked with `X-Prohibitorum-Navigation`,
 * name where they lead instead of redirecting there (`navigationMW`). Every
 * outcome of each is a redirect; SAML SSO is not one, because it can answer
 * with a form the browser has to post.
 */
const followablePaths = new Set([
  "/oauth/authorize",
  "/api/prohibitorum/account-chooser/continue",
]);

/** Enough for continue → authorize → a page, with room to spare. */
const hopLimit = 4;

/**
 * Where a protocol hop leads, or nothing when the answer cannot be read — an
 * older server, a failure, a lost connection — and the hop is loaded as a
 * document after all. Running the hop again is safe then: an answer that is
 * not a redirect left nothing, such as a code, waiting at an address.
 */
async function nextHop(url: URL): Promise<string | undefined> {
  try {
    const response = await fetch(
      new Request(url, {
        headers: {
          "X-Prohibitorum-Navigation": "follow",
          Accept: "application/json",
        },
        credentials: "same-origin",
        cache: "no-store",
        // A server that ignores the header redirects; following that here
        // would deliver the hop's destination to fetch instead of the browser.
        redirect: "manual",
      }),
    );
    if (!response.ok) return undefined;
    const body: unknown = await response.json();
    const location =
      typeof body === "object" && body !== null && "location" in body
        ? body.location
        : undefined;
    return typeof location === "string" && location !== ""
      ? location
      : undefined;
  } catch {
    return undefined;
  }
}
