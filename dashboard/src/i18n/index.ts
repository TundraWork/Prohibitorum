import { setupI18n } from "@lingui/core";
import { createStore } from "jotai";
import { atomWithStorage } from "jotai/utils";
import { messages as en } from "@/locales/en/messages.po";
import { messages as zh } from "@/locales/zh/messages.po";

export type Locale = "en" | "zh";

export const localeAtom = atomWithStorage<Locale>(
  "prohibitorum.locale",
  "en",
  undefined,
  { getOnInit: true },
);
export const store = createStore();
export const i18n = setupI18n({ messages: { en, zh } });

export function resolveLocale(value: unknown): Locale {
  return value === "zh" ? "zh" : "en";
}

export function initializeLocale() {
  const activate = () => {
    const locale = resolveLocale(store.get(localeAtom));
    i18n.activate(locale);
    document.documentElement.lang = locale === "zh" ? "zh-CN" : "en";
  };
  activate();
  return store.sub(localeAtom, activate);
}
