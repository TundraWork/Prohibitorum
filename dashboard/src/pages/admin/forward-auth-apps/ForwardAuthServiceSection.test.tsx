import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { ForwardAuthServiceSection } from "@/pages/admin/forward-auth-apps/ForwardAuthServiceSection";
import {
  type ForwardAuthApp,
  forwardAuthAppFixture,
} from "@/test/forward-auth-app";

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

function puts() {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter((request) => request.method === "PUT");
}

function mount(
  app: ForwardAuthApp,
  extra?: (request: Request) => Response | undefined,
) {
  fetchBoundary.mockImplementation(async (request) => {
    const canned = extra?.(request);
    if (canned) return canned;
    if (request.method === "PUT") return Response.json(app);
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <ForwardAuthServiceSection app={app} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("the service section", () => {
  it("shows the hostname with no footer while nothing is edited", async () => {
    mount(forwardAuthAppFixture());

    expect(
      await screen.findByRole("textbox", { name: /^Hostname/ }),
    ).toHaveValue("grafana.example.test");
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("saves the hostname with the stored name as the whole record", async () => {
    const user = userEvent.setup();
    mount(forwardAuthAppFixture());

    const host = await screen.findByRole("textbox", { name: /^Hostname/ });
    await user.clear(host);
    await user.type(host, "metrics.example.test");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await puts()[0]?.json()).toEqual({
      displayName: "Grafana",
      host: "metrics.example.test",
    });
  });

  it("discards an edit and puts the saved value back", async () => {
    const user = userEvent.setup();
    mount(forwardAuthAppFixture());

    const host = await screen.findByRole("textbox", { name: /^Hostname/ });
    await user.type(host, "x");
    await user.click(screen.getByRole("button", { name: "Discard" }));

    expect(host).toHaveValue("grafana.example.test");
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
    expect(puts()).toHaveLength(0);
  });

  it.each([
    ["", "Enter a hostname."],
    [
      "https://app.example.test",
      "Use a lowercase hostname with no scheme and no port, such as app.example.com.",
    ],
  ])("refuses the hostname %j without sending", async (value, message) => {
    const user = userEvent.setup();
    mount(forwardAuthAppFixture());

    const host = await screen.findByRole("textbox", { name: /^Hostname/ });
    await user.clear(host);
    if (value !== "") await user.type(host, value);
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText(message)).toBeVisible();
    expect(puts()).toHaveLength(0);
  });

  it("puts a hostname another application already claims on the hostname", async () => {
    const user = userEvent.setup();
    mount(forwardAuthAppFixture(), (request) =>
      request.method === "PUT"
        ? Response.json(
            { code: "oidc_client_already_exists", requestId: "test" },
            { status: 409 },
          )
        : undefined,
    );

    const host = await screen.findByRole("textbox", { name: /^Hostname/ });
    await user.type(host, "x");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(
      await screen.findByText("That Client ID or host name is already in use."),
    ).toBeVisible();
    expect(host).toHaveAttribute("aria-invalid", "true");
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("reports a refused save once, on the section", async () => {
    const user = userEvent.setup();
    mount(forwardAuthAppFixture(), (request) =>
      request.method === "PUT"
        ? Response.json({ code: "internal_error" }, { status: 500 })
        : undefined,
    );

    await user.type(
      await screen.findByRole("textbox", { name: /^Hostname/ }),
      "x",
    );
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});
