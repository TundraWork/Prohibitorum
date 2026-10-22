import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createStore, Provider } from "jotai";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import { publicConfigQueryOptions, sessionQueryOptions } from "@/api/queries";
import { AppEnvironment } from "@/app/AppEnvironment";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { modeChoiceAtom, schemeChoiceAtom } from "@/lib/appearance";
import { AppearancePanel } from "@/pages/profile/AppearancePanel";
import { testConfig, testSession } from "@/test/app";

type Session = components["schemas"]["SessionView"];

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifySuccess = vi.fn<(message: MessageDescriptor) => void>();
let queryClient: QueryClient;
let store: ReturnType<typeof createStore>;
/** The bodies `PATCH /me/preferences` received. */
let saved: unknown[];
/** Answers the write in flight; the server waits until the test calls it. */
let answer: () => void;

beforeEach(() => {
  i18n.activate("en");
  localStorage.clear();
  saved = [];
  fetchBoundary.mockReset();
  notifySuccess.mockReset();
  fetchBoundary.mockImplementation(async (request) => {
    const body = (await request.json()) as Session["preferences"];
    saved.push(body);
    await new Promise<void>((resolve) => {
      answer = resolve;
    });
    return Response.json(body);
  });
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(() => undefined, notifySuccess);
  queryClient.setQueryData(publicConfigQueryOptions().queryKey, testConfig);
  queryClient.setQueryData<Session>(sessionQueryOptions().queryKey, {
    ...testSession,
    preferences: { appearance: { scheme: "nord", mode: null } },
  });
  store = createStore();
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.clear();
  delete document.documentElement.dataset.scheme;
  delete document.documentElement.dataset.theme;
  document.documentElement.classList.remove("dark");
});

function mount() {
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <Provider store={store}>
          <AppEnvironment>
            <AppearancePanel />
          </AppEnvironment>
        </Provider>
      </QueryClientProvider>
    </I18nProvider>,
  );
}

it("shows the account's choice and every scheme, previewed in both modes", () => {
  mount();
  const modes = screen.getByRole("radiogroup", { name: "Light or dark" });
  expect(within(modes).getAllByRole("radio")).toHaveLength(3);
  expect(
    within(modes).getByRole("radio", { name: "System theme" }),
  ).toBeChecked();
  const schemes = screen.getByRole("radiogroup", { name: "Color scheme" });
  expect(within(schemes).getAllByRole("radio")).toHaveLength(8);
  expect(within(schemes).getByRole("radio", { name: "Nord" })).toBeChecked();
  expect(document.querySelectorAll("[data-scheme-preview]")).toHaveLength(8);
});

it("applies a choice at once and saves the whole appearance section", async () => {
  const user = userEvent.setup();
  mount();
  await user.click(screen.getByRole("radio", { name: "Sakura" }));
  await waitFor(() =>
    expect(saved).toEqual([{ appearance: { scheme: "sakura", mode: null } }]),
  );
  // The page shows the new scheme before the server answers.
  expect(document.documentElement.dataset.scheme).toBe("sakura");
  expect(screen.getByRole("radio", { name: "Sakura" })).toBeChecked();
  expect(store.get(schemeChoiceAtom)).toBe("sakura");
  answer();

  await user.click(screen.getByRole("radio", { name: "Dark theme" }));
  expect(document.documentElement.dataset.theme).toBe("dark");
  expect(store.get(modeChoiceAtom)).toBe("dark");
  await waitFor(() =>
    expect(saved.at(-1)).toEqual({
      appearance: { scheme: "sakura", mode: "dark" },
    }),
  );
  answer();
  expect(notifySuccess).not.toHaveBeenCalled();
});
