import { Dropdown, Label } from "@heroui/react";
import { useLingui } from "@lingui/react/macro";
import { useAtom } from "jotai";
import { Languages } from "lucide-react";
import { Button } from "@/components/custom/Button";
import { localeAtom, resolveLocale } from "@/i18n";

export function LanguageMenu() {
  const [locale, setLocale] = useAtom(localeAtom);
  const { t } = useLingui();
  const label = t({ id: "language.label", message: "Language" });

  return (
    <Dropdown>
      <Button isIconOnly size="sm" variant="ghost" aria-label={label}>
        <Languages size={18} aria-hidden="true">
          <title>{label}</title>
        </Languages>
      </Button>
      <Dropdown.Popover placement="bottom end">
        <Dropdown.Menu
          aria-label={label}
          selectionMode="single"
          disallowEmptySelection
          selectedKeys={[resolveLocale(locale)]}
          onAction={(key) => {
            if (key === "en" || key === "zh") setLocale(key);
          }}
        >
          <Dropdown.Item id="en" textValue="English">
            <Label lang="en">English</Label>
            <Dropdown.ItemIndicator />
          </Dropdown.Item>
          <Dropdown.Item id="zh" textValue="中文">
            <Label lang="zh-CN">中文</Label>
            <Dropdown.ItemIndicator />
          </Dropdown.Item>
        </Dropdown.Menu>
      </Dropdown.Popover>
    </Dropdown>
  );
}
