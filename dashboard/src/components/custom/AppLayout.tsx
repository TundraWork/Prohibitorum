import { useLingui } from "@lingui/react/macro";
import { Outlet } from "@tanstack/react-router";
import { useAtomValue } from "jotai";
import { useEffect } from "react";
import { I18nProvider as AriaI18nProvider } from "react-aria";
import { AppNotifications } from "@/components/custom/AppNotifications";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { PageScrollArea } from "@/components/custom/PageScrollArea";
import { themeAtom } from "@/components/custom/ThemeSelect";

export function AppLayout() {
  const { i18n } = useLingui();
  const theme = useAtomValue(themeAtom);
  const { name } = useInstanceBranding();
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
  useEffect(() => {
    document.title = name;
  }, [name]);
  return (
    <AriaI18nProvider locale={i18n.locale === "zh" ? "zh-CN" : "en"}>
      <Outlet />
      <PageScrollArea />
      <AppNotifications />
    </AriaI18nProvider>
  );
}
