import { ToggleButton, ToggleButtonGroup } from "@heroui/react";
import { useLingui } from "@lingui/react/macro";
import { atom, useAtom } from "jotai";
import { Monitor, Moon, Sun } from "lucide-react";

export const themeAtom = atom<"light" | "dark" | "system">("system");

export function ThemeSelect() {
  const [theme, setTheme] = useAtom(themeAtom);
  const { t } = useLingui();
  const options = [
    {
      value: "light",
      label: t({ id: "theme.light", message: "Light theme" }),
      icon: Sun,
    },
    {
      value: "dark",
      label: t({ id: "theme.dark", message: "Dark theme" }),
      icon: Moon,
    },
    {
      value: "system",
      label: t({ id: "theme.system", message: "System theme" }),
      icon: Monitor,
    },
  ] as const;

  return (
    <ToggleButtonGroup
      size="sm"
      selectionMode="single"
      disallowEmptySelection
      selectedKeys={[theme]}
      onSelectionChange={(keys) => {
        const value = keys.values().next().value;
        if (value === "light" || value === "dark" || value === "system") {
          setTheme(value);
        }
      }}
      aria-label={t({ id: "theme.label", message: "Theme" })}
    >
      {options.map(({ value, label, icon: Icon }) => (
        <ToggleButton key={value} id={value} isIconOnly aria-label={label}>
          <Icon size={16} aria-hidden="true">
            <title>{label}</title>
          </Icon>
        </ToggleButton>
      ))}
    </ToggleButtonGroup>
  );
}
