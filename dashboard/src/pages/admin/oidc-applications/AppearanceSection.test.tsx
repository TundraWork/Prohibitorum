import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { AppearanceSection } from "@/pages/admin/oidc-applications/AppearanceSection";
import { type OidcApp, oidcAppFixture } from "@/test/oidc-app";

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
  app: OidcApp,
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
        <AppearanceSection app={app} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("the appearance section", () => {
  it("holds no footer while nothing is edited", async () => {
    mount(oidcAppFixture());
    await screen.findByRole("textbox", { name: "Name" });
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("saves a new name merged into the stored record", async () => {
    const user = userEvent.setup();
    mount(oidcAppFixture({ requireConsent: true }));

    const name = await screen.findByRole("textbox", { name: "Name" });
    await user.clear(name);
    await user.type(name, "Metrics");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await puts()[0]?.json()).toMatchObject({
      displayName: "Metrics",
      launchUrl: "https://grafana.example.test",
      requireConsent: true,
      allowedScopes: ["openid", "profile", "email"],
    });
  });

  it("clears the launch address when it is emptied", async () => {
    const user = userEvent.setup();
    mount(oidcAppFixture());

    await user.clear(
      await screen.findByRole("textbox", { name: "Launch URL" }),
    );
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await puts()[0]?.json()).toMatchObject({ launchUrl: null });
  });

  it("refuses an empty name and a launch address that is not absolute", async () => {
    const user = userEvent.setup();
    mount(oidcAppFixture());

    await user.clear(await screen.findByRole("textbox", { name: "Name" }));
    const launch = screen.getByRole("textbox", { name: "Launch URL" });
    await user.clear(launch);
    await user.type(launch, "grafana");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("Enter a name.")).toBeVisible();
    expect(
      screen.getByText("Use an absolute http or https address."),
    ).toBeVisible();
    expect(puts()).toHaveLength(0);
  });

  it("reports a refused save once, on the section", async () => {
    const user = userEvent.setup();
    mount(oidcAppFixture(), (request) =>
      request.method === "PUT"
        ? Response.json({ code: "internal_error" }, { status: 500 })
        : undefined,
    );

    const name = await screen.findByRole("textbox", { name: "Name" });
    await user.type(name, "!");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});
