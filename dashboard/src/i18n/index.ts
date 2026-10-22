import { setupI18n } from "@lingui/core";
import { createStore } from "jotai";
import { atomWithStorage } from "jotai/utils";
import { messages as en } from "@/locales/en/messages.po";
import { messages as zh } from "@/locales/zh/messages.po";

/**
 * The interface languages, in the order language pickers list them. This is
 * the same list the server allows for the maintenance notice
 * (`pkg/branding.NoticeLocales`); a test on each side pins the contents, so a
 * new language lands in both or neither.
 */
export const locales = ["en", "zh"] as const;

export type Locale = (typeof locales)[number];

/** Each language named in its own writing, for menus and field labels. */
export const localeNames: Record<Locale, string> = {
  en: "English",
  zh: "中文",
};

/** The BCP-47 tag set on elements written in a language that is not the UI's. */
export const localeTags: Record<Locale, string> = {
  en: "en",
  zh: "zh-CN",
};

const localeMessages = { en, zh };

export const localeAtom = atomWithStorage<Locale>(
  "prohibitorum.locale",
  "en",
  undefined,
  { getOnInit: true },
);
export const store = createStore();
export const i18n = setupI18n({ messages: localeMessages });

export function resolveLocale(value: unknown): Locale {
  return locales.includes(value as Locale) ? (value as Locale) : "en";
}

export function initializeLocale() {
  const activate = () => {
    const locale = resolveLocale(store.get(localeAtom));
    i18n.activate(locale);
    document.documentElement.lang = localeTags[locale];
  };
  activate();
  return store.sub(localeAtom, activate);
}
