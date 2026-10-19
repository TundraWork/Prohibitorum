import type { ReactNode } from "react";
import type { LoginAppearance } from "@/api/raw-paths";
import { AppToolbar } from "@/components/custom/AppToolbar";
import { InstanceIdentity } from "@/components/custom/InstanceIdentity";
import { publicCreditPlacement } from "@/components/custom/login-appearance/card-position";
import { LoginBackdrop } from "@/components/custom/login-appearance/LoginBackdrop";
import { cardSurfaceStyle } from "@/components/custom/login-appearance/surface-style";
import type { LoginBackground } from "@/components/custom/login-appearance/use-login-background";
import { WallpaperCredit } from "@/components/custom/login-appearance/WallpaperCredit";
import { PublicStage } from "@/components/custom/PublicStage";

/**
 * The public pages' look from the appearance the settings chose: the
 * background, the toolbar on its two capsules, the stage the page sits on and
 * the picture's credit. `PublicLayout` draws a route in it and the sign-in
 * page editor's preview draws a sample page in it, so the two cannot drift.
 *
 * From `lg` the stage sits where the settings place it and the credit at the
 * bottom corner away from it, kept narrow enough to stay clear of it. Below
 * `lg` the stage is centred and the credit sits under it.
 */
export function PublicScene({
  appearance,
  background: shown,
  readablePictures = false,
  children,
}: {
  appearance: LoginAppearance;
  /** The pictures to show, as `useLoginBackground` chose them. */
  background: LoginBackground;
  /** Fetch the pictures so their pixels can be read; see `LoginBackdrop`. */
  readablePictures?: boolean;
  children: ReactNode;
}) {
  const hasCredit = shown.wallpaperPicture !== undefined;
  const credit = (
    <WallpaperCredit appearance={appearance} picture={shown.wallpaperPicture} />
  );

  return (
    <>
      <LoginBackdrop
        appearance={appearance}
        background={shown}
        readable={readablePictures}
      />
      <AppToolbar
        surface={appearance.capsules}
        showThemeSelect={appearance.theme === "switchable"}
      >
        <InstanceIdentity />
      </AppToolbar>
      <PublicStage
        frame={appearance.background.source === "none" ? "plain" : "frosted"}
        card={cardSurfaceStyle(appearance.card)}
        position={appearance.cardPosition}
        after={
          hasCredit && (
            <div className="flex w-full min-w-0 justify-center lg:hidden">
              {credit}
            </div>
          )
        }
      >
        {children}
      </PublicStage>
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
