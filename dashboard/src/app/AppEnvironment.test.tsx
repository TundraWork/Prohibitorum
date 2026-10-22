import { I18nProvider } from "@lingui/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, waitFor } from "@testing-library/react";
import { createStore, Provider } from "jotai";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import { publicConfigQueryOptions, sessionQueryOptions } from "@/api/queries";
import { AppEnvironment } from "@/app/AppEnvironment";
import { useAppearanceChoice } from "@/app/use-appearance";
import { i18n } from "@/i18n";
import {
  appliedAppearanceKey,
  forcedModeAtom,
  modeChoiceAtom,
  schemeChoiceAtom,
} from "@/lib/appearance";
import { testConfig, testSession } from "@/test/app";

type Session = components["schemas"]["SessionView"];

let store: ReturnType<typeof createStore>;
let queryClient: QueryClient;
let systemDark: boolean;
let changeListeners: Array<() => void>;
const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
/** The bodies of the preference writes, in order. */
let patches: unknown[];

beforeEach(() => {
  i18n.activate("en");
  localStorage.clear();
  store = createStore();
  queryClient = new QueryClient();
  systemDark = false;
  changeListeners = [];
  patches = [];
  fetchBoundary.mockReset();
  fetchBoundary.mockImplementation(async (request) => {
    const body = await request.json();
    if (request.method === "PATCH") patches.push(body);
    return Response.json({ appearance: body.appearance });
  });
  vi.stubGlobal("fetch", fetchBoundary);
  vi.spyOn(window, "matchMedia").mockImplementation(
    (query: string) =>
      ({
        get matches() {
          return systemDark && query === "(prefers-color-scheme: dark)";
        },
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: (_: string, listener: () => void) => {
          changeListeners.push(listener);
        },
        removeEventListener: (_: string, listener: () => void) => {
          changeListeners = changeListeners.filter((l) => l !== listener);
        },
        dispatchEvent: vi.fn(),
      }) as unknown as MediaQueryList,
  );
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  localStorage.clear();
  const root = document.documentElement;
  delete root.dataset.theme;
  delete root.dataset.scheme;
  root.classList.remove("dark");
});

let chooser: ReturnType<typeof useAppearanceChoice> | undefined;

function Chooser() {
  chooser = useAppearanceChoice();
  return null;
}

function mount() {
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <Provider store={store}>
          <AppEnvironment>
            <Chooser />
          </AppEnvironment>
        </Provider>
      </QueryClientProvider>
    </I18nProvider>,
  );
}

function withInstanceScheme(scheme: string) {
  queryClient.setQueryData(publicConfigQueryOptions().queryKey, {
    ...testConfig,
    loginAppearance: {
      ...testConfig.loginAppearance,
      scheme,
    } as typeof testConfig.loginAppearance,
  });
}

function signedIn(appearance: Session["preferences"]["appearance"]) {
  queryClient.setQueryData<Session>(sessionQueryOptions().queryKey, {
    ...testSession,
    preferences: { appearance },
  });
}

const root = () => document.documentElement;
const htmlTheme = () => root().dataset.theme;
const htmlScheme = () => root().dataset.scheme;
const hasDarkClass = () => root().classList.contains("dark");
const applied = () =>
  JSON.parse(localStorage.getItem(appliedAppearanceKey) ?? "null");

it("writes the visitor's mode on the document", () => {
  store.set(modeChoiceAtom, "dark");
  mount();
  expect(htmlTheme()).toBe("dark");
  expect(hasDarkClass()).toBe(true);
  act(() => store.set(modeChoiceAtom, "light"));
  expect(htmlTheme()).toBe("light");
  expect(hasDarkClass()).toBe(false);
});

it("follows the system preference while no mode or the system mode is chosen", () => {
  systemDark = true;
  mount();
  expect(htmlTheme()).toBe("dark");
  expect(hasDarkClass()).toBe(true);
  systemDark = false;
  act(() => {
    for (const listener of changeListeners) listener();
  });
  expect(htmlTheme()).toBe("light");
  expect(hasDarkClass()).toBe(false);
});

it("uses a forced public mode over the visitor's choice and the system preference", () => {
  systemDark = true;
  store.set(forcedModeAtom, "light");
  mount();
  expect(htmlTheme()).toBe("light");
  expect(changeListeners).toHaveLength(0);

  act(() => store.set(modeChoiceAtom, "dark"));
  expect(htmlTheme()).toBe("light");

  act(() => store.set(forcedModeAtom, undefined));
  expect(htmlTheme()).toBe("dark");
  expect(store.get(modeChoiceAtom)).toBe("dark");
});

it("applies the visitor's scheme over the instance default, and the default without one", () => {
  withInstanceScheme("sand");
  mount();
  expect(htmlScheme()).toBe("sand");
  act(() => store.set(schemeChoiceAtom, "lagoon"));
  expect(htmlScheme()).toBe("lagoon");
  act(() => store.set(schemeChoiceAtom, null));
  expect(htmlScheme()).toBe("sand");
});

it("keeps the scheme applied before the first paint until the instance default is known", async () => {
  root().dataset.scheme = "citrus";
  mount();
  expect(htmlScheme()).toBe("citrus");
  expect(applied()).toBeNull();
  act(() => withInstanceScheme("nord"));
  await waitFor(() => expect(htmlScheme()).toBe("nord"));
});

it("keeps what it applied for the next page load, the forced mode included", () => {
  withInstanceScheme("matcha");
  store.set(forcedModeAtom, "dark");
  mount();
  expect(applied()).toEqual({ scheme: "matcha", mode: "dark" });
  act(() => store.set(forcedModeAtom, undefined));
  expect(applied()).toEqual({ scheme: "matcha", mode: "system" });
});

it("reads a stored choice it does not recognise as not chosen", () => {
  localStorage.setItem("prohibitorum.scheme", JSON.stringify("solarized"));
  localStorage.setItem("prohibitorum.mode", JSON.stringify("dim"));
  expect(store.get(schemeChoiceAtom)).toBeNull();
  expect(store.get(modeChoiceAtom)).toBeNull();
  localStorage.setItem("prohibitorum.scheme", JSON.stringify("sakura"));
  // A mounted atom reads the browser's storage again.
  store.sub(schemeChoiceAtom, () => {});
  expect(store.get(schemeChoiceAtom)).toBe("sakura");
});

it("lets an account's saved appearance replace this browser's choice", () => {
  store.set(schemeChoiceAtom, "citrus");
  store.set(modeChoiceAtom, "light");
  signedIn({ scheme: "nord", mode: "dark" });
  mount();
  expect(store.get(schemeChoiceAtom)).toBe("nord");
  expect(store.get(modeChoiceAtom)).toBe("dark");
  expect(htmlScheme()).toBe("nord");
  expect(patches).toHaveLength(0);
});

it("gives an account without a saved appearance this browser's choice", async () => {
  store.set(schemeChoiceAtom, "sakura");
  signedIn({ scheme: null, mode: null });
  mount();
  await waitFor(() => expect(patches).toHaveLength(1));
  expect(patches[0]).toEqual({
    appearance: { scheme: "sakura", mode: null },
  });
  await waitFor(() =>
    expect(
      queryClient.getQueryData<Session>(sessionQueryOptions().queryKey)
        ?.preferences.appearance,
    ).toEqual({ scheme: "sakura", mode: null }),
  );
  expect(htmlScheme()).toBe("sakura");
});

it("saves nothing when neither the account nor this browser has chosen", () => {
  withInstanceScheme("graphite");
  signedIn({ scheme: null, mode: null });
  mount();
  expect(patches).toHaveLength(0);
});

it("saves a change to the account as the whole appearance section", async () => {
  signedIn({ scheme: "nord", mode: null });
  mount();
  act(() => chooser?.choose({ mode: "dark" }));
  expect(htmlTheme()).toBe("dark");
  await waitFor(() => expect(patches).toHaveLength(1));
  expect(patches[0]).toEqual({
    appearance: { scheme: "nord", mode: "dark" },
  });
});

it("keeps a visitor's change in this browser only", () => {
  mount();
  act(() => chooser?.choose({ scheme: "lavender" }));
  expect(store.get(schemeChoiceAtom)).toBe("lavender");
  expect(htmlScheme()).toBe("lavender");
  expect(patches).toHaveLength(0);
});
