import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  loginAppearanceQueryOptions,
  publicConfigQueryOptions,
} from "@/api/queries";
import type { LoginAppearance } from "@/api/raw-paths";
import { configureSudo, resetSudo } from "@/api/sudo";
import { createQueryClient } from "@/app/query-client";
import { defaultLoginAppearance } from "@/components/custom/login-appearance/appearance";
import { i18n } from "@/i18n";
import { SignInPagePanel } from "@/pages/admin/settings/SignInPagePanel";
import { apiError, fakeApi, testConfig } from "@/test/app";

let queryClient: QueryClient;
let api: ReturnType<typeof fakeApi>;

// jsdom's File cannot be the body of the test environment's Request, so the
// upload stops short of the network and only counts what it was given.
const uploadedFiles = vi.hoisted(() => [] as string[]);
vi.mock("@/api/mutations", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/mutations")>();
  return {
    ...actual,
    uploadLoginImageMutationOptions: (
      ...args: Parameters<typeof actual.uploadLoginImageMutationOptions>
    ) => ({
      ...actual.uploadLoginImageMutationOptions(...args),
      mutationFn: async (file: File) => {
        uploadedFiles.push(file.name);
        return { id: 100 + uploadedFiles.length, url: "/x", etag: "x" };
      },
    }),
  };
});

function images(count: number) {
  return Array.from({ length: count }, (_, i) => ({
    id: i + 1,
    url: `/branding/login-images/${i + 1}`,
    etag: `e${i + 1}`,
  }));
}

function setUp({
  appearance = defaultLoginAppearance,
  hasUnsplashKey = false,
  imageCount = 0,
}: {
  appearance?: LoginAppearance;
  hasUnsplashKey?: boolean;
  imageCount?: number;
} = {}) {
  queryClient.setQueryData(publicConfigQueryOptions().queryKey, {
    ...testConfig,
    loginAppearance: appearance,
    loginImages: images(imageCount),
  });
  queryClient.setQueryData(loginAppearanceQueryOptions().queryKey, {
    appearance,
    hasUnsplashKey,
  });
}

function withAppearance(edit: (a: LoginAppearance) => void): LoginAppearance {
  const a = structuredClone(defaultLoginAppearance);
  edit(a);
  return a;
}

beforeEach(() => {
  i18n.activate("en");
  uploadedFiles.length = 0;
  api = fakeApi({
    "GET /api/prohibitorum/me/sudo/methods": () =>
      Response.json({ methods: ["password_totp"], fresh: true }),
    "PUT /api/prohibitorum/admin/settings/login-appearance": () =>
      new Response(null, { status: 204 }),
    "GET /api/prohibitorum/admin/settings/login-appearance/wallpaper": () =>
      apiError("wallpaper_unavailable", 503),
  });
  queryClient = createQueryClient(vi.fn());
  configureSudo({
    queryClient,
    set: () => {},
    setFresh: () => {},
    getFresh: () => true,
  });
});

afterEach(() => {
  resetSudo();
  queryClient.clear();
  vi.unstubAllGlobals();
});

function mount() {
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <SignInPagePanel />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return screen.getByRole("form", { name: "Sign-in page" });
}

function sources() {
  return screen.getByRole("radiogroup", { name: "Background source" });
}

async function choose(user: ReturnType<typeof userEvent.setup>, name: string) {
  await user.click(within(sources()).getByText(name));
}

/** The key's Remove button itself; a tooltip's trigger around it shares the name. */
function removeKeyButton() {
  const button = screen
    .getAllByRole("button", { name: "Remove" })
    .find((element) => element.tagName === "BUTTON");
  if (!button) throw new Error("no Remove button");
  return button;
}

function savedBody() {
  const sent = api.sent(
    "PUT",
    "/api/prohibitorum/admin/settings/login-appearance",
  );
  return sent[sent.length - 1]?.body as
    | { appearance: LoginAppearance; unsplashAccessKey?: string }
    | undefined;
}

describe("sign-in page settings", () => {
  it("keeps each source's settings while another is chosen, and saves them all", async () => {
    setUp();
    const user = userEvent.setup();
    const form = mount();

    await choose(user, "Gradient");
    await user.click(within(form).getByText("Aurora"));
    await choose(user, "Bing");
    await choose(user, "Gradient");
    expect(
      within(screen.getByRole("radiogroup", { name: "Gradient" })).getByRole(
        "radio",
        { name: "Aurora" },
      ),
    ).toBeChecked();

    await user.click(within(form).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(savedBody()).toBeDefined());
    const body = savedBody();
    expect(body?.appearance).toEqual(
      withAppearance((a) => {
        a.background.source = "gradient";
        a.background.gradient = "aurora";
      }),
    );
    expect(body).not.toHaveProperty("unsplashAccessKey");
  });

  it("draws the draft in the preview as it changes", async () => {
    setUp();
    const user = userEvent.setup();
    const form = mount();
    const preview = document.querySelector("[data-login-preview]");
    expect(preview?.querySelector("[data-login-backdrop]")).toBeNull();

    await choose(user, "Gradient");
    expect(
      preview?.querySelector('[data-login-backdrop="gradient"]'),
    ).toHaveAttribute("data-gradient", "lagoon");
    await user.click(within(form).getByText("Ember"));
    expect(
      preview?.querySelector('[data-login-backdrop="gradient"]'),
    ).toHaveAttribute("data-gradient", "ember");
  });

  it("shows opacity and frosted glass only while translucent, and keeps their values", async () => {
    setUp({
      appearance: withAppearance((a) => {
        a.card = { translucent: false, opacity: 35, blur: false };
      }),
    });
    const user = userEvent.setup();
    mount();
    const card = screen.getByRole("group", { name: "Sign-in card" });
    expect(within(card).queryByRole("slider")).not.toBeInTheDocument();
    expect(
      within(card).queryByRole("switch", { name: "Frosted glass" }),
    ).not.toBeInTheDocument();

    await user.click(within(card).getByText("Translucent"));
    expect(within(card).getByRole("slider", { name: "Opacity" })).toHaveValue(
      "35",
    );
    expect(
      within(card).getByRole("switch", { name: "Frosted glass" }),
    ).not.toBeChecked();

    await user.click(within(card).getByText("Translucent"));
    expect(within(card).queryByRole("slider")).not.toBeInTheDocument();
    await user.click(within(card).getByText("Translucent"));
    expect(within(card).getByRole("slider", { name: "Opacity" })).toHaveValue(
      "35",
    );
  });

  it("stops offering to add images at ten", () => {
    setUp({
      appearance: withAppearance((a) => {
        a.background.source = "images";
      }),
      imageCount: 10,
    });
    mount();
    expect(
      screen.queryByRole("button", { name: "Add images" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText("10 of 10 · remove one to add another"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Remove image 10" }),
    ).toBeInTheDocument();
  });

  it("uploads only as many chosen files as there is room for, and says how many were left", async () => {
    setUp({
      appearance: withAppearance((a) => {
        a.background.source = "images";
      }),
      imageCount: 8,
    });
    const user = userEvent.setup();
    mount();
    const input =
      document.querySelector<HTMLInputElement>('input[type="file"]');
    if (!input) throw new Error("no file input");
    const files = ["a", "b", "c", "d"].map(
      (name) => new File(["x"], `${name}.png`, { type: "image/png" }),
    );
    await user.upload(input, files);

    // The first two, in the order they were chosen.
    await waitFor(() => expect(uploadedFiles).toEqual(["a.png", "b.png"]));
    expect(
      await screen.findByText(
        "2 files weren't uploaded because the 10-image limit was reached.",
      ),
    ).toBeInTheDocument();
  });

  it("does not let the key be removed while the saved background is Unsplash", () => {
    setUp({
      appearance: withAppearance((a) => {
        a.background.source = "unsplash";
      }),
      hasUnsplashKey: true,
    });
    mount();
    expect(screen.getByText("Access key saved")).toBeInTheDocument();
    expect(removeKeyButton()).toBeDisabled();
    expect(screen.getByRole("button", { name: "Replace" })).toBeEnabled();
  });

  it("lets an unused key be removed", () => {
    setUp({ hasUnsplashKey: true });
    const user = userEvent.setup();
    mount();
    return (async () => {
      await choose(user, "Unsplash");
      expect(removeKeyButton()).toBeEnabled();
    })();
  });

  it("sends a typed key with the save, and asks for one when none is saved", async () => {
    setUp();
    const user = userEvent.setup();
    const form = mount();
    await choose(user, "Unsplash");

    await user.click(within(form).getByRole("button", { name: "Save" }));
    expect(
      await screen.findByText(
        "Enter the access key of your Unsplash application.",
      ),
    ).toBeInTheDocument();
    expect(savedBody()).toBeUndefined();

    await user.type(within(form).getByLabelText("Access key"), "Good_key-1");
    await user.click(within(form).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(savedBody()).toBeDefined());
    expect(savedBody()).toMatchObject({
      unsplashAccessKey: "Good_key-1",
      appearance: { background: { source: "unsplash" } },
    });
  });

  it("puts Unsplash's refusal of the key under the key field", async () => {
    setUp();
    api.routes["PUT /api/prohibitorum/admin/settings/login-appearance"] = () =>
      apiError("unsplash_key_invalid", 400);
    const user = userEvent.setup();
    const form = mount();
    await choose(user, "Unsplash");
    await user.type(within(form).getByLabelText("Access key"), "Wrong_key");
    await user.click(within(form).getByRole("button", { name: "Save" }));
    expect(
      await screen.findByText(
        "Unsplash did not accept this access key. Check it and enter it again.",
      ),
    ).toBeInTheDocument();
    expect(within(form).getByLabelText("Access key")).toHaveAttribute(
      "aria-invalid",
      "true",
    );
  });

  it("offers eight suggested colours", async () => {
    setUp();
    const user = userEvent.setup();
    mount();
    await choose(user, "Color");
    const suggestions = screen.getByRole("listbox", {
      name: "Suggested colors",
    });
    // Each swatch is named after its colour, as React Aria describes it.
    const names = within(suggestions)
      .getAllByRole("option")
      .map((option) =>
        option.querySelector("[aria-label]")?.getAttribute("aria-label"),
      );
    expect(names.every((name) => name)).toBe(true);
    expect(names).toHaveLength(8);
    expect(new Set(names).size).toBe(8);
  });
});
