import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import { configureSudo, resetSudo, type SudoRequest } from "@/api/sudo";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { PasskeysPanel } from "@/pages/security/PasskeysPanel";

vi.mock("@simplewebauthn/browser", () => ({
  browserSupportsWebAuthn: vi.fn(() => true),
  startRegistration: vi.fn(),
  WebAuthnError: class extends Error {
    code = "";
  },
}));

const { startRegistration } = await import("@simplewebauthn/browser");

type Credential = components["schemas"]["CredentialView"];

const hoursAgo = (hours: number) =>
  new Date(Date.now() - hours * 3_600_000).toISOString();

const macbook: Credential = {
  id: 1,
  nickname: "MacBook Pro",
  createdAt: hoursAgo(72),
  lastUsedAt: hoursAgo(2),
  backupState: true,
  credentialIdSuffix: "aa11",
  attestationType: "none",
  transports: ["internal", "hybrid"],
};
const yubikey: Credential = {
  id: 2,
  createdAt: hoursAgo(24 * 40),
  backupState: false,
  credentialIdSuffix: "bb22",
  attestationType: "none",
  transports: ["usb"],
};

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifySuccess = vi.fn<(message: MessageDescriptor) => void>();
const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifySuccess.mockReset();
  notifyError.mockReset();
  vi.mocked(startRegistration).mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  vi.stubGlobal("isSecureContext", true);
  queryClient = createQueryClient(notifyError, notifySuccess);
});

afterEach(() => {
  resetSudo();
  queryClient.clear();
  vi.unstubAllGlobals();
});

function freshSudo() {
  configureSudo({
    queryClient,
    set: () => {},
    setFresh: () => {},
    getFresh: () => true,
  });
}

/** The writes the panel made, by the end of the path. */
function writes(suffix: string) {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter(
      (request) =>
        request.method === "POST" &&
        new URL(request.url).pathname.endsWith(suffix),
    );
}

function mount(
  credentials: Credential[],
  extra?: (request: Request) => Response | undefined,
) {
  fetchBoundary.mockImplementation(async (request) => {
    const canned = extra?.(request);
    if (canned) return canned;
    const path = new URL(request.url).pathname;
    if (path.endsWith("/me/credentials")) return Response.json(credentials);
    if (path.endsWith("/me/sudo/methods")) {
      return Response.json({ methods: ["webauthn"], fresh: true });
    }
    if (path.endsWith("/credentials/register/begin")) {
      return Response.json({ publicKey: { challenge: "c" } });
    }
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <PasskeysPanel />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

async function rows() {
  const list = await screen.findByRole("list", { name: "Passkeys" });
  return within(list).getAllByRole("listitem");
}

async function openMenu(user: UserEvent, row: HTMLElement) {
  await user.click(
    within(row).getByRole("button", { name: /^More actions for / }),
  );
  return screen.findByRole("menu");
}

describe("the passkey rows", () => {
  it("shows the nickname, the sync badge, the use and the suffix", async () => {
    mount([macbook, yubikey]);

    const [first, second] = (await rows()) as [HTMLElement, HTMLElement];
    expect(first).toHaveTextContent("MacBook Pro");
    expect(first).toHaveTextContent("Synced");
    expect(first).toHaveTextContent("Added 3 days ago");
    expect(first).toHaveTextContent("…aa11");
    expect(first.querySelector("[data-slot='item-meta']")).toHaveTextContent(
      "Used 2 hours ago",
    );

    expect(second).toHaveTextContent("Unnamed passkey");
    expect(second).toHaveTextContent("Single device");
    expect(second.querySelector("[data-slot='item-meta']")).toHaveTextContent(
      "Not used yet",
    );
  });

  it("keeps the heading and its description around the list", async () => {
    mount([macbook, yubikey]);

    await rows();
    expect(screen.getByRole("heading", { name: "Passkeys" })).toBeVisible();
    expect(
      screen.getByText(
        "Sign in with a passkey. It is convenient, and there is no password to leak. Passkeys are tied to your devices.",
      ),
    ).toBeVisible();
  });

  it("offers Rename and Remove in the row's menu", async () => {
    const user = userEvent.setup();
    mount([macbook, yubikey]);

    const [first] = await rows();
    await openMenu(user, first as HTMLElement);

    expect(screen.getByRole("menuitem", { name: "Rename" })).toBeEnabled();
    expect(
      screen.getByRole("menuitem", { name: "Remove" }),
    ).not.toHaveAttribute("aria-disabled", "true");
  });

  it("disables Remove for the only passkey and says why in the row", async () => {
    const user = userEvent.setup();
    mount([macbook]);

    const [only] = await rows();
    expect(only).toHaveTextContent("Your only passkey");
    await openMenu(user, only as HTMLElement);

    expect(screen.getByRole("menuitem", { name: "Remove" })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
    expect(screen.getByRole("menuitem", { name: "Rename" })).toBeEnabled();
  });

  it("does not call a row the only one while there are two", async () => {
    mount([macbook, yubikey]);

    await rows();
    expect(screen.queryByText("Your only passkey")).not.toBeInTheDocument();
  });

  it("shows an empty state with no row list when there are none", async () => {
    mount([]);

    expect(await screen.findByText("No passkeys yet")).toBeVisible();
    expect(
      screen.queryByRole("list", { name: "Passkeys" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add a passkey" })).toBeEnabled();
  });
});

describe("adding a passkey", () => {
  it("runs the ceremony, completes it and reloads the list", async () => {
    freshSudo();
    const user = userEvent.setup();
    vi.mocked(startRegistration).mockResolvedValue({
      id: "new",
    } as unknown as Awaited<ReturnType<typeof startRegistration>>);
    mount([macbook, yubikey]);

    await rows();
    await user.click(screen.getByRole("button", { name: "Add a passkey" }));

    await waitFor(() =>
      expect(writes("/credentials/register/complete")).toHaveLength(1),
    );
    expect(startRegistration).toHaveBeenCalledTimes(1);
    await waitFor(() =>
      expect(notifySuccess).toHaveBeenCalledWith(
        expect.objectContaining({ id: "success.passkey.added" }),
      ),
    );
  });

  it("reports a failed ceremony once, inline, with no toast", async () => {
    freshSudo();
    const user = userEvent.setup();
    vi.mocked(startRegistration).mockRejectedValue(new Error("device error"));
    mount([macbook, yubikey]);

    await rows();
    await user.click(screen.getByRole("button", { name: "Add a passkey" }));

    expect(
      await screen.findByText("The passkey could not be created."),
    ).toBeVisible();
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
    expect(writes("/credentials/register/complete")).toHaveLength(0);
  });

  it("says nothing when the browser prompt is dismissed", async () => {
    freshSudo();
    const user = userEvent.setup();
    const dismissed = new Error("dismissed");
    dismissed.name = "NotAllowedError";
    vi.mocked(startRegistration).mockRejectedValue(dismissed);
    mount([macbook, yubikey]);

    await rows();
    await user.click(screen.getByRole("button", { name: "Add a passkey" }));

    await waitFor(() => expect(startRegistration).toHaveBeenCalled());
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("renaming a passkey", () => {
  it("opens with the current name and saves the new one", async () => {
    freshSudo();
    const user = userEvent.setup();
    mount([macbook, yubikey]);

    const [first] = await rows();
    await openMenu(user, first as HTMLElement);
    await user.click(screen.getByRole("menuitem", { name: "Rename" }));

    const dialog = await screen.findByRole("dialog", {
      name: "Rename this passkey",
    });
    const field = within(dialog).getByRole("textbox", { name: "Name" });
    expect(field).toHaveValue("MacBook Pro");
    expect(
      within(dialog).getByText(
        "Use a name that is easy to identify, such as the name of the device it is on.",
      ),
    ).toBeVisible();

    await user.clear(field);
    await user.type(field, "Work laptop");
    await user.click(within(dialog).getByRole("button", { name: "Save name" }));

    await waitFor(() => expect(writes("/credentials/rename")).toHaveLength(1));
    expect(await writes("/credentials/rename")[0]?.json()).toEqual({
      id: 1,
      nickname: "Work laptop",
    });
    await waitFor(() =>
      expect(
        screen.queryByRole("dialog", { name: "Rename this passkey" }),
      ).not.toBeInTheDocument(),
    );
  });

  it("closes without sending anything on Cancel", async () => {
    const user = userEvent.setup();
    mount([macbook, yubikey]);

    const [first] = await rows();
    await openMenu(user, first as HTMLElement);
    await user.click(screen.getByRole("menuitem", { name: "Rename" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Rename this passkey",
    });
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));

    await waitFor(() =>
      expect(
        screen.queryByRole("dialog", { name: "Rename this passkey" }),
      ).not.toBeInTheDocument(),
    );
    expect(writes("/credentials/rename")).toHaveLength(0);
  });

  it("shows a failed save inline, once", async () => {
    const user = userEvent.setup();
    mount([macbook, yubikey], (request) =>
      new URL(request.url).pathname.endsWith("/credentials/rename")
        ? Response.json(
            { code: "rate_limited", requestId: "r2" },
            { status: 429 },
          )
        : undefined,
    );

    const [first] = await rows();
    await openMenu(user, first as HTMLElement);
    await user.click(screen.getByRole("menuitem", { name: "Rename" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Rename this passkey",
    });
    await user.click(within(dialog).getByRole("button", { name: "Save name" }));

    expect(
      await screen.findByText("Too many requests. Wait a moment."),
    ).toBeVisible();
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(
      screen.queryByRole("dialog", { name: "Rename this passkey" }),
    ).not.toBeInTheDocument();
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("stays open through Escape while a save is pending, then reports a failure once", async () => {
    const user = userEvent.setup();
    let fail: () => void = () => undefined;
    mount([macbook, yubikey]);
    const base = fetchBoundary.getMockImplementation();
    fetchBoundary.mockImplementation(async (request) => {
      if (new URL(request.url).pathname.endsWith("/credentials/rename")) {
        await new Promise<void>((resolve) => {
          fail = resolve;
        });
        return Response.json(
          { code: "rate_limited", requestId: "r3" },
          { status: 429 },
        );
      }
      return base ? base(request) : new Response(null, { status: 404 });
    });

    const [first] = await rows();
    await openMenu(user, first as HTMLElement);
    await user.click(screen.getByRole("menuitem", { name: "Rename" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Rename this passkey",
    });
    await user.click(within(dialog).getByRole("button", { name: "Save name" }));
    await waitFor(() => expect(writes("/credentials/rename")).toHaveLength(1));
    await user.keyboard("{Escape}");
    expect(dialog).toBeInTheDocument();
    expect(
      within(dialog).getByRole("button", { name: "Close" }),
    ).toBeDisabled();
    expect(
      within(dialog).getByRole("button", { name: "Cancel" }),
    ).toBeDisabled();
    expect(document.querySelectorAll('[data-slot="spinner"]')).toHaveLength(1);
    fail();

    expect(
      await screen.findByText("Too many requests. Wait a moment."),
    ).toBeVisible();
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(
      screen.queryByRole("dialog", { name: "Rename this passkey" }),
    ).not.toBeInTheDocument();
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("removing a passkey", () => {
  it("keeps the question open and pending until the removal settles", async () => {
    freshSudo();
    const user = userEvent.setup();
    mount([macbook, yubikey]);
    const base = fetchBoundary.getMockImplementation();
    fetchBoundary.mockImplementation((request) =>
      new URL(request.url).pathname.endsWith("/credentials/delete")
        ? new Promise<Response>(() => {})
        : base
          ? base(request)
          : Promise.resolve(new Response(null, { status: 404 })),
    );

    const [first] = await rows();
    await openMenu(user, first as HTMLElement);
    await user.click(screen.getByRole("menuitem", { name: "Remove" }));
    const dialog = await screen.findByRole("alertdialog", {
      name: "Remove this passkey?",
    });
    const confirm = within(dialog).getByRole("button", { name: "Remove" });
    await user.click(confirm);
    await waitFor(() => expect(writes("/credentials/delete")).toHaveLength(1));
    await user.keyboard("{Escape}");

    expect(dialog).toBeInTheDocument();
    expect(confirm).toHaveAttribute("aria-busy", "true");
    expect(document.querySelectorAll('[data-slot="spinner"]')).toHaveLength(1);
  });

  it("asks first, and sends nothing until it is confirmed", async () => {
    freshSudo();
    const user = userEvent.setup();
    mount([macbook, yubikey]);

    const [first] = await rows();
    await openMenu(user, first as HTMLElement);
    await user.click(screen.getByRole("menuitem", { name: "Remove" }));

    const dialog = await screen.findByRole("alertdialog", {
      name: "Remove this passkey?",
    });
    expect(writes("/credentials/delete")).toHaveLength(0);

    await user.click(within(dialog).getByRole("button", { name: "Remove" }));
    await waitFor(() => expect(writes("/credentials/delete")).toHaveLength(1));
    expect(await writes("/credentials/delete")[0]?.json()).toEqual({ id: 1 });
  });

  it("keeps the passkey when the question is declined", async () => {
    freshSudo();
    const user = userEvent.setup();
    mount([macbook, yubikey]);

    const [first] = await rows();
    await openMenu(user, first as HTMLElement);
    await user.click(screen.getByRole("menuitem", { name: "Remove" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));

    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(writes("/credentials/delete")).toHaveLength(0);
  });

  it("asks to confirm it is you after the question, and sends once that settles", async () => {
    const user = userEvent.setup();
    let parked: SudoRequest | null = null;
    configureSudo({
      queryClient,
      set: (request) => {
        parked = request;
      },
      setFresh: () => {},
      getFresh: () => false,
    });
    mount([macbook, yubikey], (request) =>
      new URL(request.url).pathname.endsWith("/me/sudo/methods")
        ? Response.json({ methods: ["webauthn"], fresh: false })
        : undefined,
    );

    const [first] = await rows();
    await openMenu(user, first as HTMLElement);
    await user.click(screen.getByRole("menuitem", { name: "Remove" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Remove" }));

    await waitFor(() => expect(parked).not.toBeNull());
    expect(writes("/credentials/delete")).toHaveLength(0);

    const request = parked as unknown as SudoRequest;
    request.resolve(await request.perform());
    await waitFor(() =>
      expect(notifySuccess).toHaveBeenCalledWith(
        expect.objectContaining({ id: "success.passkey.removed" }),
      ),
    );
    expect(writes("/credentials/delete")).toHaveLength(1);
  });

  it("shows the server's refusal inline, once", async () => {
    freshSudo();
    const user = userEvent.setup();
    mount([macbook, yubikey], (request) =>
      new URL(request.url).pathname.endsWith("/credentials/delete")
        ? Response.json(
            { code: "last_passkey", requestId: "r1" },
            { status: 409 },
          )
        : undefined,
    );

    const [first] = await rows();
    await openMenu(user, first as HTMLElement);
    await user.click(screen.getByRole("menuitem", { name: "Remove" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Remove" }));

    expect(
      await screen.findByText(/This is the only passkey on the account/),
    ).toBeVisible();
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("when the passkeys cannot be loaded", () => {
  it("reports it once, in the section, and offers Try again", async () => {
    const user = userEvent.setup();
    let failing = true;
    mount([macbook], (request) =>
      failing && new URL(request.url).pathname.endsWith("/me/credentials")
        ? new Response(null, { status: 500 })
        : undefined,
    );

    const retry = await screen.findByRole("button", { name: "Try again" });
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();

    failing = false;
    await user.click(retry);

    expect(await screen.findByText("MacBook Pro")).toBeVisible();
    expect(notifyError).not.toHaveBeenCalled();
  });
});
