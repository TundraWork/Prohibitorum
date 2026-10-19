import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { ForwardAuthAppearanceSection } from "@/pages/admin/forward-auth-apps/ForwardAuthAppearanceSection";
import { forwardAuthAppFixture } from "@/test/forward-auth-app";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(
    vi.fn<(error: unknown) => void>(),
    vi.fn<(message: MessageDescriptor) => void>(),
  );
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

function mount() {
  fetchBoundary.mockImplementation(async (request) =>
    request.method === "PUT"
      ? Response.json(forwardAuthAppFixture())
      : new Response(null, { status: 204 }),
  );
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <ForwardAuthAppearanceSection app={forwardAuthAppFixture()} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

function puts() {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter((request) => request.method === "PUT");
}

describe("the appearance section", () => {
  it("offers the icon controls under its own heading", () => {
    render(
      <I18nProvider i18n={i18n}>
        <QueryClientProvider client={queryClient}>
          <ForwardAuthAppearanceSection app={forwardAuthAppFixture()} />
        </QueryClientProvider>
      </I18nProvider>,
    );

    expect(
      screen.getByRole("heading", { level: 2, name: "Appearance" }),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Upload an icon" }),
    ).toBeVisible();
  });

  it("saves the name with the stored hostname as the whole record", async () => {
    const user = userEvent.setup();
    mount();

    const name = await screen.findByRole("textbox", { name: "Name" });
    await user.clear(name);
    await user.type(name, "Metrics");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await puts()[0]?.json()).toEqual({
      displayName: "Metrics",
      host: "grafana.example.test",
    });
  });

  it("refuses an empty name without sending", async () => {
    const user = userEvent.setup();
    mount();

    await user.clear(await screen.findByRole("textbox", { name: "Name" }));
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("Enter a name.")).toBeVisible();
    expect(puts()).toHaveLength(0);
  });

  it("discards an edit and puts the saved name back", async () => {
    const user = userEvent.setup();
    mount();

    const name = await screen.findByRole("textbox", { name: "Name" });
    await user.type(name, "!");
    await user.click(screen.getByRole("button", { name: "Discard" }));

    expect(name).toHaveValue("Grafana");
    expect(puts()).toHaveLength(0);
  });
});
