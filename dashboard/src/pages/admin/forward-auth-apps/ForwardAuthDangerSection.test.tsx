import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { configureSudo, resetSudo } from "@/api/sudo";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { ForwardAuthDangerSection } from "@/pages/admin/forward-auth-apps/ForwardAuthDangerSection";
import {
  type ForwardAuthApp,
  forwardAuthAppFixture,
} from "@/test/forward-auth-app";

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

const groupsResponse = (names: string[]) =>
  Response.json({
    app: {
      kind: "forward_auth",
      appId: "grafana",
      displayName: "Grafana",
      accessRestricted: names.length > 0,
    },
    accessRestricted: names.length > 0,
    providers: [],
    groups: names.map((displayName, index) => ({
      id: index + 1,
      slug: displayName.toLowerCase(),
      displayName,
    })),
  });

function mount(
  app: ForwardAuthApp,
  extra?: (request: Request) => Response | Promise<Response> | undefined,
) {
  fetchBoundary.mockImplementation(async (request) => {
    const canned = extra?.(request);
    if (canned) return canned;
    const path = new URL(request.url).pathname;
    if (path.endsWith("/me/sudo/methods")) {
      return Response.json({ methods: ["password_totp"], fresh: true });
    }
    if (path.endsWith("/access")) return groupsResponse([]);
    if (path.endsWith("/set-disabled")) return Response.json(app);
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <ForwardAuthDangerSection app={app} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("disabling and deleting", () => {
  it("offers the two actions under the same heading as the other applications", () => {
    mount(forwardAuthAppFixture());

    expect(
      screen.getByRole("heading", { level: 2, name: "Disable or delete" }),
    ).toBeVisible();
    expect(screen.getByText("Disable the application")).toBeVisible();
    expect(screen.getByText("Delete the application")).toBeVisible();
  });

  it("offers to enable an application that is disabled", () => {
    mount(forwardAuthAppFixture({ disabled: true }));
    expect(screen.getByText("Enable the application")).toBeVisible();
  });

  it("disables only after the confirmation", async () => {
    const user = userEvent.setup();
    mount(forwardAuthAppFixture());

    await user.click(screen.getByRole("button", { name: "Disable" }));
    const dialog = await screen.findByRole("alertdialog", {
      name: "Disable this application?",
    });
    expect(posts("/set-disabled")).toHaveLength(0);
    await user.click(within(dialog).getByRole("button", { name: "Disable" }));

    await waitFor(() => expect(posts("/set-disabled")).toHaveLength(1));
    expect(await posts("/set-disabled")[0]?.json()).toEqual({
      clientId: "grafana",
      disabled: true,
    });
  });

  it("enables after the confirmation too", async () => {
    const user = userEvent.setup();
    mount(forwardAuthAppFixture({ disabled: true }));

    await user.click(screen.getByRole("button", { name: "Enable" }));
    const dialog = await screen.findByRole("alertdialog", {
      name: "Enable this application?",
    });
    expect(posts("/set-disabled")).toHaveLength(0);
    await user.click(within(dialog).getByRole("button", { name: "Enable" }));

    await waitFor(() => expect(posts("/set-disabled")).toHaveLength(1));
    expect(await posts("/set-disabled")[0]?.json()).toEqual({
      clientId: "grafana",
      disabled: false,
    });
  });

  it("deletes only after the confirmation, then leaves for the list", async () => {
    const user = userEvent.setup();
    mount(forwardAuthAppFixture());

    await user.click(screen.getByRole("button", { name: "Delete" }));
    const dialog = await screen.findByRole("alertdialog", {
      name: "Delete this application?",
    });
    expect(posts("/delete")).toHaveLength(0);
    await user.click(
      within(dialog).getByRole("button", { name: "Delete application" }),
    );

    await waitFor(() => expect(posts("/delete")).toHaveLength(1));
    expect(await posts("/delete")[0]?.json()).toEqual({ clientId: "grafana" });
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({
        to: "/admin/forward-auth-apps",
      }),
    );
  });

  it("names the user groups that go with the application", async () => {
    const user = userEvent.setup();
    mount(forwardAuthAppFixture(), (request) =>
      new URL(request.url).pathname.endsWith("/access")
        ? groupsResponse(["Ops", "Dev"])
        : undefined,
    );

    await user.click(screen.getByRole("button", { name: "Delete" }));
    const dialog = await screen.findByRole("alertdialog");

    await waitFor(() =>
      expect(dialog).toHaveTextContent(
        "including its selection of 2 user groups",
      ),
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
    mount(forwardAuthAppFixture(), (request) =>
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
    mount(forwardAuthAppFixture(), (request) =>
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

  it("reports a refused delete once, on the section", async () => {
    const user = userEvent.setup();
    mount(forwardAuthAppFixture(), (request) =>
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
    mount(forwardAuthAppFixture(), (request) =>
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

  it("does not report a failed group read, which only costs the count", async () => {
    mount(forwardAuthAppFixture(), (request) =>
      new URL(request.url).pathname.endsWith("/access")
        ? Response.json({ code: "internal_error" }, { status: 500 })
        : undefined,
    );

    await screen.findByText("Delete the application");
    await waitFor(() =>
      expect(
        fetchBoundary.mock.calls.some(([request]) =>
          new URL(request.url).pathname.endsWith("/access"),
        ),
      ).toBe(true),
    );
    expect(notifyError).not.toHaveBeenCalled();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("confirms without a group count when the groups could not be read", async () => {
    const user = userEvent.setup();
    mount(forwardAuthAppFixture(), (request) =>
      new URL(request.url).pathname.endsWith("/access")
        ? Response.json({ code: "internal_error" }, { status: 500 })
        : undefined,
    );

    await user.click(screen.getByRole("button", { name: "Delete" }));
    const dialog = await screen.findByRole("alertdialog");

    expect(dialog).toHaveTextContent(
      "removed along with its icon and its access policy. Requests",
    );
    expect(dialog).not.toHaveTextContent("including its selection");
  });
});
