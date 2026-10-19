import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { SamlAppearanceSection } from "@/pages/admin/saml-applications/SamlAppearanceSection";
import { type SamlApp, samlAppFixture } from "@/test/saml-app";

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
  app: SamlApp,
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
        <SamlAppearanceSection app={app} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("the appearance section", () => {
  it("holds no footer while nothing is edited", async () => {
    mount(samlAppFixture());
    await screen.findByRole("textbox", { name: "Name" });
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("saves the name with the rest of the record as stored", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture({ sessionLifetimeSecs: 3600 }));

    const name = await screen.findByRole("textbox", { name: "Name" });
    await user.clear(name);
    await user.type(name, "Wiki");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await puts()[0]?.json()).toEqual({
      displayName: "Wiki",
      nameIdFormat: "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent",
      attributeMap: samlAppFixture().attributeMap,
      requireSignedAuthnRequest: false,
      allowIdpInitiated: false,
      sessionLifetimeSecs: 3600,
    });
  });

  it("refuses an empty name without sending", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture());

    await user.clear(await screen.findByRole("textbox", { name: "Name" }));
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("Enter a name.")).toBeVisible();
    expect(puts()).toHaveLength(0);
  });

  it("offers the icon controls", async () => {
    mount(samlAppFixture());
    expect(
      await screen.findByRole("button", { name: "Upload an icon" }),
    ).toBeVisible();
  });

  it("reports a refused save once, on the section", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture(), (request) =>
      request.method === "PUT"
        ? Response.json({ code: "internal_error" }, { status: 500 })
        : undefined,
    );

    await user.type(await screen.findByRole("textbox", { name: "Name" }), "!");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});
