import { Link } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import type { LoginAppearance, WallpaperPicture } from "@/api/raw-paths";
import { capsuleSurfaceStyle } from "@/components/custom/login-appearance/surface-style";

/**
 * Where the Bing or Unsplash picture on screen comes from, on a small capsule
 * drawn like the toolbar's. Bing's title and copyright appear when the
 * administrator shows the caption; the Unsplash credit is always shown and
 * changes with the photo, as Unsplash asks. Draws nothing without a `picture`,
 * which `useLoginBackground` gives only for those two sources.
 */
export function WallpaperCredit({
  appearance,
  picture,
}: {
  appearance: LoginAppearance;
  picture?: WallpaperPicture;
}) {
  const background = appearance.background;
  if (picture === undefined) return null;
  const surface = capsuleSurfaceStyle(appearance.capsules);
  const capsule = `min-h-8 max-w-full rounded-full px-3.5 py-1.5 text-xs shadow-surface ${surface.className}`;

  if (background.source === "bing") {
    if (!background.bing.showCaption || !picture.title) return null;
    return (
      <p
        className={`inline-flex min-w-0 items-center gap-1.5 ${capsule}`}
        style={surface.style}
      >
        <span className="shrink-0 font-medium text-foreground">
          {picture.title}
        </span>
        {picture.copyright && (
          <>
            <span aria-hidden="true" className="text-muted">
              ·
            </span>
            <Link
              href={picture.copyrightUrl}
              target="_blank"
              rel="noreferrer"
              className="block min-w-0 truncate text-xs text-muted"
            >
              {picture.copyright}
            </Link>
          </>
        )}
      </p>
    );
  }

  const name = picture.photographer ?? "";
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
            href={picture.photographerUrl}
            target="_blank"
            rel="noreferrer"
            className="text-xs font-medium text-foreground"
          >
            {name}
          </Link>{" "}
          on{" "}
          <Link
            href={picture.photoUrl}
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
