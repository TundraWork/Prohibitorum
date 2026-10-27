import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { configureSudo, resetSudo } from "@/api/sudo";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { SamlDangerSection } from "@/pages/admin/saml-applications/SamlDangerSection";
import { type SamlApp, samlAppFixture } from "@/test/saml-app";

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
  app: SamlApp,
  extra?: (request: Request) => Response | Promise<Response> | undefined,
) {
  fetchBoundary.mockImplementation(async (request) => {
    const canned = extra?.(request);
    if (canned) return canned;
    const path = new URL(request.url).pathname;
    if (path.endsWith("/me/sudo/methods")) {
      return Response.json({ methods: ["password_totp"], fresh: true });
    }
    if (path.endsWith("/set-disabled")) return Response.json(app);
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <SamlDangerSection app={app} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("disabling and deleting", () => {
  it("offers the two actions under the same heading as the other applications", () => {
    mount(samlAppFixture());

    expect(
      screen.getByRole("heading", { level: 2, name: "Disable or delete" }),
    ).toBeVisible();
    expect(screen.getByText("Disable this application")).toBeVisible();
    expect(screen.getByText("Delete this application")).toBeVisible();
  });

  it("offers to enable an application that is disabled", () => {
    mount(samlAppFixture({ disabled: true }));
    expect(screen.getByText("Enable this application")).toBeVisible();
  });

  it("disables only after the confirmation", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture());

    await user.click(screen.getByRole("button", { name: "Disable" }));
    const dialog = await screen.findByRole("alertdialog", {
      name: "Disable this application?",
    });
    expect(posts("/set-disabled")).toHaveLength(0);
    await user.click(within(dialog).getByRole("button", { name: "Disable" }));

    await waitFor(() => expect(posts("/set-disabled")).toHaveLength(1));
    expect(await posts("/set-disabled")[0]?.json()).toEqual({
      id: 7,
      disabled: true,
    });
  });

  it("enables after the confirmation too", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture({ disabled: true }));

    await user.click(screen.getByRole("button", { name: "Enable" }));
    const dialog = await screen.findByRole("alertdialog", {
      name: "Enable this application?",
    });
    expect(posts("/set-disabled")).toHaveLength(0);
    await user.click(within(dialog).getByRole("button", { name: "Enable" }));

    await waitFor(() => expect(posts("/set-disabled")).toHaveLength(1));
    expect(await posts("/set-disabled")[0]?.json()).toEqual({
      id: 7,
      disabled: false,
    });
  });

  it("deletes only after the confirmation, then leaves for the list", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture());

    await user.click(screen.getByRole("button", { name: "Delete" }));
    const dialog = await screen.findByRole("alertdialog", {
      name: "Delete this application?",
    });
    expect(posts("/delete")).toHaveLength(0);
    await user.click(
      within(dialog).getByRole("button", { name: "Delete application" }),
    );

    await waitFor(() => expect(posts("/delete")).toHaveLength(1));
    expect(await posts("/delete")[0]?.json()).toEqual({ id: 7 });
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/admin/saml-applications" }),
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
    mount(samlAppFixture(), (request) =>
      new URL(request.url).pathname.endsWith("/me/sudo/methods")
        ? Response.json({ methods: ["password_totp"], fresh: false })
        : undefined,
    );

    await user.click(screen.getByRole("button", { name: "Delete" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Delete application" }),
    );

    await waitFor(() => expect(set).toHaveBeenCalled());
    expect(posts("/delete")).toHaveLength(0);
  });

  it("keeps the confirmation open while the write is in flight", async () => {
    const user = userEvent.setup();
    let release: (response: Response) => void = () => {};
    mount(samlAppFixture(), (request) =>
      new URL(request.url).pathname.endsWith("/delete")
        ? new Promise<Response>((resolve) => {
            release = resolve;
          })
        : undefined,
    );

    await user.click(screen.getByRole("button", { name: "Delete" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Delete application" }),
    );
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
    mount(samlAppFixture());
    const trigger = screen.getByRole("button", { name: "Delete" });

    await user.click(trigger);
    const dialog = await screen.findByRole("alertdialog");
    const confirm = within(dialog).getByRole("button", {
      name: "Delete application",
    });
    await user.click(confirm);
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/admin/saml-applications" }),
    );

    expect(screen.getByRole("alertdialog")).toBeVisible();
    const spinners = document.querySelectorAll('[data-slot="spinner"]');
    expect(spinners).toHaveLength(1);
    expect(confirm).toContainElement(spinners[0] as HTMLElement);
    expect(trigger).toBeDisabled();
    arrive();
  });

  it("reports a refused delete once, on the section", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture(), (request) =>
      new URL(request.url).pathname.endsWith("/delete")
        ? Response.json({ code: "internal_error" }, { status: 500 })
        : undefined,
    );

    await user.click(screen.getByRole("button", { name: "Delete" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Delete application" }),
    );

    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("reports a refused disable once, on the section", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture(), (request) =>
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
