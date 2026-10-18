import { useLingui } from "@lingui/react/macro";
import { useAtom } from "jotai";
import { LanguagesIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Menu,
  MenuPopup,
  MenuRadioGroup,
  MenuRadioItem,
  MenuTrigger,
} from "@/components/ui/menu";
import { localeAtom, resolveLocale } from "@/i18n";

/** The two languages, as radio items for any menu that offers the choice. */
export function LanguageMenuItems() {
  const [locale, setLocale] = useAtom(localeAtom);
  const { t } = useLingui();
  return (
    <MenuRadioGroup
      aria-label={t({ id: "language.label", message: "Language" })}
      value={resolveLocale(locale)}
      onValueChange={(value) => {
        if (value === "en" || value === "zh") setLocale(value);
      }}
    >
      <MenuRadioItem value="en" lang="en">
        English
      </MenuRadioItem>
      <MenuRadioItem value="zh" lang="zh-CN">
        中文
      </MenuRadioItem>
    </MenuRadioGroup>
  );
}

export function LanguageMenu() {
  const { t } = useLingui();
  const label = t({ id: "language.label", message: "Language" });

  return (
    <Menu>
      <MenuTrigger
        render={<Button aria-label={label} size="icon-sm" variant="ghost" />}
      >
        <LanguagesIcon aria-hidden="true" />
      </MenuTrigger>
      <MenuPopup align="end">
        <LanguageMenuItems />
      </MenuPopup>
    </Menu>
  );
}
