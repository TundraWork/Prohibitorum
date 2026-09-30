import { useQuery } from "@tanstack/react-query";
import { Outlet } from "@tanstack/react-router";
import { useSetAtom } from "jotai";
import { useLayoutEffect } from "react";
import { loginWallpaperQueryOptions } from "@/api/queries";
import { AppToolbar } from "@/components/custom/AppToolbar";
import { InstanceIdentity } from "@/components/custom/InstanceIdentity";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { publicCreditPlacement } from "@/components/custom/login-appearance/card-position";
import { LoginBackdrop } from "@/components/custom/login-appearance/LoginBackdrop";
import { cardSurfaceStyle } from "@/components/custom/login-appearance/surface-style";
import { useLoginBackground } from "@/components/custom/login-appearance/use-login-background";
import { WallpaperCredit } from "@/components/custom/login-appearance/WallpaperCredit";
import { PublicCard } from "@/components/custom/PublicCard";
import { RouteLayoutContext } from "@/components/custom/RouteFeedback";
import { publicThemeAtom } from "@/components/custom/ThemeSelect";

/**
 * The public pages' frame: the background the settings chose, the toolbar on
 * its two capsules, and the card. A Bing or Unsplash picture is read without
 * holding the page up; until it arrives, or if it cannot be read, the page
 * keeps its own background and says nothing.
 *
 * From `lg` the card sits where the settings place it and the picture's credit
 * at the bottom corner away from it — bottom-left unless the card is on the
 * left — kept narrow enough to stay clear of the card. Below `lg` the card is
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
  const hasCredit = shown.wallpaperPicture !== undefined;
  const credit = (
    <WallpaperCredit appearance={appearance} picture={shown.wallpaperPicture} />
  );

  return (
    <>
      <LoginBackdrop appearance={appearance} background={shown} />
      <AppToolbar
        surface={appearance.capsules}
        showThemeSelect={appearance.theme === "switchable"}
      >
        <InstanceIdentity />
      </AppToolbar>
      <PublicCard
        belowToolbar
        surface={cardSurfaceStyle(appearance.card)}
        position={appearance.cardPosition}
        after={
          hasCredit && (
            <div className="mt-4 flex justify-center lg:hidden">{credit}</div>
          )
        }
      >
        <RouteLayoutContext value="public">
          <Outlet />
        </RouteLayoutContext>
      </PublicCard>
      {hasCredit && (
        <div
          className={publicCreditPlacement({
            position: appearance.cardPosition,
          })}
        >
          {credit}
        </div>
      )}
    </>
  );
}
