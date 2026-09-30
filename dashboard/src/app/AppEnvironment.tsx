import { useLingui } from "@lingui/react/macro";
import { useAtomValue } from "jotai";
import { type ReactNode, useLayoutEffect } from "react";
import { I18nProvider as AriaI18nProvider } from "react-aria";
import { PageScrollArea } from "@/components/custom/PageScrollArea";
import { publicThemeAtom, themeAtom } from "@/components/custom/ThemeSelect";

/**
 * What every screen needs whether or not a route drew: the theme, the locale
 * React Aria formats with, and the page's scrollbar. It sits outside the
 * router, so a root route that fails still draws its error in them.
 *
 * The theme is the one the public pages force, if any, or else the visitor's.
 * It is written on `<html>` before the browser paints, so moving between the
 * console and a public page never shows a frame in the other theme, and popups
 * rendered under `body` follow it too.
 */
export function AppEnvironment({ children }: { children: ReactNode }) {
  const { i18n } = useLingui();
  const chosen = useAtomValue(themeAtom);
  const forced = useAtomValue(publicThemeAtom);
  const theme = forced ?? chosen;
  useLayoutEffect(() => {
    if (theme !== "system") {
      document.documentElement.dataset.theme = theme;
      return;
    }
    const preference = window.matchMedia("(prefers-color-scheme: dark)");
    const apply = () => {
      document.documentElement.dataset.theme = preference.matches
        ? "dark"
        : "light";
    };
    apply();
    preference.addEventListener("change", apply);
    return () => preference.removeEventListener("change", apply);
  }, [theme]);
  return (
    <AriaI18nProvider locale={i18n.locale === "zh" ? "zh-CN" : "en"}>
      {children}
      <PageScrollArea />
    </AriaI18nProvider>
  );
}
