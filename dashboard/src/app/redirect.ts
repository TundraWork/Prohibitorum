import type { RegisteredRouter } from "@tanstack/react-router";
import { loadDocument } from "@/app/load-document";

// A browser reads a tab, CR or LF in an address as nothing, so
// "/%09/example.org" arrives at "//example.org": a protocol-relative
// redirect to another host. Encoded or not, such a path is not one this
// site can be navigated to.
const controlChars = /[\t\n\r]|%0[9ad]/i;

/**
 * A path on this site. A value starting with `//` or `/\` is read by the
 * browser as another host, and a path carrying a tab, CR or LF — including
 * percent-encoded — collapses into one, so neither is a path on this site.
 */
export function isSitePath(value: string | undefined): value is string {
  if (value?.startsWith("/") !== true) return false;
  if (value.startsWith("//") || value.startsWith("/\\")) return false;
  return !controlChars.test(value);
}

/**
 * A path on this site, from a target that may also name this site's origin.
 * A plain path is checked as written; an absolute URL is parsed and must be
 * this site's, with the path rebuilt from the parsed parts. Anything
 * off-site, unreadable, or whose path is not a site path (`//…`, `/\…`, or a
 * tab, CR or LF however encoded) is refused. Navigate to the value returned
 * here, never to the input it came from.
 */
export function sitePathOf(value: string | undefined): string | undefined {
  // A return address arrives path-absolute or as a same-origin absolute URL
  // (the Go twin resolveReturnTo accepts exactly these two forms). A plain
  // path is checked as written, so its percent-encoding survives to the
  // control-character check instead of being decoded away by the parser.
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
