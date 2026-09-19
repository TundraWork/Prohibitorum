import { Button, ButtonGroup } from "@heroui/react";
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
    <ButtonGroup
      size="sm"
      aria-label={t({ id: "theme.label", message: "Theme" })}
    >
      {options.map(({ value, label, icon: Icon }) => (
        <Button
          key={value}
          isIconOnly
          aria-label={label}
          aria-pressed={theme === value}
          variant={theme === value ? "secondary" : "ghost"}
          onPress={() => setTheme(value)}
        >
          <Icon size={16} aria-hidden="true">
            <title>{label}</title>
          </Icon>
        </Button>
      ))}
    </ButtonGroup>
  );
}
