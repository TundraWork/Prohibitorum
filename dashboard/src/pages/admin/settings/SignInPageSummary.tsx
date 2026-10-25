import { Trans, useLingui } from "@lingui/react/macro";
import { useSuspenseQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
  CameraIcon,
  ImagesIcon,
  LogInIcon,
  MountainIcon,
  SquareDashedIcon,
  SunriseIcon,
} from "lucide-react";
import { publicConfigQueryOptions } from "@/api/queries";
import type { LoginAppearance } from "@/api/raw-paths";
import { Button } from "@/components/custom/Button";
import { instanceBranding } from "@/components/custom/instance-branding";
import { gradientBackground } from "@/components/custom/login-appearance/gradients";
import { Section } from "@/components/custom/Section";
import { cn } from "@/lib/utils";

/**
 * How the sign-in page looks now, in a line or two and a small picture, with
 * the way to the page that edits it. The settings are read from `/config`, the
 * same read the public pages draw from, so nothing here waits on the editor's
 * own read.
 */
export function SignInPageSummary() {
  const { t } = useLingui();
  const { data: config } = useSuspenseQuery(publicConfigQueryOptions());
  const { loginAppearance: appearance, loginImages } = instanceBranding(config);
  const source = appearance.background.source;
  const count = loginImages.length;

  const sourceNames = {
    none: t({ id: "settings.sign-in.source.none", message: "Default" }),
    color: t({ id: "settings.sign-in.source.color", message: "Color" }),
    gradient: t({
      id: "settings.sign-in.source.gradient",
      message: "Gradient",
    }),
    bing: "Bing",
    unsplash: "Unsplash",
    pixabay: "Pixabay",
    images: t({
      id: "settings.sign-in.summary.images",
      message: `Images (${count})`,
    }),
  };
  const positions = {
    left: t({ id: "settings.sign-in.position.left", message: "Left" }),
    center: t({ id: "settings.sign-in.position.center", message: "Center" }),
    right: t({ id: "settings.sign-in.position.right", message: "Right" }),
  };

  return (
    <Section
      id="sign-in-page"
      icon={<LogInIcon />}
      title={<Trans id="settings.sign-in.title">Sign-in page</Trans>}
      description={
        <Trans id="settings.sign-in.summary.description">
          Customize the background, card and toolbar of the sign-in page.
        </Trans>
      }
      action={
        <Button
          variant="outline"
          render={<Link to="/admin/settings/sign-in-page" />}
        >
          <Trans id="settings.sign-in.edit">Edit the sign-in page</Trans>
        </Button>
      }
    >
      <div className="flex items-center gap-5">
        <Thumbnail appearance={appearance} firstImage={loginImages[0]?.url} />
        <dl className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)] gap-x-6 gap-y-1.5 text-sm">
          <dt className="text-muted-foreground">
            <Trans id="settings.sign-in.background">Background</Trans>
          </dt>
          <dd className="font-medium">{sourceNames[source]}</dd>
          <dt className="text-muted-foreground">
            <Trans id="settings.sign-in.summary.position">Card position</Trans>
          </dt>
          <dd className="font-medium">{positions[appearance.cardPosition]}</dd>
        </dl>
      </div>
    </Section>
  );
}

/**
 * A still of the page: the background where it can be drawn without fetching
 * anything, and the card where it sits. Bing and Unsplash pictures come from
 * their services, so they show the source's mark instead.
 */
function Thumbnail({
  appearance,
  firstImage,
}: {
  appearance: LoginAppearance;
  firstImage: string | undefined;
}) {
  const background = appearance.background;
  const Mark =
    background.source === "bing"
      ? SunriseIcon
      : background.source === "unsplash"
        ? CameraIcon
        : background.source === "pixabay"
          ? MountainIcon
          : background.source === "images"
            ? ImagesIcon
            : SquareDashedIcon;
  return (
    <div
      aria-hidden="true"
      data-sign-in-thumbnail=""
      className="relative aspect-[16/10] w-32 shrink-0 overflow-hidden rounded-lg border bg-sidebar"
      style={
        background.source === "color"
          ? { backgroundColor: background.color }
          : background.source === "gradient"
            ? { background: gradientBackground[background.gradient] }
            : undefined
      }
    >
      {background.source === "images" && firstImage !== undefined && (
        <img src={firstImage} alt="" className="size-full object-cover" />
      )}
      {(background.source === "none" ||
        background.source === "bing" ||
        background.source === "unsplash" ||
        background.source === "pixabay" ||
        (background.source === "images" && firstImage === undefined)) && (
        <Mark className="absolute top-1.5 left-1.5 size-3.5 text-muted-foreground" />
      )}
      <span
        className={cn(
          "absolute inset-y-2 w-9 rounded-sm bg-card shadow-xs ring-1 ring-border",
          appearance.cardPosition === "left" && "left-2",
          appearance.cardPosition === "center" && "left-1/2 -translate-x-1/2",
          appearance.cardPosition === "right" && "right-2",
        )}
      />
    </div>
  );
}
