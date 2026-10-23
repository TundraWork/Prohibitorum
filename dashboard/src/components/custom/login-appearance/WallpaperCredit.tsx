import { ImageIcon } from "lucide-react";
import type { ReactNode } from "react";
import type { LoginAppearance, WallpaperPicture } from "@/api/raw-paths";

const link = "underline underline-offset-2 hover:text-white";

/**
 * Where the picture on screen comes from, on a small dark pill that reads on
 * any picture. Bing's title and copyright appear when the administrator
 * shows the caption; the services' credit is always shown and changes with
 * the picture. Neither names a platform in words: its links lead there.
 * Every segment is drawn only when its field exists, so a source with less
 * to show (Pixabay: only the author, linked to the photo page) simply has
 * fewer segments. Draws nothing without a `picture`, which
 * `useLoginBackground` gives only for the picture services.
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

  // The photo's description and its photographer, each linked where it
  // lives. The photographer falls back to the photo page when the service
  // has no page of their own (Pixabay); either way the link is where the
  // platform is named, so the credit stays short.
  const authorHref = picture.photographerUrl ?? picture.photoUrl;
  return (
    <Pill>
      {picture.title && picture.photoUrl && (
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
      {picture.photographer && authorHref && (
        <a
          href={authorHref}
          target="_blank"
          rel="noreferrer"
          className={`shrink-0 ${picture.title ? "" : "font-medium "}${link}`}
        >
          {picture.photographer}
        </a>
      )}
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
