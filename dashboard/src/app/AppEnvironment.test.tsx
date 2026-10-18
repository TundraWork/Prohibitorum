import { I18nProvider } from "@lingui/react";
import { act, render } from "@testing-library/react";
import { createStore, Provider } from "jotai";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AppEnvironment } from "@/app/AppEnvironment";
import { publicThemeAtom, themeAtom } from "@/components/custom/ThemeSelect";
import { i18n } from "@/i18n";

let store: ReturnType<typeof createStore>;
let systemDark: boolean;
let changeListeners: Array<() => void>;

beforeEach(() => {
  i18n.activate("en");
  store = createStore();
  systemDark = false;
  changeListeners = [];
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
  delete document.documentElement.dataset.theme;
  document.documentElement.classList.remove("dark");
});

function mount() {
  render(
    <I18nProvider i18n={i18n}>
      <Provider store={store}>
        <AppEnvironment>
          <p>Page</p>
        </AppEnvironment>
      </Provider>
    </I18nProvider>,
  );
}

const htmlTheme = () => document.documentElement.dataset.theme;
const hasDarkClass = () => document.documentElement.classList.contains("dark");

it("writes the visitor's theme on the document", () => {
  store.set(themeAtom, "dark");
  mount();
  expect(htmlTheme()).toBe("dark");
  act(() => store.set(themeAtom, "light"));
  expect(htmlTheme()).toBe("light");
});

it("adds the dark class for the dark theme", () => {
  store.set(themeAtom, "dark");
  mount();
  expect(hasDarkClass()).toBe(true);
  expect(htmlTheme()).toBe("dark");
});

it("removes the dark class for the light theme", () => {
  store.set(themeAtom, "dark");
  mount();
  act(() => store.set(themeAtom, "light"));
  expect(hasDarkClass()).toBe(false);
});

it("follows the system preference while the visitor picks the system theme", () => {
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

it("uses a forced public theme over the visitor's choice and the system preference", () => {
  systemDark = true;
  store.set(publicThemeAtom, "light");
  mount();
  expect(htmlTheme()).toBe("light");
  expect(changeListeners).toHaveLength(0);

  act(() => store.set(themeAtom, "dark"));
  expect(htmlTheme()).toBe("light");

  act(() => store.set(publicThemeAtom, undefined));
  expect(htmlTheme()).toBe("dark");
  expect(store.get(themeAtom)).toBe("dark");
});
