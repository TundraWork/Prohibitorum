import { z } from "zod";
import { searchChoice } from "@/app/search-params";

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

/** The search schema of a page with these tabs: `?tab=`, else the first. */
export function tabSearch<const T extends readonly [string, ...string[]]>(
  tabs: T,
) {
  return z.object({ tab: searchChoice(tabs, tabs[0]) });
}

/**
 * Sections of one account in the management area. The profile form stands
 * alone, the per-account actions each own a block, and everything read-only
 * about how the account gets in shares the last tab, so the destructive
 * controls are not stacked beside the fields a reader came to edit.
 */
export const accountTabs = ["profile", "access", "danger"] as const;
export type AccountTab = (typeof accountTabs)[number];

/**
 * The instance settings, one tab per kind: what the instance is called and
 * looks like, whether it is open, how it reads client addresses, and the keys
 * it signs with.
 */
export const settingsTabs = [
  "general",
  "maintenance",
  "network",
  "keys",
] as const;
export type SettingsTab = (typeof settingsTabs)[number];
