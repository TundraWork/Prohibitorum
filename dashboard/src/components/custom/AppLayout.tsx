import { useLingui } from "@lingui/react/macro";
import { Outlet } from "@tanstack/react-router";
import { useAtomValue } from "jotai";
import { useEffect } from "react";
import { I18nProvider as AriaI18nProvider } from "react-aria";
import { AppNotifications } from "@/components/custom/AppNotifications";
import { darkAtom } from "@/components/custom/ThemeToggle";

const configuredName = document.querySelector<HTMLMetaElement>(
  'meta[name="instance-name"]',
)?.content;
export const instanceName =
  configuredName && configuredName !== "__INSTANCE_NAME__"
    ? configuredName
    : "Prohibitorum";

export function AppLayout() {
  const { i18n } = useLingui();
  const dark = useAtomValue(darkAtom);
  useEffect(() => {
    document.documentElement.dataset.theme = dark ? "dark" : "light";
  }, [dark]);
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
