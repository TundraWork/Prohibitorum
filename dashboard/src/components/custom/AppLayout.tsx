import { useLingui } from "@lingui/react/macro";
import { Outlet } from "@tanstack/react-router";
import { useAtomValue } from "jotai";
import { useEffect } from "react";
import { I18nProvider as AriaI18nProvider } from "react-aria";
import { AppNotifications } from "@/components/custom/AppNotifications";
import { themeAtom } from "@/components/custom/ThemeSelect";

const configuredName = document.querySelector<HTMLMetaElement>(
  'meta[name="instance-name"]',
)?.content;
export const instanceName =
  configuredName && configuredName !== "__INSTANCE_NAME__"
    ? configuredName
    : "Prohibitorum";

/** The instance's own mark, served from the same origin as the console. */
export const instanceIconUrl = "/branding/icon";

export function AppLayout() {
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
  useEffect(() => {
    document.title = instanceName;
  }, []);
  return (
    <AriaI18nProvider locale={i18n.locale === "zh" ? "zh-CN" : "en"}>
      <Outlet />
      <AppNotifications />
    </AriaI18nProvider>
  );
}
