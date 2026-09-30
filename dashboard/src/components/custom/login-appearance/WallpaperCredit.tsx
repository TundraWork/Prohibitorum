import { Link } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import type { LoginAppearance, Wallpaper } from "@/api/raw-paths";
import { capsuleSurfaceStyle } from "@/components/custom/login-appearance/surface-style";

/**
 * Where a Bing or Unsplash picture comes from, on a small capsule drawn like
 * the toolbar's. Bing's title and copyright appear when the administrator
 * shows the caption; the Unsplash credit is always shown, as Unsplash asks.
 * Draws nothing for any other source or before the wallpaper arrives.
 */
export function WallpaperCredit({
  appearance,
  wallpaper,
}: {
  appearance: LoginAppearance;
  wallpaper?: Wallpaper;
}) {
  const background = appearance.background;
  if (wallpaper === undefined || wallpaper.source !== background.source) {
    return null;
  }
  const surface = capsuleSurfaceStyle(appearance.capsules);
  const capsule = `min-h-8 max-w-full rounded-full px-3.5 py-1.5 text-xs shadow-surface ${surface.className}`;

  if (wallpaper.source === "bing") {
    if (!background.bing.showCaption || !wallpaper.title) return null;
    return (
      <p
        className={`inline-flex min-w-0 items-center gap-1.5 ${capsule}`}
        style={surface.style}
      >
        <span className="shrink-0 font-medium text-foreground">
          {wallpaper.title}
        </span>
        {wallpaper.copyright && (
          <>
            <span aria-hidden="true" className="text-muted">
              ·
            </span>
            <Link
              href={wallpaper.copyrightUrl}
              target="_blank"
              rel="noreferrer"
              className="block min-w-0 truncate text-xs text-muted"
            >
              {wallpaper.copyright}
            </Link>
          </>
        )}
      </p>
    );
  }

  const name = wallpaper.photographer ?? "";
  // One run of inline text inside a single flex item, which centres it in the
  // capsule's minimum height: separate items would put a gap between the words,
  // and spaces into the Chinese sentence.
  return (
    <p
      className={`inline-flex items-center text-muted ${capsule}`}
      style={surface.style}
    >
      <span>
        <Trans id="login.wallpaper.unsplash-credit">
          Photo by{" "}
          <Link
            href={wallpaper.photographerUrl}
            target="_blank"
            rel="noreferrer"
            className="text-xs font-medium text-foreground"
          >
            {name}
          </Link>{" "}
          on{" "}
          <Link
            href={wallpaper.photoUrl}
            target="_blank"
            rel="noreferrer"
            className="text-xs font-medium text-foreground"
          >
            Unsplash
          </Link>
        </Trans>
      </span>
    </p>
  );
}
