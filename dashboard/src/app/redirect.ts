import type { RegisteredRouter } from "@tanstack/react-router";
import { loadDocument } from "@/app/load-document";

/**
 * A path on this site. A value starting `//` or `/\` is read by the browser
 * as another host, so it is not one.
 */
export function isSitePath(value: string | undefined): value is string {
  return (
    value?.startsWith("/") === true &&
    !value.startsWith("//") &&
    !value.startsWith("/\\")
  );
}

/**
 * A path on this site, from a target that may also name this site's origin:
 * a same-origin absolute URL resolves to its path, while anything off-site or
 * unreadable is refused. The server's twin is `resolveReturnTo`.
 */
export function sitePathOf(value: string | undefined): string | undefined {
  if (isSitePath(value)) return value;
  if (value === undefined) return undefined;
  let target: URL;
  try {
    target = new URL(value);
  } catch {
    return undefined;
  }
  if (target.origin !== window.location.origin) return undefined;
  return target.pathname + target.search + target.hash;
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
