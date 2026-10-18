import { Trans } from "@lingui/react/macro";
import { ImageIcon } from "lucide-react";
import type { ReactNode } from "react";
import type { LoginAppearance, WallpaperPicture } from "@/api/raw-paths";

const link = "underline underline-offset-2 hover:text-white";

/**
 * Where the Bing or Unsplash picture on screen comes from, on a small dark
 * pill that reads on any picture. Bing's title and copyright appear when the
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

  if (background.source === "bing") {
    if (!background.bing.showCaption || !picture.title) return null;
    return (
      <Pill>
        <span className="shrink-0 font-medium">{picture.title}</span>
        {picture.copyright && (
          <>
            <span aria-hidden="true" className="text-white/60">
              ·
            </span>
            <a
              href={picture.copyrightUrl}
              target="_blank"
              rel="noreferrer"
              className={`min-w-0 truncate ${link}`}
            >
              {picture.copyright}
            </a>
          </>
        )}
      </Pill>
    );
  }

  const name = picture.photographer ?? "";
  // One run of inline text inside a single flex item: separate items would put
  // a gap between the words, and spaces into the Chinese sentence.
  return (
    <Pill>
      <span className="min-w-0 truncate">
        <Trans id="login.wallpaper.unsplash-credit">
          Photo by{" "}
          <a
            href={picture.photographerUrl}
            target="_blank"
            rel="noreferrer"
            className={`font-medium ${link}`}
          >
            {name}
          </a>{" "}
          on{" "}
          <a
            href={picture.photoUrl}
            target="_blank"
            rel="noreferrer"
            className={`font-medium ${link}`}
          >
            Unsplash
          </a>
        </Trans>
      </span>
    </Pill>
  );
}

function Pill({ children }: { children: ReactNode }) {
  return (
    <p className="inline-flex h-7 max-w-full min-w-0 items-center gap-1.5 rounded-lg bg-black/45 px-2.5 text-white/90 text-xs backdrop-blur-md">
      <ImageIcon aria-hidden="true" className="size-3.5 shrink-0" />
      {children}
    </p>
  );
}
