import { useLingui } from "@lingui/react/macro";
import { MonitorIcon, MoonIcon, SunIcon } from "lucide-react";
import { useAppearanceChoice } from "@/app/use-appearance";
import { Button } from "@/components/ui/button";
import {
  Menu,
  MenuPopup,
  MenuRadioGroup,
  MenuRadioItem,
  MenuTrigger,
} from "@/components/ui/menu";
import { isColorMode } from "@/lib/appearance";

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

/** The theme choice as radio items, for any menu that offers it. */
export function ThemeMenuItems() {
  const { mode, choose } = useAppearanceChoice();
  const { t } = useLingui();
  return (
    <MenuRadioGroup
      aria-label={t({ id: "theme.label", message: "Theme" })}
      value={mode ?? "system"}
      onValueChange={(value) => {
        if (isColorMode(value)) choose({ mode: value });
      }}
    >
      {useThemeOptions().map(({ value, label }) => (
        <MenuRadioItem key={value} value={value}>
          {label}
        </MenuRadioItem>
      ))}
    </MenuRadioGroup>
  );
}

/** An icon button, showing the current theme, that opens the theme choice. */
export function ThemeMenu() {
  const theme = useAppearanceChoice().mode ?? "system";
  const { t } = useLingui();
  const Icon = useThemeOptions().find(({ value }) => value === theme)?.icon;
  return (
    <Menu>
      <MenuTrigger
        render={
          <Button
            aria-label={t({ id: "theme.label", message: "Theme" })}
            size="icon-sm"
            variant="ghost"
          />
        }
      >
        {Icon && <Icon aria-hidden="true" />}
      </MenuTrigger>
      <MenuPopup align="end">
        <ThemeMenuItems />
      </MenuPopup>
    </Menu>
  );
}
