import type { LoginBackgroundSource } from "@/api/raw-paths";

/**
 * The picture services the sign-in page can draw its background from. The
 * server accepts the same sources (`branding`); a new service joins this list
 * and the server's. Uploaded images are not a service and stay out.
 */
export const photoSources = [
  "bing",
  "unsplash",
  "pixabay",
  "openverse",
] as const satisfies readonly LoginBackgroundSource[];

/**
 * The services that serve a batch of pictures, so the page shows one at
 * random or rotates through them; Bing has one picture a day.
 */
export const rotatingPhotoSources = [
  "unsplash",
  "pixabay",
  "openverse",
] as const;

/**
 * Pixabay's search languages, as its API docs list them; the server accepts
 * exactly these (`branding.PixabayLanguages`). The picker shows each name
 * through `Intl.DisplayNames` in the console's language.
 */
export const pixabayLanguages = [
  "cs",
  "da",
  "de",
  "en",
  "es",
  "fr",
  "id",
  "it",
  "hu",
  "nl",
  "no",
  "pl",
  "pt",
  "ro",
  "sk",
  "fi",
  "sv",
  "tr",
  "vi",
  "th",
  "bg",
  "ru",
  "el",
  "ja",
  "ko",
  "zh",
] as const;
