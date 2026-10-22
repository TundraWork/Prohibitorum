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
const defaultRequired =
  "Write the notice in the default language too, so every visitor sees it.";

function puts() {
  return fetchBoundary.mock.calls
    .filter(([request]) => new URL(request.url).pathname === maintenancePath)
    .map(([request]) => request.clone());
}

function mount(
  config: PublicConfig = publicConfigFixture(),
  answer?: (request: Request) => Response | Promise<Response> | undefined,
) {
  fetchBoundary.mockImplementation((request: Request) => {
    if (new URL(request.url).pathname === "/api/prohibitorum/config") {
      return Promise.resolve(Response.json(config));
    }
    const answered = answer?.(request);
    return answered instanceof Response
      ? Promise.resolve(answered)
      : (answered ?? Promise.resolve(new Response(null, { status: 204 })));
  });
  queryClient.setQueryData(publicConfigQueryOptions().queryKey, config);
  return render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <MaintenancePanel />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

const maintenanceSwitch = () =>
  screen.getByRole("switch", { name: "Maintenance mode" });
const noticeBox = (name: string) =>
  screen.getByRole("textbox", { name: new RegExp(`^${name}`) });

describe("the maintenance section", () => {
  it("has its heading, the switch, the default-language choice and one notice box per language", () => {
    mount(
      publicConfigFixture({
        maintenanceMessages: { en: "Back at noon", zh: "中午恢复" },
      }),
    );

    expect(
      screen.getByRole("heading", { level: 2, name: "Maintenance mode" }),
    ).toBeVisible();
    expect(maintenanceSwitch()).not.toBeChecked();
    // The boxes appear in the locale list's order, each labelled in its own
    // language; the default language's label carries the Default badge.
    const boxes = screen.getAllByRole("textbox");
    expect(boxes[0]).toHaveValue("Back at noon");
    expect(boxes[1]).toHaveValue("中午恢复");
    expect(noticeBox("English")).toBe(boxes[0]);
    expect(noticeBox("中文")).toBe(boxes[1]);
    expect(
      screen.getByText("Default", { selector: "[data-slot=badge]" }),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("moves the Default badge when the default language changes", async () => {
    const user = userEvent.setup();
    mount();

    await user.click(screen.getByRole("radio", { name: "中文" }));

    // The badge travels with the default: the 中文 box's label carries it now,
    // and the English one no longer does.
    const zhLabel = noticeBox("中文")
      .closest("[data-slot=field]")
      ?.querySelector("[data-slot=field-label]");
    const enLabel = noticeBox("English")
      .closest("[data-slot=field]")
      ?.querySelector("[data-slot=field-label]");
    expect(zhLabel?.textContent).toContain("Default");
    expect(enLabel?.textContent).not.toContain("Default");
  });

  it("carries the notice explanation under the default language's box", async () => {
    const user = userEvent.setup();
    mount();

    await user.click(screen.getByRole("radio", { name: "中文" }));

    // The explanation names the default language's role, so it belongs to
    // whichever box is the default — not to the first one.
    const hint = await screen.findByText(
      "Shown on the sign-in page and the maintenance page while maintenance mode is on. A visitor sees the notice in their own language, or this one when theirs has none.",
    );
    expect(noticeBox("中文").closest("[data-slot=field]")?.contains(hint)).toBe(
      true,
    );
    expect(
      noticeBox("English").closest("[data-slot=field]")?.contains(hint),
    ).toBe(false);
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
    await user.type(noticeBox("English"), "Back at noon");
    await user.click(
      screen.getByRole("button", { name: "Turn on maintenance and save" }),
    );

    const dialog = await screen.findByRole("alertdialog", {
      name: "Turn on maintenance mode?",
    });
    expect(puts()).toHaveLength(0);
    expect(
      within(dialog).getByText("Only administrators will be able to sign in."),
    ).toBeVisible();

    await user.click(within(dialog).getByRole("button", { name: "Turn on" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await puts()[0]?.json()).toEqual({
      maintenanceMode: true,
      maintenanceMessages: { en: "Back at noon", zh: "" },
      maintenanceDefaultLocale: "en",
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
    mount(
      publicConfigFixture({
        maintenanceMode: true,
        maintenanceMessages: { en: "Back at noon" },
      }),
    );

    await user.click(maintenanceSwitch());
    expect(screen.queryByText(consequence)).toBeNull();
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(await puts()[0]?.json()).toEqual({
      maintenanceMode: false,
      maintenanceMessages: { en: "Back at noon", zh: "" },
      maintenanceDefaultLocale: "en",
    });
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

    await user.click(noticeBox("English"));
    await user.paste("x".repeat(501));
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(
      await screen.findByText("Use 500 characters or fewer."),
    ).toBeVisible();
    expect(puts()).toHaveLength(0);
  });

  it("refuses a save that fills a language but not the default, on the default's box", async () => {
    const user = userEvent.setup();
    mount(publicConfigFixture({ maintenanceMode: true }));

    await user.type(noticeBox("中文"), "维护中");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText(defaultRequired)).toBeVisible();
    expect(puts()).toHaveLength(0);
  });

  it("maps the server's maintenance_default_notice_required onto the default language's box", async () => {
    const user = userEvent.setup();
    mount(publicConfigFixture({ maintenanceMode: true }), (request) =>
      request.method === "PUT"
        ? Response.json(
            {
              code: "maintenance_default_notice_required",
              requestId: "maintenance-test",
            },
            { status: 400 },
          )
        : undefined,
    );

    await user.type(noticeBox("English"), "Back at noon");
    await user.type(noticeBox("中文"), "马上回来");
    // Fill the default so the client-side check passes and the server's
    // refusal is the one under test.
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    const error = await screen.findByText(defaultRequired);
    expect(error).toBeVisible();
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("puts a server's plain bad_request on the default language's box, on a real save", async () => {
    const user = userEvent.setup();
    mount(publicConfigFixture({ maintenanceMode: true }), (request) =>
      request.method === "PUT"
        ? Response.json(
            { code: "bad_request", requestId: "maintenance-test" },
            { status: 400 },
          )
        : undefined,
    );

    await user.type(noticeBox("English"), "Back at noon");
    await user.type(noticeBox("中文"), "马上回来");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    const error = await screen.findByText("The request could not be accepted.");
    const enField = noticeBox("English").closest("[data-slot=field]");
    const zhField = noticeBox("中文").closest("[data-slot=field]");
    expect(enField?.contains(error)).toBe(true);
    expect(zhField?.contains(error)).toBe(false);
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("reports a refused save once, on the form and not as a toast", async () => {
    const user = userEvent.setup();
    mount(publicConfigFixture({ maintenanceMode: true }), (request) =>
      request.method === "PUT"
        ? Response.json(
            { code: "internal_error", requestId: "maintenance-test" },
            { status: 500 },
          )
        : undefined,
    );

    await user.type(noticeBox("English"), "x");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await screen.findByRole("alert")).toBeVisible();
    expect(notifyError).not.toHaveBeenCalled();
  });
});
