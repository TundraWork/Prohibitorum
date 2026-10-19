import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { SamlSignInSection } from "@/pages/admin/saml-applications/SamlSignInSection";
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
        <SamlSignInSection app={app} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("the sign-in section", () => {
  it("holds no footer while nothing is edited", async () => {
    mount(samlAppFixture());
    await screen.findByRole("textbox", { name: "Session lifetime" });
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("shows a stored lifetime in minutes", async () => {
    mount(samlAppFixture({ sessionLifetimeSecs: 5400 }));
    expect(
      await screen.findByRole("textbox", { name: "Session lifetime" }),
    ).toHaveValue("90");
  });

  it("sends the lifetime in seconds with the rest of the record as stored", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture({ allowIdpInitiated: true }));

    await user.type(
      await screen.findByRole("textbox", { name: "Session lifetime" }),
      "30",
    );
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await puts()[0]?.json()).toEqual({
      displayName: "Company wiki",
      nameIdFormat: "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent",
      attributeMap: samlAppFixture().attributeMap,
      requireSignedAuthnRequest: false,
      allowIdpInitiated: true,
      sessionLifetimeSecs: 1800,
    });
  });

  it("sends the two switches as chosen", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture());

    await user.click(
      await screen.findByRole("switch", {
        name: "Require a signed AuthnRequest",
      }),
    );
    await user.click(
      screen.getByRole("switch", { name: "Allow IdP-initiated sign-in" }),
    );
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await puts()[0]?.json()).toMatchObject({
      requireSignedAuthnRequest: true,
      allowIdpInitiated: true,
    });
  });

  it("sends no lifetime once the box is emptied", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture({ sessionLifetimeSecs: 3600 }));

    await user.clear(
      await screen.findByRole("textbox", { name: "Session lifetime" }),
    );
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await puts()[0]?.json()).not.toHaveProperty("sessionLifetimeSecs");
  });

  it("refuses a negative lifetime without sending", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture());

    await user.type(
      await screen.findByRole("textbox", { name: "Session lifetime" }),
      "-5",
    );
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(
      await screen.findByText(
        "Use a whole number of minutes, or leave it empty.",
      ),
    ).toBeVisible();
    expect(puts()).toHaveLength(0);
  });

  it("puts the edit back on Discard", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture());

    await user.click(
      await screen.findByRole("switch", {
        name: "Allow IdP-initiated sign-in",
      }),
    );
    await user.click(screen.getByRole("button", { name: "Discard" }));

    expect(
      screen.getByRole("switch", { name: "Allow IdP-initiated sign-in" }),
    ).not.toBeChecked();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("reports a refused save once, on the section", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture(), (request) =>
      request.method === "PUT"
        ? Response.json({ code: "internal_error" }, { status: 500 })
        : undefined,
    );

    await user.click(
      await screen.findByRole("switch", {
        name: "Allow IdP-initiated sign-in",
      }),
    );
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("correcting a refused lifetime", () => {
  it("saves once the lifetime is a number again", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture());

    const lifetime = await screen.findByRole("textbox", {
      name: "Session lifetime",
    });
    await user.type(lifetime, "-5");
    await user.click(screen.getByRole("button", { name: "Save" }));
    await screen.findByText(
      "Use a whole number of minutes, or leave it empty.",
    );

    await user.clear(lifetime);
    await user.type(lifetime, "10");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await puts()[0]?.json()).toMatchObject({ sessionLifetimeSecs: 600 });
    expect(
      screen.queryByText("Use a whole number of minutes, or leave it empty."),
    ).toBeNull();
  });
});
