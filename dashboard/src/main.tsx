import "@fontsource-variable/inter";
import "@/styles/index.css";
import { I18nProvider } from "@lingui/react";
import { Provider } from "jotai";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "@/App";
import { application } from "@/app/application";
import { Devtools } from "@/devtools/Devtools";
import { i18n, initializeLocale, store } from "@/i18n";

const stopLocaleSync = initializeLocale();

// Making room for the open devtools panel is a development aid: the panel is a
// fixed overlay, and the watch narrows the page to what is left. Nothing about
// it has to be in place before the app reads, so it loads alongside the mount.
if (import.meta.env.DEV) {
  void import("@/devtools/dock/dock").then(({ installDevtoolsDock }) => {
    installDevtoolsDock();
  });
}

const root = document.getElementById("root");
if (!root) throw new Error("Missing application root");

/**
 * Answers the app's requests from the mock panel as a development aid, and
 * waits for that to be in place before the app draws.
 *
 * The wait is the whole point rather than tidiness: the middleware is
 * registered by this module, and the protected route's session read enforces
 * freshness, so the first load always goes to the network. Mounting before the
 * middleware exists therefore sends that read to the server, whose 401 is what
 * redirected a `?mock` walkthrough to `/login` on a first load but not on a
 * navigation after it. Installing first is also what lets `?mock` be read from
 * the address bar in time — `installApiMocks` applies the query as it
 * registers, while its navigation subscriptions only see what comes after.
 *
 * The guard keeps the module and its fixtures out of the production bundle;
 * with it the branch is dead during a build and the import never enters the
 * graph, so the production path resolves without waiting on anything.
 */
const apiMocks = import.meta.env.DEV
  ? import("@/devtools/mock/install").then(({ installApiMocks }) =>
      installApiMocks(application),
    )
  : Promise.resolve();

void apiMocks.then(() => {
  createRoot(root).render(
    <StrictMode>
      <Provider store={store}>
        <I18nProvider i18n={i18n}>
          <App />
        </I18nProvider>
      </Provider>
      <Devtools />
    </StrictMode>,
  );
});

if (import.meta.hot) import.meta.hot.dispose(stopLocaleSync);
