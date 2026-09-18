import { ListBox, Select } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useAtom } from "jotai";
import { localeAtom, resolveLocale } from "@/i18n";

export function LanguageSelect() {
  const [locale, setLocale] = useAtom(localeAtom);
  const { t } = useLingui();

  return (
    <Select
      aria-label={t({ id: "language.label", message: "Language" })}
      className="language-select"
      value={resolveLocale(locale)}
      onChange={(value) => {
        if (value === "en" || value === "zh") setLocale(value);
      }}
    >
      <Select.Trigger>
        <Select.Value />
        <Select.Indicator />
      </Select.Trigger>
      <Select.Popover>
        <ListBox>
          <ListBox.Item
            id="en"
            textValue={t({ id: "language.english", message: "English" })}
          >
            <Trans id="language.english">English</Trans>
            <ListBox.ItemIndicator />
          </ListBox.Item>
          <ListBox.Item
            id="zh"
            textValue={t({ id: "language.chinese", message: "Chinese" })}
          >
            <Trans id="language.chinese">Chinese</Trans>
            <ListBox.ItemIndicator />
          </ListBox.Item>
        </ListBox>
      </Select.Popover>
    </Select>
  );
}
