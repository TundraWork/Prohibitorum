import { Trans } from "@lingui/react/macro";
import { Palette } from "lucide-react";
import { type ReactNode, useId } from "react";
import {
  useAppearanceChoice,
  useResolvedAppearance,
} from "@/app/use-appearance";
import { ChoiceTile } from "@/components/custom/ChoiceTile";
import { Section } from "@/components/custom/Section";
import {
  useColorSchemeNames,
  useThemeOptions,
} from "@/components/custom/ThemeSelect";
import { RadioGroup } from "@/components/ui/radio-group";
import { Spinner } from "@/components/ui/spinner";
import { isColorMode } from "@/lib/appearance";
import {
  type ColorSchemeSlug,
  colorSchemes,
  isColorSchemeSlug,
} from "@/lib/color-scheme";
import { cn } from "@/lib/utils";

/**
 * Light or dark, then the colour scheme, each a row of tiles. A scheme's tile
 * shows it in both modes side by side, so it can be judged without switching.
 * The choice is saved to the account, so it follows the reader to every
 * device, and applies at once. The chosen tile carries the wait while it
 * saves, and the next choice can be made meanwhile.
 */
export function AppearancePanel() {
  const { mode, choose, saving } = useAppearanceChoice();
  const { scheme } = useResolvedAppearance().applied;
  const names = useColorSchemeNames();
  const modeLabel = useId();
  const schemeLabel = useId();
  return (
    <Section
      icon={<Palette />}
      title={<Trans id="profile.appearance.title">Appearance</Trans>}
      description={
        <Trans id="profile.appearance.description">
          Choose the theme you like. The setting syncs across your devices.
        </Trans>
      }
    >
      <div className="flex flex-col gap-5">
        <div className="flex flex-col gap-2">
          <span id={modeLabel} className="font-medium text-sm">
            <Trans id="theme.mode">Light or dark</Trans>
          </span>
          <RadioGroup
            aria-labelledby={modeLabel}
            aria-busy={saving === "mode" || undefined}
            value={mode ?? "system"}
            onValueChange={(next) => {
              if (isColorMode(next)) choose({ mode: next });
            }}
            className="grid grid-cols-3 gap-2"
          >
            {useThemeOptions().map(({ value, label, icon: Icon }) => (
              <ChoiceTile
                key={value}
                value={value}
                layout="compact"
                media={
                  <Saving
                    shown={saving === "mode" && value === (mode ?? "system")}
                  >
                    <Icon className="size-4.5" aria-hidden="true" />
                  </Saving>
                }
                label={label}
              />
            ))}
          </RadioGroup>
        </div>
        <div className="flex flex-col gap-2">
          <span id={schemeLabel} className="font-medium text-sm">
            <Trans id="theme.scheme">Color scheme</Trans>
          </span>
          <RadioGroup
            aria-labelledby={schemeLabel}
            aria-busy={saving === "scheme" || undefined}
            value={scheme}
            onValueChange={(next) => {
              if (isColorSchemeSlug(next)) choose({ scheme: next });
            }}
            className="grid grid-cols-[repeat(auto-fill,minmax(7.5rem,1fr))] gap-2"
          >
            {colorSchemes.map(({ slug }) => (
              <ChoiceTile
                key={slug}
                value={slug}
                layout="media"
                media={
                  <Saving
                    shown={saving === "scheme" && slug === scheme}
                    className="w-full"
                  >
                    <SchemePreview scheme={slug} />
                  </Saving>
                }
                label={names[slug]}
              />
            ))}
          </RadioGroup>
        </div>
      </div>
    </Section>
  );
}

/**
 * A tile's picture, with the spinner in its corner while the choice saves, as
 * the avatar gallery marks the picture it is saving.
 */
function Saving({
  shown,
  className,
  children,
}: {
  shown: boolean;
  className?: string;
  children: ReactNode;
}) {
  return (
    <span className={cn("relative flex", className)}>
      {children}
      {shown && (
        <span className="absolute -end-1.5 -bottom-1.5 z-20 rounded-full bg-background p-0.5 shadow-xs ring-1 ring-border">
          <Spinner className="size-3" aria-hidden="true" data-slot="spinner" />
        </span>
      )}
    </span>
  );
}

/**
 * A scheme in miniature, light on the left and dark on the right: each half is
 * the page's surface with a line of text and a button in the accent.
 */
export function SchemePreview({ scheme }: { scheme: ColorSchemeSlug }) {
  const { swatch } = colorSchemes.find((s) => s.slug === scheme) ?? {
    swatch: colorSchemes[0].swatch,
  };
  return (
    <span
      aria-hidden="true"
      data-scheme-preview={scheme}
      className="flex aspect-[4/3] w-full overflow-hidden rounded-md ring-1 ring-black/10 ring-inset dark:ring-white/16"
    >
      {(["light", "dark"] as const).map((mode) => (
        <span
          key={mode}
          className="flex flex-1 flex-col justify-end gap-1.5 p-2"
          style={{ background: swatch[mode].surface }}
        >
          <span
            className="h-1 w-4/5 rounded-full"
            style={{
              background: `color-mix(in srgb, ${swatch[mode].accent} 35%, transparent)`,
            }}
          />
          <span
            className="h-2.5 w-3/5 rounded-full"
            style={{ background: swatch[mode].accent }}
          />
        </span>
      ))}
    </span>
  );
}
