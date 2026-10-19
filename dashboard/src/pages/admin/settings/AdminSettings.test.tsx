import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { publicConfigQueryOptions } from "@/api/queries";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { AdminSettings } from "@/pages/admin/settings/AdminSettings";
import { publicConfigFixture } from "@/test/public-config";

let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  // The reads behind the other sections never answer here: this checks the
  // frame, and each section keeps its heading while it waits.
  vi.stubGlobal(
    "fetch",
    vi.fn(() => new Promise<Response>(() => {})),
  );
  queryClient = createQueryClient(() => undefined);
  queryClient.setQueryData(
    publicConfigQueryOptions().queryKey,
    publicConfigFixture(),
  );
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

function mount() {
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <AdminSettings />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

const sections = [
  "General",
  "Sign-in page",
  "Maintenance mode",
  "Client IP",
  "Signing keys",
];

describe("the settings page", () => {
  it("is headed Settings, with a line saying what it is for", () => {
    mount();

    expect(
      screen.getByRole("heading", { level: 1, name: "Settings" }),
    ).toBeVisible();
    expect(
      screen.getByText(
        "How this instance is named, signed in to and kept running.",
      ),
    ).toBeVisible();
  });

  it("draws the five sections in order", () => {
    mount();

    expect(
      screen
        .getAllByRole("heading", { level: 2 })
        .map((heading) => heading.textContent),
    ).toEqual(sections);
  });

  it("lists the same sections in the outline, each linking to its own", () => {
    mount();

    const rail = screen.getByRole("navigation", { name: "On this page" });
    const links = within(rail).getAllByRole("link");
    expect(links.map((link) => link.textContent)).toEqual(sections);
    expect(
      within(rail).getByRole("link", { name: "Maintenance mode" }),
    ).toHaveAttribute("href", "#maintenance");
    for (const link of links) {
      const id = link.getAttribute("href")?.slice(1) ?? "";
      expect(document.getElementById(id)).not.toBeNull();
    }
  });
});
