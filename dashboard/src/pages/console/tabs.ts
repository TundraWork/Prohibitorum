/**
 * Tab state for the console's tabbed pages.
 *
 * The URL search string is the only source of truth: the page renders the tab
 * the query names, and switching a tab navigates with `replace: true` so the
 * back button still leaves the page instead of stepping through every tab the
 * user looked at.
 *
 * Validation is deliberately exact. A missing, unknown or repeated `tab` falls
 * back to the page's first tab rather than being guessed at — no trimming, no
 * case folding — so a link someone shares either lands on a real tab or on the
 * default one, never on a 404 or a half-rendered page.
 */

/** Narrows the raw search value to one of `allowed`, else `allowed[0]`. */
export function pickTab<T extends string>(
  value: unknown,
  allowed: readonly T[],
): T {
  const fallback = allowed[0] as T;
  if (typeof value !== "string") return fallback;
  // Only the first value of a repeated parameter is considered; anything that
  // arrives as a list is not a tab the page can render.
  return (allowed as readonly string[]).includes(value)
    ? (value as T)
    : fallback;
}

export const profileTabs = ["display-name", "avatar"] as const;
export type ProfileTab = (typeof profileTabs)[number];

export function profileTab(value: unknown): ProfileTab {
  return pickTab(value, profileTabs);
}

export const securityTabs = [
  "passkeys",
  "password",
  "sessions",
  "identities",
  "tokens",
] as const;
export type SecurityTab = (typeof securityTabs)[number];

export function securityTab(value: unknown): SecurityTab {
  return pickTab(value, securityTabs);
}
