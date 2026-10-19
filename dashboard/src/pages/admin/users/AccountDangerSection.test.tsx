import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { configureSudo, resetSudo } from "@/api/sudo";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { AccountDangerSection } from "@/pages/admin/users/AccountDangerSection";
import { type Account, accountFixture } from "@/test/account";

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
  account: Account,
  extra?: (request: Request) => Response | Promise<Response> | undefined,
) {
  fetchBoundary.mockImplementation(async (request) => {
    const canned = extra?.(request);
    if (canned) return canned;
    const path = new URL(request.url).pathname;
    if (path.endsWith("/me/sudo/methods")) {
      return Response.json({ methods: ["password_totp"], fresh: true });
    }
    if (path.endsWith("/set-disabled")) return Response.json(account);
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <AccountDangerSection account={account} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("the danger section", () => {
  it("is named like the other pages' and holds both actions", () => {
    mount(accountFixture());

    expect(
      screen.getByRole("heading", { level: 2, name: "Disable or delete" }),
    ).toBeVisible();
    expect(screen.getByText("Disable this account")).toBeVisible();
    expect(screen.getByText("Delete this account")).toBeVisible();
  });
});

describe("disabling", () => {
  it("disables only after the confirmation", async () => {
    const user = userEvent.setup();
    mount(accountFixture());

    await user.click(screen.getByRole("button", { name: "Disable" }));
    const dialog = await screen.findByRole("alertdialog", {
      name: "Disable this account?",
    });
    expect(dialog).toHaveTextContent(
      "They will be signed out of every session they have open and will not be able to sign in until you enable the account again.",
    );
    expect(posts("/set-disabled")).toHaveLength(0);
    await user.click(within(dialog).getByRole("button", { name: "Disable" }));

    await waitFor(() => expect(posts("/set-disabled")).toHaveLength(1));
    expect(await posts("/set-disabled")[0]?.json()).toEqual({
      id: 2,
      disabled: true,
    });
  });

  it("enables a disabled account after its confirmation", async () => {
    const user = userEvent.setup();
    mount(accountFixture({ disabled: true }));

    expect(screen.getByText("Enable this account")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Enable" }));
    const dialog = await screen.findByRole("alertdialog", {
      name: "Enable this account?",
    });
    await user.click(within(dialog).getByRole("button", { name: "Enable" }));

    await waitFor(() => expect(posts("/set-disabled")).toHaveLength(1));
    expect(await posts("/set-disabled")[0]?.json()).toEqual({
      id: 2,
      disabled: false,
    });
  });

  it("keeps the confirmation open while the write is in flight", async () => {
    const user = userEvent.setup();
    let release: (response: Response) => void = () => {};
    mount(accountFixture(), (request) =>
      new URL(request.url).pathname.endsWith("/set-disabled")
        ? new Promise<Response>((resolve) => {
            release = resolve;
          })
        : undefined,
    );

    await user.click(screen.getByRole("button", { name: "Disable" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Disable" }));
    await waitFor(() => expect(posts("/set-disabled")).toHaveLength(1));
    await user.keyboard("{Escape}");

    expect(screen.getByRole("alertdialog")).toBeVisible();
    release(Response.json(accountFixture({ disabled: true })));
  });

  it("is unavailable for an admin, with the reason in words", () => {
    mount(accountFixture({ role: "admin" }));

    const disable = screen.getByRole("button", { name: "Disable" });
    expect(disable).toBeDisabled();
    const reason = screen.getByText(
      "An admin cannot be disabled. Make them a user first.",
    );
    expect(reason).toBeVisible();
    expect(disable).toHaveAccessibleDescription(
      "An admin cannot be disabled. Make them a user first.",
    );
  });

  it("says that disabling signs the account out of its sessions", () => {
    mount(accountFixture());
    expect(
      screen.getByText(
        "Disabling stops this account signing in and signs it out of every session it has open.",
      ),
    ).toBeVisible();
  });

  it("does not block the delete row for an admin", () => {
    mount(accountFixture({ role: "admin" }));
    expect(screen.getByRole("button", { name: "Delete" })).toBeEnabled();
  });

  it("reports a refused disable once, on the section", async () => {
    const user = userEvent.setup();
    mount(accountFixture(), (request) =>
      new URL(request.url).pathname.endsWith("/set-disabled")
        ? Response.json({ code: "admin_cannot_be_disabled" }, { status: 400 })
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

describe("deleting", () => {
  it("is held on the reader's own account, with the reason in words", () => {
    const base = accountFixture();
    fetchBoundary.mockImplementation(
      async () => new Response(null, { status: 204 }),
    );
    render(
      <I18nProvider i18n={i18n}>
        <QueryClientProvider client={queryClient}>
          <AccountDangerSection account={base} isSelf />
        </QueryClientProvider>
      </I18nProvider>,
    );

    const remove = screen.getByRole("button", { name: "Delete" });
    expect(remove).toBeDisabled();
    expect(
      screen.getByText("You can't delete your own account."),
    ).toBeVisible();
    expect(remove).toHaveAccessibleDescription(
      "You can't delete your own account.",
    );
  });

  async function openDeleteDialog(user: ReturnType<typeof userEvent.setup>) {
    await user.click(screen.getByRole("button", { name: "Delete" }));
    return screen.findByRole("alertdialog", { name: "Delete this account?" });
  }

  it("deletes only after the confirmation, then leaves for the list", async () => {
    const user = userEvent.setup();
    mount(accountFixture());

    const dialog = await openDeleteDialog(user);
    expect(posts("/delete")).toHaveLength(0);
    await user.click(
      within(dialog).getByRole("button", { name: "Delete account" }),
    );

    await waitFor(() => expect(posts("/delete")).toHaveLength(1));
    expect(await posts("/delete")[0]?.json()).toEqual({ id: 2 });
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/admin/users" }),
    );
  });

  it("asks for the identity check before deleting when the window is closed", async () => {
    const user = userEvent.setup();
    const set = vi.fn();
    configureSudo({
      queryClient,
      set,
      setFresh: () => {},
      getFresh: () => false,
    });
    mount(accountFixture(), (request) =>
      new URL(request.url).pathname.endsWith("/me/sudo/methods")
        ? Response.json({ methods: ["password_totp"], fresh: false })
        : undefined,
    );

    const dialog = await openDeleteDialog(user);
    await user.click(
      within(dialog).getByRole("button", { name: "Delete account" }),
    );

    await waitFor(() => expect(set).toHaveBeenCalled());
    expect(posts("/delete")).toHaveLength(0);
    expect(navigate).not.toHaveBeenCalled();
  });

  it("keeps the confirmation open while the write is in flight", async () => {
    const user = userEvent.setup();
    let release: (response: Response) => void = () => {};
    mount(accountFixture(), (request) =>
      new URL(request.url).pathname.endsWith("/delete")
        ? new Promise<Response>((resolve) => {
            release = resolve;
          })
        : undefined,
    );

    const dialog = await openDeleteDialog(user);
    await user.click(
      within(dialog).getByRole("button", { name: "Delete account" }),
    );
    await waitFor(() => expect(posts("/delete")).toHaveLength(1));
    await user.keyboard("{Escape}");

    expect(screen.getByRole("alertdialog")).toBeVisible();
    release(new Response(null, { status: 204 }));
  });

  it("reports a refused delete once, and stays on the page", async () => {
    const user = userEvent.setup();
    mount(accountFixture(), (request) =>
      new URL(request.url).pathname.endsWith("/delete")
        ? Response.json({ code: "internal_error" }, { status: 500 })
        : undefined,
    );

    const dialog = await openDeleteDialog(user);
    await user.click(
      within(dialog).getByRole("button", { name: "Delete account" }),
    );

    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
    expect(navigate).not.toHaveBeenCalled();
  });
});
