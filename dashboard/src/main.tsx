import "@fontsource-variable/inter";
import "@/styles/index.css";
import { I18nProvider } from "@lingui/react";
import { Provider } from "jotai";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "@/App";
import { i18n, initializeLocale, store } from "@/i18n";

const stopLocaleSync = initializeLocale();
const root = document.getElementById("root");
if (!root) throw new Error("Missing application root");

createRoot(root).render(
  <StrictMode>
    <Provider store={store}>
      <I18nProvider i18n={i18n}>
        <App />
      </I18nProvider>
    </Provider>
  </StrictMode>,
);

if (import.meta.hot) import.meta.hot.dispose(stopLocaleSync);
