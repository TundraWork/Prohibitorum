import { useLingui } from "@lingui/react/macro";
import { atom, useAtom } from "jotai";
import { MonitorIcon, MoonIcon, SunIcon } from "lucide-react";
import {
  RadioGroupPrimitive,
  RadioPrimitive,
} from "@/components/ui/radio-group";
import {
  segmentedControlItemVariants,
  segmentedControlRootClassName,
} from "@/lib/segmented-control";

/** The visitor's choice, which the console and the public pages share. */
export const themeAtom = atom<"light" | "dark" | "system">("system");

/**
 * The theme the sign-in page's settings force on the public pages, or nothing
 * when the visitor chooses. Only `PublicLayout` writes it, while it is mounted;
 * `themeAtom` is left alone, so the console keeps the visitor's choice.
 */
export const publicThemeAtom = atom<"light" | "dark" | undefined>(undefined);

const themeItem = segmentedControlItemVariants({
  size: "sm",
  state: "checked",
  className: "w-8 px-0",
});

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

export function ThemeSelect() {
  const [theme, setTheme] = useAtom(themeAtom);
  const { t } = useLingui();
  const options = useThemeOptions();

  return (
    <RadioGroupPrimitive
      aria-label={t({ id: "theme.label", message: "Theme" })}
      className={segmentedControlRootClassName}
      value={theme}
      onValueChange={(value) => {
        if (value === "light" || value === "dark" || value === "system") {
          setTheme(value);
        }
      }}
    >
      {options.map(({ value, label, icon: Icon }) => (
        <RadioPrimitive.Root
          key={value}
          aria-label={label}
          className={themeItem}
          value={value}
        >
          <Icon aria-hidden="true" />
        </RadioPrimitive.Root>
      ))}
    </RadioGroupPrimitive>
  );
}
