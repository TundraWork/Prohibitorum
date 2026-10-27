import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { configureSudo, resetSudo } from "@/api/sudo";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { ProviderDangerSection } from "@/pages/admin/identity-providers/ProviderDangerSection";
import { type Provider, providerFixture } from "@/test/identity-provider";

const navigate = vi.fn<(options: { to: string }) => Promise<void>>();
vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  useNavigate: () => navigate,
}));

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifyError.mockReset();
  navigate.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(
    notifyError,
    vi.fn<(message: MessageDescriptor) => void>(),
  );
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

const spinners = () =>
  Array.from(document.querySelectorAll('[data-slot="spinner"]'));

function posts(suffix: string) {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter(
      (request) =>
        request.method === "POST" &&
        new URL(request.url).pathname.endsWith(suffix),
    );
}

function mount(
  provider: Provider,
  extra?: (request: Request) => Response | Promise<Response> | undefined,
) {
  fetchBoundary.mockImplementation(async (request) => {
    const canned = extra?.(request);
    if (canned) return canned;
    const path = new URL(request.url).pathname;
    if (path.endsWith("/me/sudo/methods")) {
      return Response.json({ methods: ["password_totp"], fresh: true });
    }
    if (path.endsWith("/set-disabled")) return Response.json(provider);
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <ProviderDangerSection provider={provider} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

async function openDeleteDialog(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: "Delete" }));
  return screen.findByRole("alertdialog", { name: "Delete this provider?" });
}

describe("the danger section", () => {
  it("is named like the other pages' and holds no secret row", () => {
    mount(providerFixture());

    expect(
      screen.getByRole("heading", { level: 2, name: "Disable or delete" }),
    ).toBeVisible();
    expect(screen.getByText("Disable this provider")).toBeVisible();
    expect(screen.getByText("Delete this provider")).toBeVisible();
    expect(screen.queryByText(/the secret/i)).toBeNull();
  });
});

describe("disabling", () => {
  it("disables only after the confirmation", async () => {
    const user = userEvent.setup();
    mount(providerFixture());

    await user.click(screen.getByRole("button", { name: "Disable" }));
    const dialog = await screen.findByRole("alertdialog", {
      name: "Disable this provider?",
    });
    expect(posts("/set-disabled")).toHaveLength(0);
    await user.click(within(dialog).getByRole("button", { name: "Disable" }));

    await waitFor(() => expect(posts("/set-disabled")).toHaveLength(1));
    expect(await posts("/set-disabled")[0]?.json()).toEqual({
      slug: "corp",
      disabled: true,
    });
  });

  it("keeps the confirmation open while the write is in flight", async () => {
    const user = userEvent.setup();
    let release: (response: Response) => void = () => {};
    mount(providerFixture(), (request) =>
      new URL(request.url).pathname.endsWith("/set-disabled")
        ? new Promise<Response>((resolve) => {
            release = resolve;
          })
        : undefined,
    );

    const trigger = screen.getByRole("button", { name: "Disable" });
    const remove = screen.getByRole("button", { name: "Delete" });
    await user.click(trigger);
    const dialog = await screen.findByRole("alertdialog");
    const confirm = within(dialog).getByRole("button", { name: "Disable" });
    await user.click(confirm);
    await waitFor(() => expect(posts("/set-disabled")).toHaveLength(1));
    await user.keyboard("{Escape}");

    expect(screen.getByRole("alertdialog")).toBeVisible();
    // One spinner, on the confirmation; the row's buttons are only held.
    expect(spinners()).toHaveLength(1);
    expect(confirm).toContainElement(spinners()[0] as HTMLElement);
    expect(trigger).toBeDisabled();
    expect(remove).toBeDisabled();
    release(Response.json(providerFixture({ disabled: true })));
  });

  it("reports a refused disable once, on the section", async () => {
    const user = userEvent.setup();
    mount(providerFixture(), (request) =>
      new URL(request.url).pathname.endsWith("/set-disabled")
        ? Response.json({ code: "internal_error" }, { status: 500 })
        : undefined,
    );

    await user.click(screen.getByRole("button", { name: "Disable" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Disable" }));

    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("enabling", () => {
  it("runs straight through, with no confirmation", async () => {
    const user = userEvent.setup();
    mount(providerFixture({ disabled: true }));

    expect(screen.getByText("Enable this provider")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Enable" }));

    await waitFor(() => expect(posts("/set-disabled")).toHaveLength(1));
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(await posts("/set-disabled")[0]?.json()).toEqual({
      slug: "corp",
      disabled: false,
    });
  });

  it("spins the toggle itself, and holds Delete, while it runs", async () => {
    const user = userEvent.setup();
    let release: (response: Response) => void = () => {};
    mount(providerFixture({ disabled: true }), (request) =>
      new URL(request.url).pathname.endsWith("/set-disabled")
        ? new Promise<Response>((resolve) => {
            release = resolve;
          })
        : undefined,
    );

    const enable = screen.getByRole("button", { name: "Enable" });
    await user.click(enable);
    await waitFor(() => expect(posts("/set-disabled")).toHaveLength(1));

    expect(enable).toHaveAttribute("aria-busy", "true");
    expect(enable).toBeEnabled();
    expect(spinners()).toHaveLength(1);
    expect(enable).toContainElement(spinners()[0] as HTMLElement);
    expect(screen.getByRole("button", { name: "Delete" })).toBeDisabled();
    release(Response.json(providerFixture()));
  });

  it.each([
    ["oidc", {}, "Configure a valid connection before enabling this provider."],
    [
      "oidc",
      { secretConfigured: false },
      "Set a client secret before enabling this provider.",
    ],
    ["steam", {}, "Set the Web API key before enabling this provider."],
    [
      "vrchat",
      { secretStatus: "unconfigured", secretConfigured: false },
      "Sign in the operator account before enabling this provider.",
    ],
  ])(
    "is disabled for an unready %s provider, with the reason as text",
    async (protocol, overrides, reason) => {
      const user = userEvent.setup();
      mount(
        providerFixture({
          protocol,
          disabled: true,
          ready: false,
          ...overrides,
        }),
      );

      const enable = screen.getByRole("button", { name: "Enable" });
      expect(enable).toBeDisabled();
      expect(screen.getByText(reason)).toBeVisible();
      await user.click(enable);
      expect(posts("/set-disabled")).toHaveLength(0);
    },
  );

  it("gives no reason while the provider is ready", () => {
    mount(providerFixture({ disabled: true }));
    expect(screen.getByRole("button", { name: "Enable" })).toBeEnabled();
    expect(screen.queryByText(/before enabling this provider/)).toBeNull();
  });

  it("reports a refused enable once, on the section", async () => {
    const user = userEvent.setup();
    mount(providerFixture({ disabled: true }), (request) =>
      new URL(request.url).pathname.endsWith("/set-disabled")
        ? Response.json({ code: "provider_not_ready" }, { status: 503 })
        : undefined,
    );

    await user.click(screen.getByRole("button", { name: "Enable" }));

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("the delete confirmation", () => {
  it("counts the accounts and the invitations that go with the provider", async () => {
    const user = userEvent.setup();
    mount(
      providerFixture({ pendingInvitationCount: 3, linkedAccountCount: 2 }),
    );

    const dialog = await openDeleteDialog(user);
    expect(
      within(dialog).getByText(/2 accounts will no longer be able to sign in/),
    ).toBeVisible();
    expect(
      within(dialog).getByText(
        /3 unfinished invitations verified through this provider/,
      ),
    ).toBeVisible();
    expect(within(dialog).getByText("identity:corp")).toBeVisible();
  });

  it("uses the singular for exactly one of each", async () => {
    const user = userEvent.setup();
    mount(
      providerFixture({ pendingInvitationCount: 1, linkedAccountCount: 1 }),
    );

    const dialog = await openDeleteDialog(user);
    expect(
      within(dialog).getByText(/1 account will no longer be able to sign in/),
    ).toBeVisible();
    expect(
      within(dialog).getByText(
        /1 unfinished invitation verified through this provider/,
      ),
    ).toBeVisible();
  });

  it("says only what is removed when nothing rides on the provider", async () => {
    const user = userEvent.setup();
    mount(providerFixture());

    const dialog = await openDeleteDialog(user);
    expect(
      within(dialog).getByText(/The provider and its icon will be deleted/),
    ).toBeVisible();
    expect(within(dialog).queryByText(/unfinished invitation/)).toBeNull();
  });

  it("deletes only after the confirmation, then leaves for the list", async () => {
    const user = userEvent.setup();
    mount(providerFixture());

    const dialog = await openDeleteDialog(user);
    expect(posts("/delete")).toHaveLength(0);
    await user.click(within(dialog).getByRole("button", { name: "Delete" }));

    await waitFor(() => expect(posts("/delete")).toHaveLength(1));
    expect(await posts("/delete")[0]?.json()).toEqual({ slug: "corp" });
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({
        to: "/admin/identity-providers",
      }),
    );
  });

  it("asks for the identity check first when the window is closed", async () => {
    const user = userEvent.setup();
    const set = vi.fn();
    configureSudo({
      queryClient,
      set,
      setFresh: () => {},
      getFresh: () => false,
    });
    mount(providerFixture(), (request) =>
      new URL(request.url).pathname.endsWith("/me/sudo/methods")
        ? Response.json({ methods: ["password_totp"], fresh: false })
        : undefined,
    );

    const dialog = await openDeleteDialog(user);
    await user.click(within(dialog).getByRole("button", { name: "Delete" }));

    await waitFor(() => expect(set).toHaveBeenCalled());
    expect(posts("/delete")).toHaveLength(0);
  });

  it("keeps the confirmation open while the write is in flight", async () => {
    const user = userEvent.setup();
    let release: (response: Response) => void = () => {};
    mount(providerFixture(), (request) =>
      new URL(request.url).pathname.endsWith("/delete")
        ? new Promise<Response>((resolve) => {
            release = resolve;
          })
        : undefined,
    );

    const dialog = await openDeleteDialog(user);
    await user.click(within(dialog).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(posts("/delete")).toHaveLength(1));
    await user.keyboard("{Escape}");

    expect(screen.getByRole("alertdialog")).toBeVisible();
    release(new Response(null, { status: 204 }));
  });

  it("shows one spinner, on the confirmation, until the list has replaced the page", async () => {
    const user = userEvent.setup();
    let arrive: () => void = () => {};
    navigate.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          arrive = resolve;
        }),
    );
    mount(providerFixture());
    const toggle = screen.getByRole("button", { name: "Disable" });
    const trigger = screen.getByRole("button", { name: "Delete" });

    const dialog = await openDeleteDialog(user);
    const confirm = within(dialog).getByRole("button", { name: "Delete" });
    await user.click(confirm);
    await waitFor(() => expect(navigate).toHaveBeenCalled());

    expect(screen.getByRole("alertdialog")).toBeVisible();
    expect(spinners()).toHaveLength(1);
    expect(confirm).toContainElement(spinners()[0] as HTMLElement);
    expect(trigger).toBeDisabled();
    expect(toggle).toBeDisabled();
    arrive();
  });

  it("reports a refused delete once, on the section", async () => {
    const user = userEvent.setup();
    mount(providerFixture(), (request) =>
      new URL(request.url).pathname.endsWith("/delete")
        ? Response.json({ code: "internal_error" }, { status: 500 })
        : undefined,
    );

    const dialog = await openDeleteDialog(user);
    await user.click(within(dialog).getByRole("button", { name: "Delete" }));

    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});
