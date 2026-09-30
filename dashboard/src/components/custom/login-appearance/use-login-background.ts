import { useEffect, useState } from "react";
import type {
  LoginAppearance,
  Wallpaper,
  WallpaperPicture,
} from "@/api/raw-paths";

/** What the sign-in page's background shows now; see `useLoginBackground`. */
export interface LoginBackground {
  /** The picture addresses to show, empty for a source without pictures. */
  pictures: string[];
  /** Index into `pictures` of the one shown now. */
  current: number;
  /** Index of the one shown before the last change, drawn under it while it fades in. */
  previous: number | undefined;
  /** The Bing or Unsplash picture shown now, for its credit. */
  wallpaperPicture: WallpaperPicture | undefined;
}

/**
 * The background pictures for the saved source and which one is showing.
 * Uploaded images and the Unsplash batch are shown one at random per visit or
 * as a carousel, by their own settings; Bing has one picture. A wallpaper read
 * for another source counts as none.
 *
 * The rotation lives here rather than in `LoginBackdrop` so that the credit
 * follows the photo on screen.
 */
export function useLoginBackground(
  appearance: LoginAppearance,
  images: string[],
  wallpaper: Wallpaper | undefined,
): LoginBackground {
  const background = appearance.background;
  const photos =
    (background.source === "bing" || background.source === "unsplash") &&
    wallpaper?.source === background.source
      ? wallpaper.pictures
      : [];
  const pictures =
    background.source === "images"
      ? images
      : photos.map((picture) => picture.imageUrl);
  const rotation =
    background.source === "images"
      ? background.images
      : background.source === "unsplash"
        ? background.unsplash
        : undefined;
  const { current, previous } = useRotation(
    pictures,
    rotation?.order === "carousel",
    rotation?.intervalSeconds ?? 0,
  );
  return { pictures, current, previous, wallpaperPicture: photos[current] };
}

/**
 * `random` picks a picture when the page opens and keeps it for the visit.
 * `carousel` moves to the next every interval, once it has decoded, so the
 * change never shows a half-loaded image. The timer restarts when the list's
 * contents change.
 */
function useRotation(
  pictures: string[],
  carousel: boolean,
  intervalSeconds: number,
): { current: number; previous: number | undefined } {
  // Drawn once, so a list that arrives later still starts at a random picture.
  const [start] = useState(() => Math.random());
  const [step, setStep] = useState(0);
  const [previous, setPrevious] = useState<number | undefined>(undefined);
  const count = pictures.length;
  const current = count === 0 ? 0 : (Math.floor(start * count) + step) % count;
  // The list is rebuilt on every render; the timer restarts only when its
  // contents change. No address holds a line break.
  const listKey = pictures.join("\n");

  // A picture from the old list means nothing in the new one.
  const [shownKey, setShownKey] = useState(listKey);
  if (shownKey !== listKey) {
    setShownKey(listKey);
    setPrevious(undefined);
  }

  useEffect(() => {
    if (!carousel || count < 2) return;
    let cancelled = false;
    const timer = window.setInterval(() => {
      const list = listKey.split("\n");
      const next = (current + 1) % list.length;
      const picture = new Image();
      picture.src = list[next] as string;
      // A picture that will not decode is shown anyway; the browser draws
      // what it has rather than stalling the rotation.
      void picture
        .decode()
        .catch(() => undefined)
        .then(() => {
          if (cancelled) return;
          setPrevious(current);
          setStep((s) => s + 1);
        });
    }, intervalSeconds * 1000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [carousel, count, listKey, current, intervalSeconds]);

  return { current, previous };
}
