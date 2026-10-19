import type { QueryClient } from "@tanstack/react-query";
import { screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { fakeApi, publicApi, renderApp, testSession } from "@/test/app";

let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  // The reads behind the other sections never answer here: this checks the
  // frame, and each section keeps its heading while it waits.
  fakeApi({
    ...publicApi({ ...testSession, role: "admin" }),
    "GET /api/prohibitorum/admin/settings/client-ip": () =>
      new Promise<Response>(() => {}),
    "GET /api/prohibitorum/signing-keys": () => new Promise<Response>(() => {}),
  });
  queryClient = createQueryClient(() => undefined);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

function mount() {
  renderApp("/admin/settings", queryClient);
}

const sections = [
  "General",
  "Sign-in page",
  "Maintenance mode",
  "Client IP",
  "Signing keys",
];

describe("the settings page", () => {
  it("is headed Settings, with a line saying what it is for", async () => {
    mount();

    expect(
      await screen.findByRole("heading", { level: 1, name: "Settings" }),
    ).toBeVisible();
    expect(
      screen.getByText(
        "How this instance is named, signed in to and kept running.",
      ),
    ).toBeVisible();
  });

  it("draws the five sections in order", async () => {
    mount();
    await screen.findByRole("heading", { level: 1, name: "Settings" });

    expect(
      screen
        .getAllByRole("heading", { level: 2 })
        .map((heading) => heading.textContent),
    ).toEqual(sections);
  });

  it("lists the same sections in the outline, each linking to its own", async () => {
    mount();
    await screen.findByRole("heading", { level: 1, name: "Settings" });

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
