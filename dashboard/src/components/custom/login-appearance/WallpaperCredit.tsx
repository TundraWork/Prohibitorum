import { ImageIcon } from "lucide-react";
import type { ReactNode } from "react";
import type { LoginAppearance, WallpaperPicture } from "@/api/raw-paths";

const link = "underline underline-offset-2 hover:text-white";

/**
 * Where the Bing or Unsplash picture on screen comes from, on a small dark
 * pill that reads on any picture. Bing's title and copyright appear when the
 * administrator shows the caption; the Unsplash credit, the photo's
 * description and its photographer, is always shown and changes with the
 * photo, as Unsplash asks. Neither names the platform in words: its links
 * lead there. Draws nothing without a `picture`,
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

  // The photo's description and its photographer, linked to the photo and to
  // the photographer's page; the links are where Unsplash is named, so the
  // credit stays short.
  return (
    <Pill>
      {picture.title && (
        <>
          <a
            href={picture.photoUrl}
            target="_blank"
            rel="noreferrer"
            className={`min-w-0 truncate font-medium ${link}`}
          >
            {picture.title}
          </a>
          <span aria-hidden="true" className="text-white/60">
            ·
          </span>
        </>
      )}
      <a
        href={picture.photographerUrl}
        target="_blank"
        rel="noreferrer"
        className={`shrink-0 ${picture.title ? "" : "font-medium "}${link}`}
      >
        {picture.photographer}
      </a>
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
