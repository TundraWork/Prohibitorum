import { atom } from "jotai";
import { atomWithStorage, createJSONStorage } from "jotai/utils";
import {
  type ColorSchemeSlug,
  defaultColorScheme,
  isColorSchemeSlug,
} from "@/lib/color-scheme";

/**
 * The colour scheme and light/dark mode every page shows, decided in order:
 *
 * 1. the signed-in account's saved preference, which also refreshes this
 *    browser's choice (`useAccountAppearanceSync`);
 * 2. this browser's choice, made on the sign-in page or carried over from the
 *    last account signed in here;
 * 3. the instance's default scheme, and the device's light or dark setting.
 *
 * The sign-in page's settings can force light or dark on the public pages; that
 * limits the mode only, never the scheme.
 */

export type ColorMode = "light" | "dark" | "system";

export function isColorMode(value: unknown): value is ColorMode {
  return value === "light" || value === "dark" || value === "system";
}

/**
 * Browser storage that reads anything unexpected — an older release's value,
 * or one edited by hand — as "not chosen". When another tab writes, the value
 * is read again rather than taken from the event, which can be older than
 * what is stored by the time it arrives.
 */
function checkedStorage<T>(isValid: (value: unknown) => value is T) {
  const json = createJSONStorage<T | null>(() => localStorage);
  const check = (value: unknown) => (isValid(value) ? value : null);
  return {
    ...json,
    getItem: (key: string, initial: T | null) =>
      check(json.getItem(key, initial)),
    subscribe: (
      key: string,
      callback: (value: T | null) => void,
      initial: T | null,
    ) =>
      json.subscribe?.(
        key,
        () => callback(check(json.getItem(key, initial))),
        initial,
      ),
  };
}

/** This browser's colour scheme, or null when none was chosen. */
export const schemeChoiceAtom = atomWithStorage<ColorSchemeSlug | null>(
  "prohibitorum.scheme",
  null,
  checkedStorage(isColorSchemeSlug),
  { getOnInit: true },
);

/** This browser's light/dark mode, or null when none was chosen. */
export const modeChoiceAtom = atomWithStorage<ColorMode | null>(
  "prohibitorum.mode",
  null,
  checkedStorage(isColorMode),
  { getOnInit: true },
);

/**
 * The mode the sign-in page's settings force on the public pages, or nothing
 * when the visitor chooses. Only `PublicLayout` writes it, while it is mounted,
 * so the console keeps the visitor's choice.
 */
export const forcedModeAtom = atom<"light" | "dark" | undefined>(undefined);

export interface Appearance {
  scheme: ColorSchemeSlug;
  mode: ColorMode;
}

export function resolveAppearance(
  choice: { scheme: ColorSchemeSlug | null; mode: ColorMode | null },
  instanceScheme: string | undefined,
  forcedMode: "light" | "dark" | undefined,
): Appearance {
  return {
    scheme:
      choice.scheme ??
      (isColorSchemeSlug(instanceScheme) ? instanceScheme : defaultColorScheme),
    mode: forcedMode ?? choice.mode ?? "system",
  };
}

/**
 * The appearance last applied, read by `/appearance-guard.js` before the first
 * paint so a page load never flashes another scheme or mode. Keep the key and
 * the shape in step with that script.
 */
export const appliedAppearanceKey = "prohibitorum.appearance";
