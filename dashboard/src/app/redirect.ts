import type { RegisteredRouter } from "@tanstack/react-router";

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
  window.location.assign(target);
}
