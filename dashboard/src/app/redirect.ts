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
