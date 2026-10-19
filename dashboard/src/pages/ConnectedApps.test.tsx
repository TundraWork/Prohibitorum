import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { ConnectedApps } from "@/pages/ConnectedApps";

type App = components["schemas"]["ConsentedApp"];

const daysAgo = (days: number) =>
  new Date(Date.now() - days * 86_400_000).toISOString();

const wiki: App = {
  clientId: "wiki",
  kind: "oidc",
  name: "Outline wiki",
  grantedAt: daysAgo(4),
  scopes: ["openid", "profile", "email"],
};
const github: App = {
  clientId: "https://github.example.test/saml",
  kind: "saml",
  name: "GitHub Enterprise",
  grantedAt: daysAgo(40),
  scopes: null,
};

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifySuccess = vi.fn<(message: MessageDescriptor) => void>();
const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifySuccess.mockReset();
  notifyError.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(notifyError, notifySuccess);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

function mount(
  apps: App[],
  extra?: (request: Request) => Response | undefined,
) {
  fetchBoundary.mockImplementation(async (request) => {
    const canned = extra?.(request);
    if (canned) return canned;
    const path = new URL(request.url).pathname;
    if (path.endsWith("/me/consent")) return Response.json(apps);
    if (path.endsWith("/auth/federation")) {
      return Response.json([
        {
          slug: "google",
          displayName: "Google",
          protocol: "oidc",
          allowSignIn: true,
        },
      ]);
    }
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <ConnectedApps />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

async function rows() {
  const list = await screen.findByRole("list", {
    name: "Connected applications",
  });
  return within(list).getAllByRole("listitem");
}

describe("the page", () => {
  it("names itself and says what removing an application does", async () => {
    mount([wiki]);

    expect(
      await screen.findByRole("heading", {
        level: 1,
        name: "Connected applications",
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "Applications you allowed to sign you in. Removing one makes it ask again next time.",
      ),
    ).toBeInTheDocument();
  });

  it("says so plainly when nothing has been approved", async () => {
    mount([]);

    expect(
      await screen.findByText("No approved applications yet"),
    ).toBeInTheDocument();
  });
});

describe("the connected application rows", () => {
  it("shows the app, its kind, what it may read and when it was allowed", async () => {
    mount([wiki, github]);

    const [first, second] = (await rows()) as [HTMLElement, HTMLElement];
    expect(first).toHaveTextContent("Outline wiki");
    expect(first.querySelector("[data-slot='item-badge']")).toHaveTextContent(
      "OpenID Connect",
    );
    expect(first.querySelector("[data-slot='item-meta']")).toHaveTextContent(
      "Allowed 4 days ago",
    );
    // `kind` is echoed, not derived from the client id.
    expect(second).toHaveTextContent("GitHub Enterprise");
    expect(second.querySelector("[data-slot='item-badge']")).toHaveTextContent(
      "SAML",
    );
  });

  it("names the granted scopes the way the consent page does", async () => {
    mount([wiki]);

    const [row] = (await rows()) as [HTMLElement];
    const details = row.querySelector("[data-slot='item-details']");
    expect(details).toHaveTextContent(
      "Confirm who you are, your profile, and your email address",
    );
    expect(details).not.toHaveTextContent("openid");
  });

  it("shows a scope the app defined as written, and a provider scope by its display name", async () => {
    mount([{ ...wiki, scopes: ["identity:google", "wiki:write"] }]);

    const [row] = (await rows()) as [HTMLElement];
    const details = row.querySelector("[data-slot='item-details']");
    await waitFor(() =>
      expect(details).toHaveTextContent("Your Google identity and wiki:write"),
    );
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("keeps the slug and reports nothing when the provider lookup fails", async () => {
    mount([{ ...wiki, scopes: ["identity:google"] }], (request) =>
      new URL(request.url).pathname.endsWith("/auth/federation")
        ? new Response(null, { status: 500 })
        : undefined,
    );

    const [row] = (await rows()) as [HTMLElement];
    await waitFor(() =>
      expect(
        fetchBoundary.mock.calls.some(([request]) =>
          new URL(request.url).pathname.endsWith("/auth/federation"),
        ),
      ).toBe(true),
    );
    expect(row.querySelector("[data-slot='item-details']")).toHaveTextContent(
      "Your google identity",
    );
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("keeps the slug of a provider the instance does not list", async () => {
    mount([{ ...wiki, scopes: ["identity:okta"] }]);

    const [row] = (await rows()) as [HTMLElement];
    expect(row.querySelector("[data-slot='item-details']")).toHaveTextContent(
      "Your okta identity",
    );
  });

  it("says basic profile when the grant carries no scopes", async () => {
    mount([github, { ...wiki, clientId: "empty", scopes: [] }]);

    const [first, second] = (await rows()) as [HTMLElement, HTMLElement];
    expect(first).toHaveTextContent("Basic profile");
    expect(second).toHaveTextContent("Basic profile");
  });
});

describe("removing access", () => {
  it("asks first, then sends the app's id and kind and reloads the list", async () => {
    const user = userEvent.setup();
    let remaining = [wiki, github];
    mount([], (request) => {
      const path = new URL(request.url).pathname;
      if (path.endsWith("/me/consent/revoke")) {
        remaining = [wiki];
        return new Response(null, { status: 204 });
      }
      if (path.endsWith("/me/consent")) return Response.json(remaining);
      return undefined;
    });

    const [, second] = (await rows()) as [HTMLElement, HTMLElement];
    await user.click(
      within(second).getByRole("button", { name: "Remove access" }),
    );
    const dialog = await screen.findByRole("alertdialog", {
      name: "Remove access?",
    });
    expect(fetchBoundary.mock.calls.some(([r]) => r.method === "POST")).toBe(
      false,
    );
    await user.click(
      within(dialog).getByRole("button", { name: "Remove access" }),
    );

    await waitFor(() => expect(notifySuccess).toHaveBeenCalledTimes(1));
    const sent = fetchBoundary.mock.calls
      .map(([r]) => r)
      .find((r) => r.method === "POST");
    expect(await sent?.json()).toEqual({
      clientId: github.clientId,
      kind: "saml",
    });
    await waitFor(async () => expect(await rows()).toHaveLength(1));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    expect(screen.queryByText("GitHub Enterprise")).not.toBeInTheDocument();
  });

  it("keeps the app when the dialog is dismissed", async () => {
    const user = userEvent.setup();
    mount([wiki]);

    const [row] = (await rows()) as [HTMLElement];
    await user.click(
      within(row).getByRole("button", { name: "Remove access" }),
    );
    await user.click(await screen.findByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());

    expect(fetchBoundary.mock.calls.some(([r]) => r.method === "POST")).toBe(
      false,
    );
    expect(screen.getByText("Outline wiki")).toBeInTheDocument();
  });

  it("shows a failed removal once, in the page, without a toast", async () => {
    const user = userEvent.setup();
    mount([wiki], (request) =>
      new URL(request.url).pathname.endsWith("/me/consent/revoke")
        ? Response.json(
            { code: "rate_limited", requestId: "r1" },
            { status: 429 },
          )
        : undefined,
    );

    const [row] = (await rows()) as [HTMLElement];
    await user.click(
      within(row).getByRole("button", { name: "Remove access" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Remove access" }),
    );

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Too many requests. Wait a moment.");
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
    expect(notifySuccess).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(screen.getByText("Outline wiki")).toBeInTheDocument();
  });
});

describe("when the list cannot be loaded", () => {
  it("offers Try again without a toast, and loads on the retry", async () => {
    const user = userEvent.setup();
    let failing = true;
    mount([wiki], (request) =>
      failing && new URL(request.url).pathname.endsWith("/me/consent")
        ? new Response(null, { status: 500 })
        : undefined,
    );

    const retry = await screen.findByRole("button", { name: "Try again" });
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();

    failing = false;
    await user.click(retry);

    expect(await screen.findByText("Outline wiki")).toBeInTheDocument();
    expect(notifyError).not.toHaveBeenCalled();
  });
});
