import { useEffect, useState } from "react";
import type { LoginAppearance } from "@/api/raw-paths";
import { gradientBackground } from "@/components/custom/login-appearance/gradients";
import type { LoginBackground } from "@/components/custom/login-appearance/use-login-background";
import { useReducedMotion } from "@/components/custom/login-appearance/use-reduced-motion";

/**
 * The sign-in page's background, drawn by the saved source, covering the
 * window behind everything.
 *
 * Uploaded images and the picture services draw the pictures
 * `useLoginBackground` chose; until a wallpaper arrives, or when it could not
 * be read, nothing is drawn and the page keeps its own background. Every
 * picture is decoration: empty `alt`, hidden from readers.
 */
export function LoginBackdrop({
  appearance,
  background: shown,
  readable = false,
}: {
  appearance: LoginAppearance;
  background: LoginBackground;
  /**
   * Fetch the pictures so that a script may read their pixels, as a preview
   * does to measure what is behind its card. A picture whose host will not
   * allow that is fetched the ordinary way instead.
   */
  readable?: boolean;
}) {
  const background = appearance.background;
  const position = "pointer-events-none fixed inset-0 -z-10";

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
    case "pixabay":
    case "images":
      if (shown.pictures.length === 0) return null;
      return (
        <Pictures
          readable={readable}
          className={position}
          current={shown.pictures[shown.current] as string}
          previous={
            shown.previous === undefined
              ? undefined
              : shown.pictures[shown.previous]
          }
        />
      );
    default:
      return null;
  }
}

/**
 * The picture on screen, faded in once it has loaded so a slow one never
 * paints in bands: the first over 700ms, a change over a second with the one
 * before underneath until the new one covers it. With reduced motion it
 * appears, and swaps, in place.
 */
function Pictures({
  current,
  previous,
  className,
  readable,
}: {
  current: string;
  previous: string | undefined;
  className: string;
  readable: boolean;
}) {
  const reduced = useReducedMotion();
  const changed = previous !== undefined && previous !== current;
  return (
    <div
      aria-hidden="true"
      data-login-backdrop="pictures"
      className={className}
    >
      {changed && !reduced && (
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
        readable={readable}
        fade={reduced ? undefined : changed ? "duration-1000" : "duration-700"}
      />
    </div>
  );
}

function FadingImage({
  src,
  fade,
  readable,
}: {
  src: string;
  readable: boolean;
  /** The fade's duration class; none draws the picture as soon as it loads. */
  fade: "duration-700" | "duration-1000" | undefined;
}) {
  const animate = fade !== undefined;
  const [loaded, setLoaded] = useState(false);
  const [shown, setShown] = useState(false);
  // A host that sends no CORS headers refuses a readable fetch, and the
  // picture is then drawn the ordinary way.
  const [refused, setRefused] = useState(false);
  useEffect(() => {
    if (!loaded || !animate) return;
    // One frame at zero opacity, so the transition has a start to run from
    // even when the picture was already decoded.
    const frame = window.requestAnimationFrame(() => setShown(true));
    return () => window.cancelAnimationFrame(frame);
  }, [loaded, animate]);
  return (
    <img
      src={src}
      alt=""
      data-current=""
      crossOrigin={readable && !refused ? "anonymous" : undefined}
      onError={() => {
        if (readable) setRefused(true);
      }}
      onLoad={() => setLoaded(true)}
      className={`absolute inset-0 size-full object-cover ${animate ? `transition-opacity ${fade} ease-out` : ""} ${(animate ? shown : loaded) ? "opacity-100" : "opacity-0"}`}
    />
  );
}
