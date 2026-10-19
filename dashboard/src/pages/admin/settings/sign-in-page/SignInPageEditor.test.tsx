import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { LoginAppearance } from "@/api/raw-paths";
import { createQueryClient } from "@/app/query-client";
import { defaultLoginAppearance } from "@/components/custom/login-appearance/appearance";
import { i18n } from "@/i18n";
import {
  apiError,
  type FakeApi,
  fakeApi,
  publicApi,
  renderApp,
  testConfig,
  testSession,
} from "@/test/app";

let queryClient: QueryClient;
let notifyError: ReturnType<typeof vi.fn<(error: unknown) => void>>;

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

const base = "/api/prohibitorum";
const appearancePath = `${base}/admin/settings/login-appearance`;

beforeEach(() => {
  i18n.activate("en");
  uploadedFiles.length = 0;
  notifyError = vi.fn<(error: unknown) => void>();
  // No sudo runner is configured: saving the sign-in page must not ask for
  // one, and `runWithSudo` would throw if it did.
  queryClient = createQueryClient(notifyError);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function withAppearance(edit: (a: LoginAppearance) => void): LoginAppearance {
  const a = structuredClone(defaultLoginAppearance);
  edit(a);
  return a;
}

function images(count: number) {
  return Array.from({ length: count }, (_, i) => ({
    id: i + 1,
    url: `/branding/login-images/${i + 1}`,
    etag: `e${i + 1}`,
  }));
}

/** What the server holds, so a save is read back as saved. */
interface Stored {
  appearance: LoginAppearance;
  hasUnsplashKey: boolean;
}

async function open({
  appearance = defaultLoginAppearance,
  hasUnsplashKey = false,
  imageCount = 0,
  routes = {},
}: {
  appearance?: LoginAppearance;
  hasUnsplashKey?: boolean;
  imageCount?: number;
  routes?: Parameters<typeof fakeApi>[0];
} = {}) {
  const stored: Stored = { appearance, hasUnsplashKey };
  const api: FakeApi = fakeApi({
    ...publicApi(
      { ...testSession, role: "admin" },
      {
        ...testConfig,
        loginAppearance: appearance,
        loginImages: images(imageCount),
      },
    ),
    [`GET ${appearancePath}`]: () => Response.json(stored),
    [`PUT ${appearancePath}`]: async (request) => {
      const body = (await request.json()) as {
        appearance: LoginAppearance;
        unsplashAccessKey?: string;
      };
      stored.appearance = body.appearance;
      if (body.unsplashAccessKey) stored.hasUnsplashKey = true;
      return new Response(null, { status: 204 });
    },
    [`GET ${appearancePath}/wallpaper`]: () =>
      apiError("wallpaper_unavailable", 503),
    ...routes,
  });
  renderApp("/admin/settings/sign-in-page", queryClient);
  await screen.findByRole("heading", { level: 1, name: "Sign-in page" });
  return api;
}

/** The block a heading names: its section, with the controls and footer inside. */
function frame(name: string): HTMLElement {
  const section = screen
    .getByRole("heading", { level: 2, name })
    .closest("section");
  if (!section) throw new Error(`no section for ${name}`);
  return section;
}

function savedBody(api: FakeApi) {
  const sent = api.sent("PUT", appearancePath);
  return sent[sent.length - 1]?.body as
    | { appearance: LoginAppearance; unsplashAccessKey?: string }
    | undefined;
}

async function chooseSource(
  user: ReturnType<typeof userEvent.setup>,
  name: string,
) {
  const sources = screen.getByRole("radiogroup", {
    name: "Background source",
  });
  await user.click(within(sources).getByRole("radio", { name }));
}

const backLink = () => screen.getByRole("link", { name: "Back to Settings" });

const saveButton = () => screen.getByRole("button", { name: "Save" });

/** The sign-in page itself, which the preview draws in a frame. */
async function previewDocument(): Promise<Document> {
  const preview = document.querySelector<HTMLIFrameElement>(
    "iframe[data-sign-in-preview]",
  );
  if (!preview) throw new Error("no preview");
  await waitFor(() => {
    if (!preview.contentDocument?.querySelector("main"))
      throw new Error("not drawn yet");
  });
  return preview.contentDocument as Document;
}

describe("the page", () => {
  it("is headed Sign-in page, says what it is for and leads back to Settings", async () => {
    await open();
    expect(
      screen.getByText(
        "What people see before they sign in. The preview shows your changes as you make them.",
      ),
    ).toBeVisible();
    expect(backLink()).toHaveAttribute("href", "/admin/settings");
  });

  it("groups its controls as Background, Card and Toolbar beside the preview", async () => {
    await open();
    for (const name of ["Background", "Card", "Toolbar", "Preview"])
      expect(frame(name)).toBeVisible();
    expect(
      within(frame("Card")).getByRole("radiogroup", { name: "Position" }),
    ).toBeVisible();
    expect(
      within(frame("Toolbar")).getByRole("switch", { name: "Translucent" }),
    ).toBeChecked();
  });

  it("names the page in the console's breadcrumb under Settings", async () => {
    await open();
    const crumb = screen.getByRole("navigation", { name: "Breadcrumb" });
    expect(within(crumb).getByText("Sign-in page")).toBeVisible();
  });
});

describe("the background source", () => {
  it("offers the six sources", async () => {
    await open();
    const sources = screen.getByRole("radiogroup", {
      name: "Background source",
    });
    expect(
      within(sources)
        .getAllByRole("radio")
        .map((radio) => radio.textContent),
    ).toEqual(["Default", "Color", "Gradient", "Bing", "Unsplash", "Images"]);
    expect(
      within(sources).getByRole("radio", { name: "Default" }),
    ).toBeChecked();
    expect(
      screen.getByText(
        "The page's own background, which follows light and dark mode.",
      ),
    ).toBeVisible();
  });

  it("keeps each source's settings while another is chosen, and saves them all", async () => {
    const user = userEvent.setup();
    const api = await open();

    await chooseSource(user, "Gradient");
    await user.click(screen.getByRole("radio", { name: "Aurora" }));
    await chooseSource(user, "Bing");
    await chooseSource(user, "Gradient");
    expect(screen.getByRole("radio", { name: "Aurora" })).toBeChecked();

    await user.click(saveButton());
    await waitFor(() => expect(savedBody(api)).toBeDefined());
    expect(savedBody(api)?.appearance).toEqual(
      withAppearance((a) => {
        a.background.source = "gradient";
        a.background.gradient = "aurora";
      }),
    );
    expect(savedBody(api)).not.toHaveProperty("unsplashAccessKey");
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("offers Bing's region and the picture's caption, and saves both", async () => {
    const user = userEvent.setup();
    const api = await open();
    await chooseSource(user, "Bing");
    expect(
      screen.getByRole("switch", {
        name: "Show the picture's title and copyright",
      }),
    ).toBeChecked();
    await user.click(
      screen.getByRole("switch", {
        name: "Show the picture's title and copyright",
      }),
    );
    await user.click(saveButton());
    await waitFor(() => expect(savedBody(api)).toBeDefined());
    expect(savedBody(api)?.appearance.background.bing).toEqual({
      market: "zh-CN",
      showCaption: false,
    });
  });

  it("takes a colour as hex, refuses one that is not a colour and saves it in lowercase", async () => {
    const user = userEvent.setup();
    const api = await open();
    await chooseSource(user, "Color");
    const hex = screen.getByRole("textbox", { name: "Color" });
    await user.clear(hex);
    await user.type(hex, "#12");
    await user.click(saveButton());
    expect(
      await screen.findByText(
        "Enter a color as # followed by six hexadecimal digits.",
      ),
    ).toBeVisible();
    expect(savedBody(api)).toBeUndefined();

    await user.clear(hex);
    await user.type(hex, "#ABCDEF");
    await user.click(saveButton());
    await waitFor(() => expect(savedBody(api)).toBeDefined());
    expect(savedBody(api)?.appearance.background.color).toBe("#abcdef");
  });
});

describe("the Unsplash source", () => {
  const unsplash = withAppearance((a) => {
    a.background.source = "unsplash";
  });

  it("sends a typed key with the save, and asks for one when none is saved", async () => {
    const user = userEvent.setup();
    const api = await open();
    await chooseSource(user, "Unsplash");

    await user.click(saveButton());
    expect(
      await screen.findByText(
        "Enter the access key of your Unsplash application.",
      ),
    ).toBeVisible();
    expect(savedBody(api)).toBeUndefined();

    await user.type(screen.getByLabelText("Access key"), "Good_key-1");
    await user.click(saveButton());
    await waitFor(() => expect(savedBody(api)).toBeDefined());
    expect(savedBody(api)).toMatchObject({
      unsplashAccessKey: "Good_key-1",
      appearance: { background: { source: "unsplash" } },
    });
  });

  it("refuses a key with characters Unsplash does not use", async () => {
    const user = userEvent.setup();
    const api = await open();
    await chooseSource(user, "Unsplash");
    await user.type(screen.getByLabelText("Access key"), "has space");
    await user.click(saveButton());
    expect(
      await screen.findByText(
        "An access key has only letters, digits, hyphens and underscores, up to 128 of them.",
      ),
    ).toBeVisible();
    expect(savedBody(api)).toBeUndefined();
  });

  it("puts Unsplash's refusal of the key under the key field, reported once and not as a toast", async () => {
    const user = userEvent.setup();
    const api = await open({
      routes: {
        [`PUT ${appearancePath}`]: () => apiError("unsplash_key_invalid", 400),
      },
    });
    await chooseSource(user, "Unsplash");
    await user.type(screen.getByLabelText("Access key"), "Wrong_key");
    await user.click(saveButton());
    expect(
      await screen.findByText(
        "Unsplash did not accept this access key. Check it and enter it again.",
      ),
    ).toBeVisible();
    expect(screen.getByLabelText("Access key")).toHaveAttribute(
      "aria-invalid",
      "true",
    );
    expect(api.sent("PUT", appearancePath)).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("does not let the saved key be removed while the saved background is Unsplash, and says why", async () => {
    await open({ appearance: unsplash, hasUnsplashKey: true });
    expect(screen.getByText("Access key saved")).toBeVisible();
    const remove = screen.getByRole("button", { name: "Remove" });
    expect(remove).toBeDisabled();
    expect(remove).toHaveAccessibleDescription(
      "Switch the background to another source and save before removing the key.",
    );
    expect(screen.getByRole("button", { name: "Replace" })).toBeEnabled();
  });

  it("lets an unused key be removed at once, without Save", async () => {
    const user = userEvent.setup();
    const api = await open({
      hasUnsplashKey: true,
      routes: {
        [`DELETE ${appearancePath}/unsplash-key`]: () =>
          new Response(null, { status: 204 }),
      },
    });
    await chooseSource(user, "Unsplash");
    const remove = screen.getByRole("button", { name: "Remove" });
    expect(remove).toBeEnabled();
    expect(
      screen.queryByText(
        "Switch the background to another source and save before removing the key.",
      ),
    ).not.toBeInTheDocument();
    await user.click(remove);
    await waitFor(() =>
      expect(api.sent("DELETE", `${appearancePath}/unsplash-key`)).toHaveLength(
        1,
      ),
    );
    expect(api.sent("PUT", appearancePath)).toHaveLength(0);
  });

  it("asks for a new key when the saved one is replaced", async () => {
    const user = userEvent.setup();
    await open({ appearance: unsplash, hasUnsplashKey: true });
    expect(screen.queryByLabelText("Access key")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Replace" }));
    expect(screen.getByLabelText("Access key")).toHaveValue("");
  });

  it("offers a random photo or a carousel, and saves the interval with it", async () => {
    const user = userEvent.setup();
    const api = await open({ appearance: unsplash, hasUnsplashKey: true });
    const order = screen.getByRole("radiogroup", { name: "Order" });
    expect(
      within(order).getByRole("radio", { name: "A random one each visit" }),
    ).toBeChecked();
    expect(screen.queryByLabelText("Change every")).toBeNull();

    await user.click(within(order).getByRole("radio", { name: "Carousel" }));
    const interval = screen.getByRole("textbox", { name: "Change every" });
    expect(interval).toHaveValue("10 seconds");
    await user.clear(interval);
    await user.type(interval, "25");
    await user.tab();

    await user.click(saveButton());
    await waitFor(() => expect(savedBody(api)).toBeDefined());
    expect(savedBody(api)?.appearance.background.unsplash).toEqual({
      query: "",
      order: "carousel",
      intervalSeconds: 25,
    });
    // The uploaded images keep their own setting.
    expect(savedBody(api)?.appearance.background.images).toEqual(
      defaultLoginAppearance.background.images,
    );
  });

  it("refuses to save without an interval and says what it takes", async () => {
    const user = userEvent.setup();
    const api = await open({
      appearance: withAppearance((a) => {
        a.background.source = "unsplash";
        a.background.unsplash.order = "carousel";
      }),
      hasUnsplashKey: true,
    });
    const interval = screen.getByRole("textbox", { name: "Change every" });
    await user.clear(interval);
    await user.tab();
    await user.click(saveButton());
    expect(
      await screen.findByText("Choose between 5 and 3600 seconds."),
    ).toBeVisible();
    expect(interval).toHaveAttribute("aria-invalid", "true");
    expect(savedBody(api)).toBeUndefined();

    await user.type(interval, "30");
    await user.tab();
    await user.click(saveButton());
    await waitFor(() => expect(savedBody(api)).toBeDefined());
    expect(savedBody(api)?.appearance.background.unsplash.intervalSeconds).toBe(
      30,
    );
  });

  it("refuses a keyword with spaces at its ends, and a hidden one that cannot be used", async () => {
    const user = userEvent.setup();
    const api = await open({
      appearance: withAppearance((a) => {
        a.background.unsplash.query = " bad ";
      }),
    });
    // Another source is chosen: the keyword is out of sight but still saved.
    await user.click(
      within(frame("Card")).getByRole("radio", { name: "Left" }),
    );
    await user.click(saveButton());
    expect(
      await screen.findByText(
        "The Unsplash keyword can't be used. Choose Unsplash to correct it.",
      ),
    ).toBeVisible();
    expect(savedBody(api)).toBeUndefined();

    await chooseSource(user, "Unsplash");
    await user.click(saveButton());
    expect(
      await screen.findByText("Remove the spaces at the start or end."),
    ).toBeVisible();
  });
});

describe("the Images source", () => {
  const imagesSource = withAppearance((a) => {
    a.background.source = "images";
  });

  it("applies uploads at once, without Save and without counting as a change", async () => {
    const user = userEvent.setup();
    const api = await open({ appearance: imagesSource, imageCount: 3 });
    const input =
      document.querySelector<HTMLInputElement>('input[type="file"]');
    if (!input) throw new Error("no file input");
    await user.upload(input, new File(["x"], "a.png", { type: "image/png" }));
    await waitFor(() => expect(uploadedFiles).toEqual(["a.png"]));
    expect(api.sent("PUT", appearancePath)).toHaveLength(0);
    expect(screen.queryByText(/unsaved change/)).not.toBeInTheDocument();
  });

  it("offers order and interval only with two or more images", async () => {
    await open({ appearance: imagesSource, imageCount: 1 });
    expect(screen.queryByRole("radiogroup", { name: "Order" })).toBeNull();
  });

  it("offers uploaded images the same controls", async () => {
    await open({
      appearance: withAppearance((a) => {
        a.background.source = "images";
        a.background.images = { order: "carousel", intervalSeconds: 3600 };
      }),
      imageCount: 2,
    });
    expect(screen.getByRole("textbox", { name: "Change every" })).toHaveValue(
      "3,600 seconds",
    );
  });

  it("stops offering to add images at ten", async () => {
    await open({ appearance: imagesSource, imageCount: 10 });
    expect(
      screen.queryByRole("group", {
        name: "Drop images here, or choose files",
      }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText("10 of 10 · remove one to add another"),
    ).toBeVisible();
  });

  it("draws the images in the preview", async () => {
    await open({ appearance: imagesSource, imageCount: 2 });
    const page = await previewDocument();
    expect(
      page.querySelector('[data-login-backdrop="pictures"]'),
    ).not.toBeNull();
  });
});

describe("the card and the toolbar", () => {
  it("saves the card's position and a forced theme with the rest", async () => {
    const user = userEvent.setup();
    const api = await open();
    const card = frame("Card");
    expect(within(card).getByRole("radio", { name: "Center" })).toBeChecked();
    await user.click(within(card).getByRole("radio", { name: "Right" }));
    expect(
      within(card).getByRole("radio", { name: "Visitor's choice" }),
    ).toBeChecked();
    await user.click(within(card).getByRole("radio", { name: "Always dark" }));

    await user.click(saveButton());
    await waitFor(() => expect(savedBody(api)).toBeDefined());
    expect(savedBody(api)?.appearance).toEqual(
      withAppearance((a) => {
        a.cardPosition = "right";
        a.theme = "dark";
      }),
    );
  });

  it("keeps the card's and the toolbar's surfaces apart", async () => {
    const user = userEvent.setup();
    const api = await open();
    const card = frame("Card");
    const toolbar = frame("Toolbar");
    expect(
      within(card).getByRole("switch", { name: "Translucent" }),
    ).not.toBeChecked();
    expect(
      within(toolbar).getByRole("switch", { name: "Translucent" }),
    ).toBeChecked();

    await user.click(within(card).getByRole("switch", { name: "Translucent" }));
    await user.click(
      within(toolbar).getByRole("switch", { name: "Frosted glass" }),
    );

    await user.click(saveButton());
    await waitFor(() => expect(savedBody(api)).toBeDefined());
    expect(savedBody(api)?.appearance).toEqual(
      withAppearance((a) => {
        a.card.translucent = true;
        a.capsules.blur = false;
      }),
    );
  });
});

describe("the preview", () => {
  it("draws the draft as it changes", async () => {
    const user = userEvent.setup();
    await open();
    const page = await previewDocument();
    expect(page.querySelector("[data-login-backdrop]")).toBeNull();

    await chooseSource(user, "Gradient");
    await waitFor(() =>
      expect(
        page.querySelector('[data-login-backdrop="gradient"]'),
      ).toHaveAttribute("data-gradient", "lagoon"),
    );
    await user.click(screen.getByRole("radio", { name: "Ember" }));
    await waitFor(() =>
      expect(
        page.querySelector('[data-login-backdrop="gradient"]'),
      ).toHaveAttribute("data-gradient", "ember"),
    );
  });

  it("places the card as the draft does", async () => {
    const user = userEvent.setup();
    await open();
    const page = await previewDocument();
    expect(page.querySelector("main")).toHaveAttribute(
      "data-position",
      "center",
    );
    await user.click(
      within(frame("Card")).getByRole("radio", { name: "Left" }),
    );
    await waitFor(() =>
      expect(page.querySelector("main")).toHaveAttribute(
        "data-position",
        "left",
      ),
    );
  });

  it("lets the administrator look at either theme until the draft forces one", async () => {
    const user = userEvent.setup();
    await open();
    const page = await previewDocument();
    const preview = frame("Preview");
    await user.click(within(preview).getByRole("radio", { name: "Dark" }));
    await waitFor(() => expect(page.documentElement).toHaveClass("dark"));
    expect(document.documentElement).not.toHaveClass("dark");

    const card = frame("Card");
    await user.click(within(card).getByRole("radio", { name: "Always light" }));
    await waitFor(() => expect(page.documentElement).not.toHaveClass("dark"));
    expect(within(preview).queryByRole("radio", { name: "Dark" })).toBeNull();

    await user.click(
      within(card).getByRole("radio", { name: "Visitor's choice" }),
    );
    expect(within(preview).getByRole("radio", { name: "Dark" })).toBeChecked();
    await waitFor(() => expect(page.documentElement).toHaveClass("dark"));
  });

  it("shows the page at desktop or phone width", async () => {
    const user = userEvent.setup();
    await open();
    await previewDocument();
    const preview = frame("Preview");
    const pageFrame = () =>
      document.querySelector<HTMLIFrameElement>("iframe[data-sign-in-preview]");
    expect(
      within(preview).getByRole("radio", { name: "Desktop" }),
    ).toBeChecked();
    expect(pageFrame()?.style.width).toBe("1280px");
    await user.click(within(preview).getByRole("radio", { name: "Phone" }));
    expect(pageFrame()?.style.width).toBe("390px");
  });

  it("asks for a saved key before it can show Unsplash photos", async () => {
    const user = userEvent.setup();
    await open();
    await chooseSource(user, "Unsplash");
    expect(
      screen.getByText("Save an access key to preview Unsplash photos."),
    ).toBeVisible();
  });
});

describe("saving and discarding", () => {
  it("has nothing to save or discard until something changes", async () => {
    await open();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Discard" })).toBeNull();
    expect(screen.queryByText(/unsaved change/)).toBeNull();
  });

  it("counts the unsaved changes in the preview's footer", async () => {
    const user = userEvent.setup();
    await open();
    const preview = frame("Preview");
    const card = frame("Card");
    await user.click(within(card).getByRole("radio", { name: "Left" }));
    expect(within(preview).getByText("1 unsaved change")).toBeVisible();
    await user.click(within(card).getByRole("radio", { name: "Always dark" }));
    expect(within(preview).getByText("2 unsaved changes")).toBeVisible();
    await user.click(within(card).getByRole("radio", { name: "Center" }));
    expect(within(preview).getByText("1 unsaved change")).toBeVisible();
  });

  it("counts a typed access key as a change", async () => {
    const user = userEvent.setup();
    await open();
    await chooseSource(user, "Unsplash");
    await user.type(screen.getByLabelText("Access key"), "abc");
    expect(screen.getByText("2 unsaved changes")).toBeVisible();
  });

  it("puts every control back as saved on Discard", async () => {
    const user = userEvent.setup();
    const api = await open();
    const card = frame("Card");
    await chooseSource(user, "Gradient");
    await user.click(within(card).getByRole("radio", { name: "Right" }));
    await user.click(screen.getByRole("button", { name: "Discard" }));

    expect(within(card).getByRole("radio", { name: "Center" })).toBeChecked();
    expect(screen.getByRole("radio", { name: "Default" })).toBeChecked();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
    expect(api.sent("PUT", appearancePath)).toHaveLength(0);
  });

  it("shows the key field again as saved after Discard", async () => {
    const user = userEvent.setup();
    await open({
      appearance: withAppearance((a) => {
        a.background.source = "unsplash";
      }),
      hasUnsplashKey: true,
    });
    await user.click(screen.getByRole("button", { name: "Replace" }));
    await user.type(screen.getByLabelText("Access key"), "abc");
    await user.click(screen.getByRole("button", { name: "Discard" }));
    expect(screen.getByText("Access key saved")).toBeVisible();
  });

  it("reads as saved once the save lands", async () => {
    const user = userEvent.setup();
    const api = await open();
    await user.click(
      within(frame("Card")).getByRole("radio", { name: "Left" }),
    );
    await user.click(saveButton());
    await waitFor(() => expect(savedBody(api)).toBeDefined());
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Save" })).toBeNull(),
    );
    expect(screen.queryByText(/unsaved change/)).toBeNull();
  });

  it("reports a failed save once and keeps the changes", async () => {
    const user = userEvent.setup();
    await open({
      routes: {
        [`PUT ${appearancePath}`]: () => apiError("internal_error", 500),
      },
    });
    await user.click(
      within(frame("Card")).getByRole("radio", { name: "Left" }),
    );
    await user.click(saveButton());
    await waitFor(() => expect(notifyError).toHaveBeenCalledTimes(1));
    expect(saveButton()).toBeEnabled();
    expect(screen.getByText("1 unsaved change")).toBeVisible();
  });
});

describe("leaving with changes", () => {
  it("asks before leaving, and stays when the answer is no", async () => {
    const user = userEvent.setup();
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    await open();
    await user.click(
      within(frame("Card")).getByRole("radio", { name: "Left" }),
    );
    await user.click(backLink());
    expect(confirm).toHaveBeenCalledTimes(1);
    expect(
      screen.getByRole("heading", { level: 1, name: "Sign-in page" }),
    ).toBeVisible();
  });

  it("leaves without asking when nothing changed", async () => {
    const user = userEvent.setup();
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    await open();
    await user.click(backLink());
    expect(confirm).not.toHaveBeenCalled();
  });
});
