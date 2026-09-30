import { useEffect, useRef, useState } from "react";
import type { LoginAppearance, Wallpaper } from "@/api/raw-paths";
import { gradientBackground } from "@/components/custom/login-appearance/gradients";
import { useReducedMotion } from "@/components/custom/login-appearance/use-reduced-motion";

/**
 * The sign-in page's background, drawn by the saved source. `contained` fills
 * the nearest positioned ancestor (the settings preview); otherwise it covers
 * the window behind everything.
 *
 * Bing and Unsplash take the `wallpaper` the caller read; until it arrives, or
 * when it could not be read, nothing is drawn and the page keeps its own
 * background. Every picture is decoration: empty `alt`, hidden from readers.
 */
export function LoginBackdrop({
  appearance,
  images,
  wallpaper,
  contained = false,
}: {
  appearance: LoginAppearance;
  /** The uploaded images' URLs, in upload order. */
  images: string[];
  wallpaper?: Wallpaper;
  contained?: boolean;
}) {
  const background = appearance.background;
  const position = contained
    ? "pointer-events-none absolute inset-0"
    : "pointer-events-none fixed inset-0 -z-10";

  switch (background.source) {
    case "color":
      return (
        <div
          aria-hidden="true"
          data-login-backdrop="color"
          className={position}
          style={{ backgroundColor: background.color }}
        />
      );
    case "gradient":
      return (
        <div
          aria-hidden="true"
          data-login-backdrop="gradient"
          data-gradient={background.gradient}
          className={position}
          style={{ background: gradientBackground[background.gradient] }}
        />
      );
    case "bing":
    case "unsplash":
      if (wallpaper === undefined || wallpaper.source !== background.source) {
        return null;
      }
      return (
        <Photo
          key={wallpaper.imageUrl}
          className={position}
          src={wallpaper.imageUrl}
        />
      );
    case "images":
      if (images.length === 0) return null;
      return (
        <ImageRotation
          className={position}
          images={images}
          carousel={background.images.order === "carousel"}
          intervalSeconds={background.images.intervalSeconds}
        />
      );
    default:
      return null;
  }
}

/** A photo fades in once it has loaded, so a slow picture never paints in bands. */
function Photo({ src, className }: { src: string; className: string }) {
  const reduced = useReducedMotion();
  const [loaded, setLoaded] = useState(false);
  return (
    <img
      src={src}
      alt=""
      aria-hidden="true"
      data-login-backdrop="photo"
      onLoad={() => setLoaded(true)}
      className={`${className} size-full object-cover ${reduced ? "" : "transition-opacity duration-700 ease-out"} ${loaded ? "opacity-100" : "opacity-0"}`}
    />
  );
}

/**
 * Uploaded images. `random` picks one when the page opens and keeps it for the
 * visit. `carousel` moves to the next every interval: the next picture is
 * decoded first, then drawn over the current one and faded in over a second,
 * so the change never shows a half-loaded image. With reduced motion it swaps
 * in place.
 */
function ImageRotation({
  images,
  carousel,
  intervalSeconds,
  className,
}: {
  images: string[];
  carousel: boolean;
  intervalSeconds: number;
  className: string;
}) {
  const reduced = useReducedMotion();
  const [index, setIndex] = useState(() =>
    Math.floor(Math.random() * images.length),
  );
  const [previous, setPrevious] = useState<string | undefined>(undefined);
  const count = images.length;
  const current = images[index % count] as string;
  // The list is rebuilt on every render; the timer restarts only when its
  // contents change.
  const imagesRef = useRef(images);
  imagesRef.current = images;
  const listKey = images.join("\n");

  useEffect(() => {
    if (!carousel || count < 2 || listKey === "") return;
    let cancelled = false;
    const timer = window.setInterval(() => {
      const list = imagesRef.current;
      const next = (index + 1) % list.length;
      const url = list[next] as string;
      const picture = new Image();
      picture.src = url;
      // A picture that will not decode is shown anyway; the browser draws
      // what it has rather than stalling the rotation.
      void picture
        .decode()
        .catch(() => undefined)
        .then(() => {
          if (cancelled) return;
          setPrevious(list[index % list.length]);
          setIndex(next);
        });
    }, intervalSeconds * 1000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [carousel, count, listKey, index, intervalSeconds]);

  return (
    <div aria-hidden="true" data-login-backdrop="images" className={className}>
      {previous !== undefined && previous !== current && !reduced && (
        <img
          key={`previous-${previous}`}
          src={previous}
          alt=""
          className="absolute inset-0 size-full object-cover"
        />
      )}
      <FadingImage
        key={current}
        src={current}
        animate={!reduced && previous !== undefined}
      />
    </div>
  );
}

function FadingImage({ src, animate }: { src: string; animate: boolean }) {
  const [shown, setShown] = useState(!animate);
  useEffect(() => {
    if (!animate) return;
    // One frame at zero opacity, so the transition has a start to run from.
    const frame = window.requestAnimationFrame(() => setShown(true));
    return () => window.cancelAnimationFrame(frame);
  }, [animate]);
  return (
    <img
      src={src}
      alt=""
      data-current=""
      className={`absolute inset-0 size-full object-cover ${animate ? "transition-opacity duration-1000 ease-out" : ""} ${shown ? "opacity-100" : "opacity-0"}`}
    />
  );
}
