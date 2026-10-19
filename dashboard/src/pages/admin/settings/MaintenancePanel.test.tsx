import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { publicConfigQueryOptions } from "@/api/queries";
import type { PublicConfig } from "@/api/raw-paths";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { MaintenancePanel } from "@/pages/admin/settings/MaintenancePanel";
import { publicConfigFixture } from "@/test/public-config";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifyError.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(
    notifyError,
    vi.fn<(message: MessageDescriptor) => void>(),
  );
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

const maintenancePath = "/api/prohibitorum/admin/settings/maintenance";
const consequence =
  "Nobody except administrators can sign in or use applications until it is turned off.";

function puts() {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter(
      (request) =>
        request.method === "PUT" &&
        new URL(request.url).pathname === maintenancePath,
    );
}

function mount(
  config: PublicConfig = publicConfigFixture(),
  answer?: (request: Request) => Response | Promise<Response> | undefined,
) {
  queryClient.setQueryData(publicConfigQueryOptions().queryKey, config);
  fetchBoundary.mockImplementation(async (request) => {
    const canned = answer?.(request);
    if (canned) return canned;
    if (request.method === "GET") return Response.json(config);
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <MaintenancePanel />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

const maintenanceSwitch = () =>
  screen.getByRole("switch", { name: "Maintenance mode" });
const notice = () => screen.getByRole("textbox", { name: "Notice" });

describe("the maintenance section", () => {
  it("has its heading, the switch and the notice with its hint, and no footer until something changes", () => {
    mount(publicConfigFixture({ maintenanceMessage: "Back at noon" }));

    expect(
      screen.getByRole("heading", { level: 2, name: "Maintenance mode" }),
    ).toBeVisible();
    expect(maintenanceSwitch()).not.toBeChecked();
    expect(notice()).toHaveValue("Back at noon");
    expect(
      screen.getByText(
        "Shown on the sign-in page while maintenance mode is on.",
      ),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("states the consequence and names the action while the switch is going on", async () => {
    const user = userEvent.setup();
    mount();

    await user.click(maintenanceSwitch());

    expect(screen.getByText(consequence)).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Turn on maintenance and save" }),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
    expect(puts()).toHaveLength(0);
  });

  it("asks before turning it on, and sends only after the confirmation", async () => {
    const user = userEvent.setup();
    mount();

    await user.click(maintenanceSwitch());
    await user.type(notice(), "Back at noon");
    await user.click(
      screen.getByRole("button", { name: "Turn on maintenance and save" }),
    );

    const dialog = await screen.findByRole("alertdialog", {
      name: "Turn on maintenance mode?",
    });
    expect(puts()).toHaveLength(0);

    await user.click(within(dialog).getByRole("button", { name: "Turn on" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await puts()[0]?.json()).toEqual({
      maintenanceMode: true,
      maintenanceMessage: "Back at noon",
    });
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  });

  it("leaves the form as it was and sends nothing when the confirmation is cancelled", async () => {
    const user = userEvent.setup();
    mount();

    await user.click(maintenanceSwitch());
    await user.click(
      screen.getByRole("button", { name: "Turn on maintenance and save" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));

    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    expect(maintenanceSwitch()).toBeChecked();
    expect(puts()).toHaveLength(0);
  });

  it("ignores dismissal while the confirmed write is in flight", async () => {
    const user = userEvent.setup();
    let release: (response: Response) => void = () => {};
    mount(publicConfigFixture(), (request) =>
      request.method === "PUT"
        ? new Promise<Response>((resolve) => {
            release = resolve;
          })
        : undefined,
    );

    await user.click(maintenanceSwitch());
    await user.click(
      screen.getByRole("button", { name: "Turn on maintenance and save" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Turn on" }));
    await waitFor(() => expect(puts()).toHaveLength(1));

    await user.keyboard("{Escape}");
    expect(screen.getByRole("alertdialog")).toBeVisible();

    release(new Response(null, { status: 204 }));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  });

  it("turns it off with a plain Save, no consequence and no confirmation", async () => {
    const user = userEvent.setup();
    mount(publicConfigFixture({ maintenanceMode: true }));

    await user.click(maintenanceSwitch());
    expect(screen.queryByText(consequence)).toBeNull();
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(await puts()[0]?.json()).toEqual({
      maintenanceMode: false,
      maintenanceMessage: "",
    });
  });

  it("saves a new notice while it is already on without asking", async () => {
    const user = userEvent.setup();
    mount(publicConfigFixture({ maintenanceMode: true }));

    await user.type(notice(), "Back at noon");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });

  it("puts the form back with Discard, sending nothing", async () => {
    const user = userEvent.setup();
    mount();

    await user.click(maintenanceSwitch());
    await user.click(screen.getByRole("button", { name: "Discard" }));

    expect(maintenanceSwitch()).not.toBeChecked();
    expect(puts()).toHaveLength(0);
  });

  it("refuses a notice over 500 characters with the limit, and sends nothing", async () => {
    const user = userEvent.setup();
    mount(publicConfigFixture({ maintenanceMode: true }));

    await user.click(notice());
    await user.paste("x".repeat(501));
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(
      await screen.findByText("Use 500 characters or fewer."),
    ).toBeVisible();
    expect(puts()).toHaveLength(0);
  });

  it("reports a refused save once, on the form and not as a toast", async () => {
    const user = userEvent.setup();
    mount(publicConfigFixture({ maintenanceMode: true }), (request) =>
      request.method === "PUT"
        ? Response.json(
            { code: "internal_error", message: "boom" },
            { status: 500 },
          )
        : undefined,
    );

    await user.type(notice(), "x");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await screen.findByRole("alert")).toBeVisible();
    expect(notifyError).not.toHaveBeenCalled();
  });
});
