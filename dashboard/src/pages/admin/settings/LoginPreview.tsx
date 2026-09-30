import {
  Avatar,
  buttonVariants,
  Card,
  Input,
  Label,
  Skeleton,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
} from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useQuery } from "@tanstack/react-query";
import { Languages, Moon, Sun } from "lucide-react";
import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import { loginWallpaperPreviewQueryOptions } from "@/api/queries";
import type { WallpaperPreviewQuery } from "@/api/raw-admin-paths";
import type { LoginAppearance } from "@/api/raw-paths";
import { Button } from "@/components/custom/Button";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import {
  previewCardPlacement,
  previewCreditPlacement,
} from "@/components/custom/login-appearance/card-position";
import { LoginBackdrop } from "@/components/custom/login-appearance/LoginBackdrop";
import {
  capsuleSurfaceStyle,
  cardSurfaceStyle,
} from "@/components/custom/login-appearance/surface-style";
import { WallpaperCredit } from "@/components/custom/login-appearance/WallpaperCredit";

/** The sign-in page is laid out at this size, then scaled to the preview's width. */
const pageWidth = 1280;
const pageHeight = 800;

type Theme = "light" | "dark";

const toolbarIcon = buttonVariants({
  variant: "ghost",
  size: "sm",
  isIconOnly: true,
});

function subscribeTheme(onChange: () => void) {
  const observer = new MutationObserver(onChange);
  observer.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["data-theme"],
  });
  return () => observer.disconnect();
}

/** The console's resolved theme, as `AppEnvironment` writes it on `<html>`. */
function useConsoleTheme(): Theme {
  return useSyncExternalStore(
    subscribeTheme,
    () =>
      document.documentElement.dataset.theme === "dark" ? "dark" : "light",
    () => "light",
  );
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
 * The sign-in page itself at a fraction of its size: laid out at 1280×800 and
 * scaled to the preview's width, so the capsules, the card and the credit keep
 * their real proportions, and the card sits where the draft places it on a
 * wide window. It draws the form's draft, so every change shows at once. It
 * follows the console's theme until the administrator picks one; a draft that
 * forces a theme shows that one and hides the choice, which comes back as it
 * was when the draft leaves the theme to visitors.
 *
 * A Bing or Unsplash picture is read for the draft's region or keyword a moment
 * after they stop changing; while it loads the background shimmers, and when
 * it cannot be read the caption says so in place of "Preview".
 */
export function LoginPreview({
  appearance,
  images,
  hasUnsplashKey,
}: {
  appearance: LoginAppearance;
  images: string[];
  hasUnsplashKey: boolean;
}) {
  const { t } = useLingui();
  const branding = useInstanceBranding();
  const consoleTheme = useConsoleTheme();
  const [chosenTheme, setChosenTheme] = useState<Theme | undefined>(undefined);
  const forcedTheme =
    appearance.theme === "switchable" ? undefined : appearance.theme;
  const theme = forcedTheme ?? chosenTheme ?? consoleTheme;

  const frame = useRef<HTMLDivElement>(null);
  const [scale, setScale] = useState(0.3);
  useLayoutEffect(() => {
    const element = frame.current;
    if (!element) return;
    const observer = new ResizeObserver(([entry]) => {
      if (entry) setScale(entry.contentRect.width / pageWidth);
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

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
  const wallpaper = useQuery({
    ...loginWallpaperPreviewQueryOptions(
      params ?? { source: "bing", market: "zh-CN" },
    ),
    enabled:
      params !== undefined && params.source === background.source && !needsKey,
  });
  const shownWallpaper =
    params?.source === background.source ? wallpaper.data : undefined;
  const loading =
    (background.source === "bing" || background.source === "unsplash") &&
    !needsKey &&
    !wallpaper.isError &&
    shownWallpaper === undefined;

  const note = needsKey
    ? t({
        id: "settings.sign-in.preview.unsplash_key",
        message: "Save an access key to preview Unsplash photos.",
      })
    : wallpaper.isError && params?.source === background.source
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

  const card = cardSurfaceStyle(appearance.card);
  const capsule = capsuleSurfaceStyle(appearance.capsules);
  const capsuleClass = `flex h-10 items-center rounded-full shadow-surface [--field-radius:100%] [--radius:100%] ${capsule.className}`;

  return (
    <figure className="flex flex-col gap-2">
      <div
        ref={frame}
        data-theme={theme}
        data-login-preview=""
        className={`${theme} relative aspect-[16/10] w-full overflow-hidden rounded-[0.375rem] bg-background shadow-[0_0_0_1px_var(--separator)]`}
      >
        {loading && <Skeleton className="absolute inset-0 rounded-none" />}
        <div
          inert
          aria-hidden="true"
          className="absolute top-0 left-0 flex origin-top-left flex-col bg-transparent text-foreground"
          style={{
            width: pageWidth,
            height: pageHeight,
            transform: `scale(${scale})`,
          }}
        >
          <LoginBackdrop
            contained
            appearance={appearance}
            images={images}
            wallpaper={shownWallpaper}
          />
          <header className="relative flex h-16 items-center justify-between px-6">
            <div
              className={`${capsuleClass} gap-3 pr-4 pl-1.5`}
              style={capsule.style}
            >
              <Avatar size="sm">
                <Avatar.Image src={branding.iconUrl} alt="" />
                <Avatar.Fallback>{branding.name.slice(0, 1)}</Avatar.Fallback>
              </Avatar>
              <span className="text-sm font-semibold">{branding.name}</span>
            </div>
            <div className={`${capsuleClass} gap-1 px-1`} style={capsule.style}>
              {/* The preview is inert, so its controls are drawn, not buttons. */}
              <span className={toolbarIcon}>
                <Languages size={18} aria-hidden="true" />
              </span>
              {forcedTheme === undefined && (
                <span className={toolbarIcon}>
                  <Sun size={18} aria-hidden="true" />
                </span>
              )}
            </div>
          </header>
          <main
            className={`relative flex w-[30rem] flex-1 flex-col justify-center px-4 pb-16 ${previewCardPlacement({ position: appearance.cardPosition })}`}
          >
            <Card className={`w-full p-8 ${card.className}`} style={card.style}>
              <Card.Content className="flex flex-col gap-4">
                <p className="text-xl font-semibold">
                  <Trans id="settings.sign-in.preview.sign_in">Sign in</Trans>
                </p>
                <Button fullWidth>
                  <Trans id="settings.sign-in.preview.passkey">
                    Sign in with a passkey
                  </Trans>
                </Button>
                <TextField>
                  <Label>
                    <Trans id="settings.sign-in.preview.username">
                      Username
                    </Trans>
                  </Label>
                  <Input variant="secondary" />
                </TextField>
                <TextField>
                  <Label>
                    <Trans id="settings.sign-in.preview.password">
                      Password
                    </Trans>
                  </Label>
                  <Input variant="secondary" type="password" />
                </TextField>
                <Button fullWidth variant="secondary">
                  <Trans id="settings.sign-in.preview.continue">
                    Continue with password
                  </Trans>
                </Button>
              </Card.Content>
            </Card>
          </main>
          <footer className="relative flex px-6 pb-5">
            <div
              className={previewCreditPlacement({
                position: appearance.cardPosition,
              })}
            >
              <WallpaperCredit
                appearance={appearance}
                wallpaper={shownWallpaper}
              />
            </div>
          </footer>
        </div>
      </div>
      <figcaption className="flex items-center justify-between gap-3">
        <span className="text-xs text-muted">
          {note ?? <Trans id="settings.sign-in.preview">Preview</Trans>}
        </span>
        {forcedTheme === undefined && (
          <ToggleButtonGroup
            size="sm"
            selectionMode="single"
            disallowEmptySelection
            selectedKeys={[theme]}
            onSelectionChange={(keys) => {
              const [key] = [...keys];
              if (key === "light" || key === "dark") setChosenTheme(key);
            }}
          >
            <ToggleButton
              id="light"
              isIconOnly
              aria-label={t({
                id: "settings.sign-in.preview.light",
                message: "Light",
              })}
            >
              <Sun size={14} aria-hidden="true" />
            </ToggleButton>
            <ToggleButton
              id="dark"
              isIconOnly
              aria-label={t({
                id: "settings.sign-in.preview.dark",
                message: "Dark",
              })}
            >
              <Moon size={14} aria-hidden="true" />
            </ToggleButton>
          </ToggleButtonGroup>
        )}
      </figcaption>
    </figure>
  );
}
