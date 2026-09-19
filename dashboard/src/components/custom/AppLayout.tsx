import { Button, Link as HeroLink } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { createLink, Outlet } from "@tanstack/react-router";
import { atom, useAtom } from "jotai";
import { useEffect } from "react";
import { I18nProvider as AriaI18nProvider } from "react-aria";
import { AppNotifications } from "@/components/custom/AppNotifications";
import { LanguageSelect } from "@/components/custom/LanguageSelect";

const NavigationLink = createLink(HeroLink);

const darkAtom = atom(false);
const configuredName = document.querySelector<HTMLMetaElement>(
  'meta[name="instance-name"]',
)?.content;
const instanceName =
  configuredName && configuredName !== "__INSTANCE_NAME__"
    ? configuredName
    : "Prohibitorum";

export function AppLayout() {
  const { i18n, t } = useLingui();
  const [dark, setDark] = useAtom(darkAtom);
  useEffect(() => {
    document.documentElement.dataset.theme = dark ? "dark" : "light";
  }, [dark]);
  useEffect(() => {
    document.title = instanceName;
  }, []);
  return (
    <AriaI18nProvider locale={i18n.locale === "zh" ? "zh-CN" : "en"}>
      <header className="mx-auto flex w-full max-w-5xl flex-wrap items-center justify-between gap-4 px-4 py-6 sm:px-6">
        <span className="text-lg font-semibold wrap-anywhere">
          {instanceName}
        </span>
        <div className="flex flex-wrap items-center gap-3">
          <LanguageSelect />
          <Button
            variant="outline"
            aria-pressed={dark}
            onPress={() => setDark((current) => !current)}
          >
            <Trans id="theme.toggle">Toggle theme</Trans>
          </Button>
        </div>
      </header>
      <main className="mx-auto flex w-full max-w-5xl flex-col gap-6 px-4 pb-8 sm:px-6">
        <nav
          className="flex flex-wrap items-center gap-x-6 gap-y-3"
          aria-label={t({
            id: "navigation.preview",
            message: "Preview navigation",
          })}
        >
          <NavigationLink to="/" activeOptions={{ exact: true }}>
            <Trans id="navigation.components">Components</Trans>
          </NavigationLink>
          <NavigationLink to="/preview/api">
            <Trans id="navigation.api">Public API</Trans>
          </NavigationLink>
          {import.meta.env.DEV && (
            <NavigationLink to="/__dev/forms">
              <Trans id="navigation.forms">
                Form verification (development)
              </Trans>
            </NavigationLink>
          )}
        </nav>
        <Outlet />
      </main>
      <AppNotifications />
    </AriaI18nProvider>
  );
}
