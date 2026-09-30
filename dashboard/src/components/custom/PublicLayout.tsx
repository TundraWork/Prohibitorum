import { useQuery } from "@tanstack/react-query";
import { Outlet } from "@tanstack/react-router";
import { loginWallpaperQueryOptions } from "@/api/queries";
import { AppToolbar } from "@/components/custom/AppToolbar";
import { InstanceIdentity } from "@/components/custom/InstanceIdentity";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { LoginBackdrop } from "@/components/custom/login-appearance/LoginBackdrop";
import { surfaceStyle } from "@/components/custom/login-appearance/surface-style";
import { WallpaperCredit } from "@/components/custom/login-appearance/WallpaperCredit";
import { PublicCard } from "@/components/custom/PublicCard";
import { RouteLayoutContext } from "@/components/custom/RouteFeedback";

/**
 * The public pages' frame: the background the settings chose, the toolbar on
 * its two capsules, and the card. A Bing or Unsplash picture is read without
 * holding the page up; until it arrives, or if it cannot be read, the page
 * keeps its own background and says nothing.
 *
 * The picture's credit sits at the window's bottom-left from `lg`, kept narrow
 * enough to stay clear of the centred card, and under the card below that.
 */
export function PublicLayout() {
  const { loginAppearance: appearance, loginImages } = useInstanceBranding();
  const background = appearance.background;
  const usesWallpaper =
    background.source === "bing" || background.source === "unsplash";
  const { data: wallpaper } = useQuery({
    ...loginWallpaperQueryOptions(background),
    enabled: usesWallpaper,
  });
  const shownWallpaper = usesWallpaper ? wallpaper : undefined;
  const credit = (
    <WallpaperCredit appearance={appearance} wallpaper={shownWallpaper} />
  );

  return (
    <>
      <LoginBackdrop
        appearance={appearance}
        images={loginImages.map((image) => image.url)}
        wallpaper={shownWallpaper}
      />
      <AppToolbar surface={appearance.capsules}>
        <InstanceIdentity />
      </AppToolbar>
      <PublicCard
        belowToolbar
        surface={surfaceStyle(appearance.card)}
        after={
          shownWallpaper !== undefined && (
            <div className="mt-4 flex justify-center lg:hidden">{credit}</div>
          )
        }
      >
        <RouteLayoutContext value="public">
          <Outlet />
        </RouteLayoutContext>
      </PublicCard>
      {shownWallpaper !== undefined && (
        <div className="fixed bottom-5 left-6 hidden max-w-[calc(50vw-17rem)] lg:flex">
          {credit}
        </div>
      )}
    </>
  );
}
