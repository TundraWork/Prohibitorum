import { Button } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { Link, Outlet } from "@tanstack/react-router";
import { atom, useAtom } from "jotai";
import { useEffect } from "react";
import { I18nProvider as AriaI18nProvider } from "react-aria";
import { AppNotifications } from "@/components/custom/AppNotifications";
import { LanguageSelect } from "@/components/custom/LanguageSelect";

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
      <header className="page-header">
        <div className="header-content">
          <div className="brand">
            <span className="brand-mark" aria-hidden="true" />
            {instanceName}
          </div>
          <div className="header-controls">
            <LanguageSelect />
            <Button
              variant="outline"
              aria-pressed={dark}
              onPress={() => setDark((current) => !current)}
            >
              <Trans id="theme.toggle">Toggle theme</Trans>
            </Button>
          </div>
        </div>
      </header>
      <main className="preview">
        <nav
          className="preview-navigation"
          aria-label={t({
            id: "navigation.preview",
            message: "Preview navigation",
          })}
        >
          <Link to="/" activeOptions={{ exact: true }}>
            <Trans id="navigation.components">Components</Trans>
          </Link>
          <Link to="/preview/api">
            <Trans id="navigation.api">Public API</Trans>
          </Link>
          {import.meta.env.DEV && (
            <Link to="/__dev/forms">
              <Trans id="navigation.forms">
                Form verification (development)
              </Trans>
            </Link>
          )}
        </nav>
        <Outlet />
      </main>
      <AppNotifications />
    </AriaI18nProvider>
  );
}
