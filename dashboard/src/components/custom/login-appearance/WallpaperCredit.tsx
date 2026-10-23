import { useLingui } from "@lingui/react/macro";
import { ImageIcon } from "lucide-react";
import { Fragment, type ReactNode } from "react";
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
  const { t } = useLingui();
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

  // Openverse: the CC licence asks for the work's title (linked to its page),
  // its author, and the licence itself, each linked where it lives. The
  // trailing "Openverse" segment is the one place a platform is named in
  // words: Openverse's terms require a visible statement that the site uses
  // it without endorsement, and a link alone does not carry that.
  if (background.source === "openverse") {
    const segments = [
      picture.title && picture.photoUrl
        ? {
            key: "title",
            node: (
              <a
                href={picture.photoUrl}
                target="_blank"
                rel="noreferrer"
                className={`min-w-0 truncate font-medium ${link}`}
              >
                {picture.title}
              </a>
            ),
          }
        : undefined,
      picture.photographer
        ? {
            key: "author",
            node: (
              <a
                href={picture.photographerUrl ?? picture.photoUrl}
                target="_blank"
                rel="noreferrer"
                className={`min-w-0 truncate ${picture.title ? "" : "font-medium "}${link}`}
              >
                {picture.photographer}
              </a>
            ),
          }
        : undefined,
      picture.license && picture.licenseUrl
        ? {
            key: "license",
            node: (
              <a
                href={picture.licenseUrl}
                target="_blank"
                rel="noreferrer"
                className={`shrink-0 ${link}`}
              >
                {picture.license}
              </a>
            ),
          }
        : undefined,
    ].filter((segment) => segment !== undefined);
    return (
      <Pill>
        {segments.map((segment, i) => (
          <Fragment key={segment.key}>
            {i > 0 && (
              <span aria-hidden="true" className="text-white/60">
                ·
              </span>
            )}
            {segment.node}
          </Fragment>
        ))}
        <span aria-hidden="true" className="text-white/60">
          ·
        </span>
        <a
          href="https://openverse.org/"
          target="_blank"
          rel="noreferrer"
          title={t({
            id: "settings.sign-in.openverse.statement",
            message:
              "This site uses Openverse but is not endorsed or certified by Openverse.",
          })}
          className={`shrink-0 ${link}`}
        >
          Openverse
        </a>
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
