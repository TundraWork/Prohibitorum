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
import {
  localeAtom,
  localeNames,
  locales,
  localeTags,
  resolveLocale,
} from "@/i18n";

/** The languages, as radio items for any menu that offers the choice. */
export function LanguageMenuItems() {
  const [locale, setLocale] = useAtom(localeAtom);
  const { t } = useLingui();
  return (
    <MenuRadioGroup
      aria-label={t({ id: "language.label", message: "Language" })}
      value={resolveLocale(locale)}
      onValueChange={(value) => {
        if (locales.includes(value as (typeof locales)[number])) {
          setLocale(value as (typeof locales)[number]);
        }
      }}
    >
      {locales.map((option) => (
        <MenuRadioItem key={option} value={option} lang={localeTags[option]}>
          {localeNames[option]}
        </MenuRadioItem>
      ))}
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
