import { useLingui } from "@lingui/react/macro";
import { atom, useAtom, useAtomValue } from "jotai";
import { MonitorIcon, MoonIcon, SunIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Menu,
  MenuPopup,
  MenuRadioGroup,
  MenuRadioItem,
  MenuTrigger,
} from "@/components/ui/menu";

/** The visitor's choice, which the console and the public pages share. */
export const themeAtom = atom<"light" | "dark" | "system">("system");

/**
 * The theme the sign-in page's settings force on the public pages, or nothing
 * when the visitor chooses. Only `PublicLayout` writes it, while it is mounted;
 * `themeAtom` is left alone, so the console keeps the visitor's choice.
 */
export const publicThemeAtom = atom<"light" | "dark" | undefined>(undefined);

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
  const [theme, setTheme] = useAtom(themeAtom);
  const { t } = useLingui();
  return (
    <MenuRadioGroup
      aria-label={t({ id: "theme.label", message: "Theme" })}
      value={theme}
      onValueChange={(value) => {
        if (value === "light" || value === "dark" || value === "system") {
          setTheme(value);
        }
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
  const theme = useAtomValue(themeAtom);
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
