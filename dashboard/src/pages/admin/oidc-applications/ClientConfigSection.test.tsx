import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { ClientConfigSection } from "@/pages/admin/oidc-applications/ClientConfigSection";
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
    const path = new URL(request.url).pathname;
    if (request.method === "GET" && path.endsWith("/auth/federation")) {
      return Response.json([
        { slug: "github", displayName: "GitHub", protocol: "oidc" },
      ]);
    }
    if (request.method === "PUT") return Response.json(app);
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <ClientConfigSection app={app} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

async function scope(name: string) {
  return screen.findByRole("checkbox", { name });
}

async function untick(user: UserEvent, name: string) {
  await user.click(await scope(name));
}

describe("the sign-in section", () => {
  it("draws every scope as a row with what it gives the client", async () => {
    mount(oidcAppFixture());

    expect(await scope("openid")).toBeChecked();
    expect(
      screen.getByText("Required to sign in. Always included."),
    ).toBeVisible();
    expect(await scope("profile")).toBeChecked();
    expect(await scope("groups")).not.toBeChecked();
    expect(await scope("identity:github")).not.toBeChecked();
    expect(
      screen.getByText("The user's linked GitHub identities."),
    ).toBeVisible();
  });

  it("holds no footer while nothing is edited", async () => {
    mount(oidcAppFixture());
    await scope("openid");
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Discard" })).toBeNull();
  });

  it("saves an addition at once, merged into the stored record", async () => {
    const user = userEvent.setup();
    mount(oidcAppFixture({ displayName: "Grafana", disabled: true }));

    await user.click(await scope("groups"));
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(screen.queryByRole("alertdialog")).toBeNull();
    const body = await puts()[0]?.json();
    expect(body).toMatchObject({
      displayName: "Grafana",
      disabled: true,
      launchUrl: "https://grafana.example.test",
      allowedScopes: ["openid", "profile", "email", "groups"],
      requirePkce: true,
    });
  });

  it("states the consequence of removing a scope and names the action", async () => {
    const user = userEvent.setup();
    mount(oidcAppFixture());

    await untick(user, "email");

    // The scope name is its own element, so the sentence is read off the paragraph.
    expect(
      await screen.findByText(
        (_, element) =>
          element?.tagName === "P" &&
          /^Clients still asking for\s*email\s*will be refused until they stop\.$/.test(
            element.textContent ?? "",
          ),
      ),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Remove scope and save" }),
    ).toBeVisible();
    expect(screen.getByText("Will be removed")).toBeVisible();
  });

  it("sends the removal only once it is confirmed", async () => {
    const user = userEvent.setup();
    mount(oidcAppFixture());

    await untick(user, "email");
    await user.click(
      screen.getByRole("button", { name: "Remove scope and save" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    expect(puts()).toHaveLength(0);
    await user.click(
      within(dialog).getByRole("button", { name: "Remove and save" }),
    );

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await puts()[0]?.json()).toMatchObject({
      allowedScopes: ["openid", "profile"],
    });
  });

  it("puts the scope back when the edit is discarded", async () => {
    const user = userEvent.setup();
    mount(oidcAppFixture());

    await untick(user, "email");
    await user.click(screen.getByRole("button", { name: "Discard" }));

    expect(await scope("email")).toBeChecked();
    expect(screen.queryByText("Will be removed")).toBeNull();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("keeps a scope whose provider is gone, with the reason, so it can be cleared", async () => {
    mount(oidcAppFixture({ allowedScopes: ["openid", "identity:retired"] }));

    expect(await scope("identity:retired")).toBeChecked();
    expect(
      screen.getByText(
        "This provider is turned off or no longer exists. Clear it to stop allowing it.",
      ),
    ).toBeVisible();
  });

  it("locks PKCE on for a public client and says why on the page", async () => {
    mount(oidcAppFixture({ clientAuthMethod: "none" }));

    const pkce = await screen.findByRole("switch", { name: "Require PKCE" });
    expect(pkce).toBeChecked();
    expect(pkce).toHaveAttribute("aria-disabled", "true");
    expect(
      screen.getByText("A public client always proves the request it started."),
    ).toBeVisible();
  });

  it("marks the redirect address that is wrong", async () => {
    const user = userEvent.setup();
    mount(oidcAppFixture());

    const input = await screen.findByRole("textbox", {
      name: "Redirect URIs 1",
    });
    await user.type(input, " ");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(
      await screen.findByText("Remove the spaces at either end."),
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

    await user.click(await scope("groups"));
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});
