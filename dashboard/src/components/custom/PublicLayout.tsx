import { useQuery } from "@tanstack/react-query";
import { Outlet } from "@tanstack/react-router";
import { useSetAtom } from "jotai";
import { useLayoutEffect } from "react";
import { loginWallpaperQueryOptions } from "@/api/queries";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { useLoginBackground } from "@/components/custom/login-appearance/use-login-background";
import { PublicScene } from "@/components/custom/PublicScene";
import { RouteLayoutContext } from "@/components/custom/RouteFeedback";
import { publicThemeAtom } from "@/components/custom/ThemeSelect";

/**
 * The public pages' frame: the background the settings chose, the toolbar on
 * its two capsules, and the stage the page sits on. A Bing or Unsplash picture is read without
 * holding the page up; until it arrives, or if it cannot be read, the page
 * keeps its own background and says nothing.
 *
 * From `lg` the stage sits where the settings place it and the picture's credit
 * at the bottom corner away from it — bottom-left unless the stage is on the
 * left — kept narrow enough to stay clear of it. Below `lg` the stage is
 * centred and the credit sits under it.
 *
 * A theme the settings force is set before the first paint and cleared when
 * the layout goes, so the console never shows it; the toolbar then has no
 * theme control.
 */
export function PublicLayout() {
  const { loginAppearance: appearance, loginImages } = useInstanceBranding();
  const background = appearance.background;
  const usesWallpaper =
    background.source === "bing" || background.source === "unsplash";
  const setPublicTheme = useSetAtom(publicThemeAtom);
  useLayoutEffect(() => {
    setPublicTheme(
      appearance.theme === "switchable" ? undefined : appearance.theme,
    );
    return () => setPublicTheme(undefined);
  }, [appearance.theme, setPublicTheme]);
  const { data: wallpaper } = useQuery({
    ...loginWallpaperQueryOptions(background),
    enabled: usesWallpaper,
  });
  const shown = useLoginBackground(
    appearance,
    loginImages.map((image) => image.url),
    usesWallpaper ? wallpaper : undefined,
  );
  return (
    <PublicScene appearance={appearance} background={shown}>
      <RouteLayoutContext value="public">
        <Outlet />
      </RouteLayoutContext>
    </PublicScene>
  );
}
