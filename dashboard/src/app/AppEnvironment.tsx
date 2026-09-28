import { useLingui } from "@lingui/react/macro";
import { useAtomValue } from "jotai";
import { type ReactNode, useEffect } from "react";
import { I18nProvider as AriaI18nProvider } from "react-aria";
import { PageScrollArea } from "@/components/custom/PageScrollArea";
import { themeAtom } from "@/components/custom/ThemeSelect";

/**
 * What every screen needs whether or not a route drew: the theme, the locale
 * React Aria formats with, and the page's scrollbar. It sits outside the
 * router, so a root route that fails still draws its error in them.
 */
export function AppEnvironment({ children }: { children: ReactNode }) {
  const { i18n } = useLingui();
  const theme = useAtomValue(themeAtom);
  useEffect(() => {
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
