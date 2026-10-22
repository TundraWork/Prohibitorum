import { Trans, useLingui } from "@lingui/react/macro";
import { MonitorIcon, MoonIcon, PaletteIcon, SunIcon } from "lucide-react";
import { Fragment } from "react";
import {
  useAppearanceChoice,
  useResolvedAppearance,
} from "@/app/use-appearance";
import { Button } from "@/components/ui/button";
import {
  Menu,
  MenuGroup,
  MenuGroupLabel,
  MenuPopup,
  MenuRadioGroup,
  MenuRadioItem,
  MenuSeparator,
  MenuTrigger,
} from "@/components/ui/menu";
import { isColorMode } from "@/lib/appearance";
import {
  type ColorSchemeSlug,
  colorSchemes,
  isColorSchemeSlug,
} from "@/lib/color-scheme";
import { cn } from "@/lib/utils";

export function useThemeOptions() {
  const { t } = useLingui();
  return [
    {
      value: "light",
      label: t({ id: "theme.light", message: "Light theme" }),
      icon: SunIcon,
    },
    {
      value: "dark",
      label: t({ id: "theme.dark", message: "Dark theme" }),
      icon: MoonIcon,
    },
    {
      value: "system",
      label: t({ id: "theme.system", message: "System theme" }),
      icon: MonitorIcon,
    },
  ] as const;
}

/** Each colour scheme's name, in the reader's language. */
export function useColorSchemeNames(): Record<ColorSchemeSlug, string> {
  const { t } = useLingui();
  return {
    graphite: t({ id: "theme.scheme.graphite", message: "Graphite" }),
    nord: t({ id: "theme.scheme.nord", message: "Nord" }),
    sand: t({ id: "theme.scheme.sand", message: "Sand" }),
    matcha: t({ id: "theme.scheme.matcha", message: "Matcha" }),
    sakura: t({ id: "theme.scheme.sakura", message: "Sakura" }),
    lavender: t({ id: "theme.scheme.lavender", message: "Lavender" }),
    lagoon: t({ id: "theme.scheme.lagoon", message: "Lagoon" }),
    citrus: t({ id: "theme.scheme.citrus", message: "Citrus" }),
  };
}

/**
 * A scheme's page surface and accent as the two halves of a dot, in the mode
 * the page shows. It only illustrates the name beside it.
 */
export function SchemeSwatch({
  scheme,
  className,
}: {
  scheme: ColorSchemeSlug;
  className?: string;
}) {
  const { swatch } = colorSchemes.find((s) => s.slug === scheme) ?? {
    swatch: colorSchemes[0].swatch,
  };
  const dot = (mode: "light" | "dark") => (
    <span
      aria-hidden="true"
      className={cn(
        "size-4 shrink-0 rounded-full ring-1 ring-black/12 ring-inset dark:ring-white/16",
        mode === "light"
          ? "inline-block dark:hidden"
          : "hidden dark:inline-block",
        className,
      )}
      style={{
        background: `linear-gradient(90deg, ${swatch[mode].surface} 50%, ${swatch[mode].accent} 50%)`,
      }}
    />
  );
  return (
    <>
      {dot("light")}
      {dot("dark")}
    </>
  );
}

/**
 * The theme choice for any menu that offers it: light or dark, then the colour
 * scheme, the ordinary schemes before the youthful ones. `modes` is false
 * where the sign-in page's settings fix light or dark, and only the scheme is
 * left to choose.
 */
export function ThemeMenuItems({ modes = true }: { modes?: boolean }) {
  const { mode, choose } = useAppearanceChoice();
  const { scheme } = useResolvedAppearance().applied;
  const names = useColorSchemeNames();
  const modeOptions = useThemeOptions();
  return (
    <>
      {modes && (
        <>
          <MenuGroup>
            <MenuGroupLabel>
              <Trans id="theme.mode">Light or dark</Trans>
            </MenuGroupLabel>
            <MenuRadioGroup
              value={mode ?? "system"}
              onValueChange={(value) => {
                if (isColorMode(value)) choose({ mode: value });
              }}
            >
              {modeOptions.map(({ value, label }) => (
                <MenuRadioItem key={value} value={value}>
                  {label}
                </MenuRadioItem>
              ))}
            </MenuRadioGroup>
          </MenuGroup>
          <MenuSeparator />
        </>
      )}
      <MenuGroup>
        <MenuGroupLabel>
          <Trans id="theme.scheme">Color scheme</Trans>
        </MenuGroupLabel>
        <MenuRadioGroup
          value={scheme}
          onValueChange={(value) => {
            if (isColorSchemeSlug(value)) choose({ scheme: value });
          }}
        >
          {colorSchemes.map(({ slug, kind }, index) => (
            <Fragment key={slug}>
              {index > 0 && kind !== colorSchemes[index - 1]?.kind && (
                <MenuSeparator />
              )}
              <MenuRadioItem value={slug}>
                <span className="flex items-center gap-2">
                  <SchemeSwatch scheme={slug} />
                  {names[slug]}
                </span>
              </MenuRadioItem>
            </Fragment>
          ))}
        </MenuRadioGroup>
      </MenuGroup>
    </>
  );
}

/**
 * The public toolbar's theme button, named in words beside the language
 * button so a visitor finds it.
 */
export function ThemeMenu({ modes = true }: { modes?: boolean }) {
  return (
    <Menu>
      <MenuTrigger render={<Button size="sm" variant="ghost" />}>
        <PaletteIcon aria-hidden="true" />
        <Trans id="theme.label">Theme</Trans>
      </MenuTrigger>
      <MenuPopup align="end">
        <ThemeMenuItems modes={modes} />
      </MenuPopup>
    </Menu>
  );
}
