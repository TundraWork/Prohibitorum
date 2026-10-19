import { useLingui } from "@lingui/react/macro";
import { useQuery } from "@tanstack/react-query";
import { CatchBoundary } from "@tanstack/react-router";
import { Suspense, useEffect, useState, useSyncExternalStore } from "react";
import { loginWallpaperPreviewQueryOptions } from "@/api/queries";
import type { WallpaperPreviewQuery } from "@/api/raw-admin-paths";
import type { LoginAppearance } from "@/api/raw-paths";
import { useLoginBackground } from "@/components/custom/login-appearance/use-login-background";
import { LoginPreviewContext } from "@/components/custom/login-preview";
import { PublicScene } from "@/components/custom/PublicScene";
import { Skeleton } from "@/components/ui/skeleton";
import {
  PreviewDocument,
  type PreviewTheme,
} from "@/pages/admin/settings/sign-in-page/PreviewDocument";
import { PasswordPage } from "@/pages/Login";

export type PreviewSize = "desktop" | "phone";

/** What each size is laid out at before it is scaled to the preview's box. */
const pageSizes: Record<PreviewSize, { width: number; height: number }> = {
  desktop: { width: 1280, height: 800 },
  phone: { width: 390, height: 844 },
};

function subscribeTheme(onChange: () => void) {
  const observer = new MutationObserver(onChange);
  observer.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["data-theme"],
  });
  return () => observer.disconnect();
}

/** The console's resolved theme, as `AppEnvironment` writes it on `<html>`. */
function useConsoleTheme(): PreviewTheme {
  return useSyncExternalStore(
    subscribeTheme,
    () =>
      document.documentElement.dataset.theme === "dark" ? "dark" : "light",
    () => "light",
  );
}

/**
 * The theme the preview shows: the one the draft forces, else the one the
 * administrator picked in the preview, else the console's own. `forced` says
 * the draft settles it, so the preview offers no choice.
 */
export function usePreviewTheme(appearance: LoginAppearance) {
  const consoleTheme = useConsoleTheme();
  const [chosen, choose] = useState<PreviewTheme | undefined>(undefined);
  const forced =
    appearance.theme === "switchable" ? undefined : appearance.theme;
  return {
    theme: forced ?? chosen ?? consoleTheme,
    forced: forced !== undefined,
    choose,
  };
}

function useDebouncedValue<T>(value: T, delayMs: number): T {
  const [settled, setSettled] = useState(value);
  const key = JSON.stringify(value);
  // biome-ignore lint/correctness/useExhaustiveDependencies: the key stands for the value.
  useEffect(() => {
    const timer = window.setTimeout(() => setSettled(value), delayMs);
    return () => window.clearTimeout(timer);
  }, [key, delayMs]);
  return settled;
}

/**
 * The sign-in page itself at the draft's settings, in a frame the size of a
 * desktop window or a phone. It draws the same scene the public pages do and,
 * on it, the real first step of signing in with the instance's own providers,
 * so every change shows at once and a position, size or theme means what it
 * will mean for visitors. The frame is inert, so nothing in it can be used.
 *
 * A Bing or Unsplash picture is read for the draft's region or keyword a moment
 * after they stop changing; while it loads the preview shimmers, and when it
 * cannot be read a note under the preview says so.
 */
export function SignInPreview({
  appearance,
  images,
  hasUnsplashKey,
  theme,
  size,
  onDocument,
}: {
  appearance: LoginAppearance;
  images: string[];
  hasUnsplashKey: boolean;
  theme: PreviewTheme;
  size: PreviewSize;
  /** Told the preview's document once it is ready, for measuring what it draws. */
  onDocument?: (document: Document | null) => void;
}) {
  const { t } = useLingui();
  const [drawFailed, setDrawFailed] = useState(false);
  const background = appearance.background;
  const params = useDebouncedValue<WallpaperPreviewQuery | undefined>(
    background.source === "bing"
      ? { source: "bing", market: background.bing.market }
      : background.source === "unsplash"
        ? { source: "unsplash", query: background.unsplash.query }
        : undefined,
    400,
  );
  const needsKey = background.source === "unsplash" && !hasUnsplashKey;
  const current = params?.source === background.source;
  const wallpaper = useQuery({
    ...loginWallpaperPreviewQueryOptions(
      params ?? { source: "bing", market: "zh-CN" },
    ),
    enabled: params !== undefined && current && !needsKey,
  });
  const shown = useLoginBackground(
    appearance,
    images,
    current ? wallpaper.data : undefined,
  );
  const loading =
    (background.source === "bing" || background.source === "unsplash") &&
    !needsKey &&
    !wallpaper.isError &&
    shown.wallpaperPicture === undefined;

  const note = drawFailed
    ? t({
        id: "settings.sign-in.preview.failed",
        message: "The sign-in page could not be drawn here.",
      })
    : needsKey
      ? t({
          id: "settings.sign-in.preview.unsplash_key",
          message: "Save an access key to preview Unsplash photos.",
        })
      : wallpaper.isError && current
        ? background.source === "bing"
          ? t({
              id: "settings.sign-in.preview.bing_failed",
              message: "Couldn't get the picture from Bing.",
            })
          : t({
              id: "settings.sign-in.preview.unsplash_failed",
              message: "Couldn't get a photo from Unsplash.",
            })
        : undefined;

  return (
    <div className="flex flex-col">
      <div className="relative">
        <PreviewDocument
          {...pageSizes[size]}
          theme={theme}
          onDocument={onDocument}
          title={t({
            id: "settings.sign-in.preview.frame",
            message: "Sign-in page preview",
          })}
        >
          {/* The step reads the instance's public data, and a read that fails
              throws; it must not take the editor down with it. The failure is
              said in the note under the frame, which a reader can reach, and
              without the page there is no card to measure. */}
          <CatchBoundary
            getResetKey={() => "preview"}
            onCatch={() => setDrawFailed(true)}
            errorComponent={() => null}
          >
            <LoginPreviewContext value={true}>
              <PublicScene
                appearance={appearance}
                background={shown}
                readablePictures
              >
                <Suspense fallback={null}>
                  <PasswordPage />
                </Suspense>
              </PublicScene>
            </LoginPreviewContext>
          </CatchBoundary>
        </PreviewDocument>
        {loading && (
          <Skeleton
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 rounded-none opacity-60"
          />
        )}
      </div>
      {note !== undefined && (
        <p role="status" className="px-5 py-3 text-muted-foreground text-sm">
          {note}
        </p>
      )}
    </div>
  );
}
