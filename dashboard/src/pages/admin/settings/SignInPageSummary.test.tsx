import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { LoginAppearance } from "@/api/raw-paths";
import { createQueryClient } from "@/app/query-client";
import { defaultLoginAppearance } from "@/components/custom/login-appearance/appearance";
import { i18n } from "@/i18n";
import {
  fakeApi,
  publicApi,
  renderApp,
  testConfig,
  testSession,
} from "@/test/app";

const queryClient = createQueryClient(vi.fn());

beforeEach(() => {
  i18n.activate("en");
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

function withAppearance(edit: (a: LoginAppearance) => void): LoginAppearance {
  const a = structuredClone(defaultLoginAppearance);
  edit(a);
  return a;
}

async function open(
  appearance: LoginAppearance = defaultLoginAppearance,
  imageCount = 0,
) {
  fakeApi({
    ...publicApi(
      { ...testSession, role: "admin" },
      {
        ...testConfig,
        loginAppearance: appearance,
        loginImages: Array.from({ length: imageCount }, (_, i) => ({
          id: i + 1,
          url: `/branding/login-images/${i + 1}`,
          etag: `e${i}`,
        })),
      },
    ),
    "GET /api/prohibitorum/admin/settings/login-appearance": () =>
      Response.json({ appearance, hasUnsplashKey: false }),
    // The other sections are not under test and never answer.
    "GET /api/prohibitorum/admin/settings/client-ip": () =>
      new Promise(() => {}),
    "GET /api/prohibitorum/signing-keys": () => new Promise(() => {}),
  });
  renderApp("/admin/settings", queryClient);
  const heading = await screen.findByRole("heading", {
    level: 2,
    name: "Sign-in page",
  });
  const section = heading.closest("section");
  if (!section) throw new Error("no section");
  return section;
}

describe("the sign-in page summary", () => {
  it("says the background and the card's place in words", async () => {
    const section = await open(
      withAppearance((a) => {
        a.background.source = "gradient";
        a.cardPosition = "right";
      }),
    );
    expect(within(section).getByText("Background")).toBeVisible();
    expect(within(section).getByText("Gradient")).toBeVisible();
    expect(within(section).getByText("Card position")).toBeVisible();
    expect(within(section).getByText("Right")).toBeVisible();
  });

  it.each([
    ["none", "Default"],
    ["color", "Color"],
    ["bing", "Bing"],
    ["unsplash", "Unsplash"],
  ] as const)("names the %s source", async (source, name) => {
    const section = await open(
      withAppearance((a) => {
        a.background.source = source;
      }),
    );
    expect(within(section).getByText(name)).toBeVisible();
  });

  it("counts the uploaded images when they are the background", async () => {
    const section = await open(
      withAppearance((a) => {
        a.background.source = "images";
      }),
      3,
    );
    expect(within(section).getByText("Images (3)")).toBeVisible();
  });

  it("leads to the editor", async () => {
    const user = userEvent.setup();
    const section = await open();
    const edit = within(section).getByRole("link", {
      name: "Edit the sign-in page",
    });
    expect(edit).toHaveAttribute("href", "/admin/settings/sign-in-page");
    await user.click(edit);
    expect(
      await screen.findByRole("heading", { level: 1, name: "Sign-in page" }),
    ).toBeVisible();
  });
});
