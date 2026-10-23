import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import type { LoginAppearanceWrite } from "@/api/raw-admin-paths";
import type { LoginAppearance, LoginBackgroundSource } from "@/api/raw-paths";

/**
 * The sign-in page settings' rules, checked here in full.
 *
 * The server checks the same things (`branding.ValidateAppearance` and the key
 * pattern) but answers any failure with one `bad_request` and no field, so the
 * form has to say which value is wrong itself. Values are never trimmed or
 * rewritten on the administrator's behalf.
 */

export interface SignInPageValues {
  appearance: LoginAppearance;
  /** Empty unless the administrator typed a new key. */
  unsplashAccessKey: string;
  /** Empty unless the administrator typed a new Pixabay API key. */
  pixabayApiKey: string;
}

export const backgroundSources: LoginBackgroundSource[] = [
  "none",
  "color",
  "gradient",
  "bing",
  "unsplash",
  "pixabay",
  "images",
];

/**
 * Quick picks beside the colour picker: the theme's teal, then quiet darks and
 * lights. React Aria names each for screen readers, and every name differs.
 */
export const suggestedColors = [
  "#1f6f8b",
  "#3b3f58",
  "#4c5d8a",
  "#5b7a5a",
  "#8a5a44",
  "#101418",
  "#b9c4cc",
  "#efe9df",
];

/** Bing's markets, most likely first; the server accepts exactly these. */
export const bingMarkets = [
  "zh-CN",
  "en-US",
  "en-GB",
  "ja-JP",
  "de-DE",
  "fr-FR",
  "es-ES",
  "it-IT",
  "pt-BR",
  "en-AU",
  "en-CA",
  "fr-CA",
  "en-IN",
];

/** `branding.MaxLoginImages`. */
export const maxLoginImages = 10;

/** What `imageutil.ValidateRaw` accepts for a sign-in image; no GIF. */
export const loginImageTypes = ["image/png", "image/jpeg", "image/webp"];

export const minIntervalSeconds = 5;
export const maxIntervalSeconds = 3600;
const maxQueryLength = 64;

const hexColor = /^#[0-9a-f]{6}$/;
const unsplashKeyPattern = /^[A-Za-z0-9_-]{1,128}$/;
// biome-ignore lint/suspicious/noControlCharactersInRegex: the rule is about control characters.
const controlCharacter = /[\u0000-\u001f\u007f-\u009f]/;

export const signInPageMessages = {
  colorInvalid: msg({
    id: "settings.sign-in.color.invalid",
    message: "Enter a color as # followed by six hexadecimal digits.",
  }),
  queryTooLong: msg({
    id: "settings.sign-in.unsplash.query.too_long",
    message: "Use 64 characters or fewer.",
  }),
  querySpaces: msg({
    id: "settings.sign-in.unsplash.query.spaces",
    message: "Remove the spaces at the start or end.",
  }),
  queryControl: msg({
    id: "settings.sign-in.unsplash.query.control",
    message: "Remove the tab or line break.",
  }),
  keyRequired: msg({
    id: "settings.sign-in.key.required",
    message: "Enter the {service} key before saving.",
  }),
  keyInvalid: msg({
    id: "settings.sign-in.unsplash.key.invalid",
    message:
      "An access key has only letters, digits, hyphens and underscores, up to 128 of them.",
  }),
  intervalRange: msg({
    id: "settings.sign-in.interval.range",
    message: "Choose between 5 and 3600 seconds.",
  }),
} as const;

export function colorError(value: string): MessageDescriptor | undefined {
  return hexColor.test(value) ? undefined : signInPageMessages.colorInvalid;
}

export function queryError(value: string): MessageDescriptor | undefined {
  if ([...value].length > maxQueryLength)
    return signInPageMessages.queryTooLong;
  if (controlCharacter.test(value)) return signInPageMessages.queryControl;
  if (value !== value.trim()) return signInPageMessages.querySpaces;
  return undefined;
}

export function intervalError(value: number): MessageDescriptor | undefined {
  return Number.isInteger(value) &&
    value >= minIntervalSeconds &&
    value <= maxIntervalSeconds
    ? undefined
    : signInPageMessages.intervalRange;
}

/**
 * A key is checked only when one is being entered; it is required when the
 * background is that key's service and no key is saved to fall back on.
 */
export function keyError(
  value: string,
  context: {
    source: LoginBackgroundSource;
    service: "unsplash" | "pixabay";
    hasSavedKey: boolean;
  },
): MessageDescriptor | undefined {
  if (value === "") {
    return context.source === context.service && !context.hasSavedKey
      ? {
          ...signInPageMessages.keyRequired,
          values: {
            service:
              context.service === "unsplash"
                ? "Unsplash access"
                : "Pixabay API",
          },
        }
      : undefined;
  }
  return unsplashKeyPattern.test(value)
    ? undefined
    : signInPageMessages.keyInvalid;
}

/**
 * The whole appearance, and each key only while its service is chosen and a
 * new one was typed.
 */
export function signInPageBody(values: SignInPageValues): LoginAppearanceWrite {
  const body: LoginAppearanceWrite = { appearance: values.appearance };
  if (
    values.unsplashAccessKey !== "" &&
    values.appearance.background.source === "unsplash"
  ) {
    body.unsplashAccessKey = values.unsplashAccessKey;
  }
  if (
    values.pixabayApiKey !== "" &&
    values.appearance.background.source === "pixabay"
  ) {
    body.pixabayApiKey = values.pixabayApiKey;
  }
  return body;
}

/**
 * How many settings the draft changes from what is saved, a typed key
 * counting as one each. Each leaf value that differs is one change, so
 * choosing a source and then its picture reads as two.
 */
export function changedSettings(
  saved: LoginAppearance,
  draft: LoginAppearance,
  typedKeys: string[],
): number {
  return (
    changedValues(saved, draft) + typedKeys.filter((key) => key !== "").length
  );
}

/** How many leaf values differ between two settings objects. */
export function changedValues(a: unknown, b: unknown): number {
  if (
    typeof a === "object" &&
    a !== null &&
    typeof b === "object" &&
    b !== null
  ) {
    return Object.keys(a).reduce(
      (sum, key) =>
        sum +
        changedValues(
          (a as Record<string, unknown>)[key],
          (b as Record<string, unknown>)[key],
        ),
      0,
    );
  }
  return Object.is(a, b) ? 0 : 1;
}

/**
 * Where the changes are: the background (and each typed key), the card's
 * place, theme, default scheme and surface, and the toolbar's surface.
 */
export function changesBySection(
  saved: LoginAppearance,
  draft: LoginAppearance,
  typedKeys: string[],
) {
  return {
    background:
      changedValues(saved.background, draft.background) +
      typedKeys.filter((key) => key !== "").length,
    card:
      changedValues(saved.cardPosition, draft.cardPosition) +
      changedValues(saved.theme, draft.theme) +
      changedValues(saved.scheme, draft.scheme) +
      changedValues(saved.card, draft.card),
    toolbar: changedValues(saved.capsules, draft.capsules),
  };
}
