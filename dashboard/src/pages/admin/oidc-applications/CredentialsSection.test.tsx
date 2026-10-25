import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { configureSudo, resetSudo } from "@/api/sudo";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { CredentialsSection } from "@/pages/admin/oidc-applications/CredentialsSection";
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

function rotations() {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter((request) =>
      new URL(request.url).pathname.endsWith("/rotate-secret"),
    );
}

function mount(
  app: OidcApp,
  extra?: (request: Request) => Response | undefined,
) {
  fetchBoundary.mockImplementation(async (request) => {
    const canned = extra?.(request);
    if (canned) return canned;
    const path = new URL(request.url).pathname;
    if (path.endsWith("/me/sudo/methods")) {
      return Response.json({ methods: ["password_totp"], fresh: true });
    }
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <CredentialsSection app={app} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("the credentials section", () => {
  it("says a public client has no secret and offers nothing to rotate", () => {
    mount(oidcAppFixture({ clientAuthMethod: "none" }));

    expect(
      screen.getByRole("heading", { level: 2, name: "Credentials" }),
    ).toBeVisible();
    expect(
      screen.getByText(/Public clients have no client secret/),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: /Rotate/ })).toBeNull();
  });

  it("rotates only after the confirmation", async () => {
    const user = userEvent.setup();
    mount(oidcAppFixture());

    await user.click(screen.getByRole("button", { name: "Rotate secret" }));
    const dialog = await screen.findByRole("alertdialog", {
      name: "Rotate the client secret?",
    });
    expect(rotations()).toHaveLength(0);
    await user.click(
      within(dialog).getByRole("button", { name: "Rotate secret" }),
    );

    await waitFor(() => expect(rotations()).toHaveLength(1));
    expect(await rotations()[0]?.json()).toEqual({ clientId: "grafana" });
  });

  it("reports a refused rotation once, on the section", async () => {
    const user = userEvent.setup();
    mount(oidcAppFixture(), (request) =>
      new URL(request.url).pathname.endsWith("/rotate-secret")
        ? Response.json({ code: "internal_error" }, { status: 500 })
        : undefined,
    );

    await user.click(screen.getByRole("button", { name: "Rotate secret" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Rotate secret" }),
    );

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("asks for the identity check before rotating when the window is closed", async () => {
    const user = userEvent.setup();
    const set = vi.fn();
    configureSudo({
      queryClient,
      set,
      setFresh: () => {},
      getFresh: () => false,
    });
    mount(oidcAppFixture(), (request) =>
      new URL(request.url).pathname.endsWith("/me/sudo/methods")
        ? Response.json({ methods: ["password_totp"], fresh: false })
        : undefined,
    );

    await user.click(screen.getByRole("button", { name: "Rotate secret" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Rotate secret" }),
    );

    await waitFor(() => expect(set).toHaveBeenCalled());
    expect(rotations()).toHaveLength(0);
  });
});
