import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ConsentRequest } from "@/api/raw-paths";
import { loadDocument } from "@/app/load-document";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import {
  apiError,
  fakeApi,
  publicApi,
  renderApp,
  testSession,
} from "@/test/app";

vi.mock("@/app/load-document", () => ({ loadDocument: vi.fn() }));

const authorize = "/oauth/authorize?client_id=wiki&scope=openid";
const address = `/consent?ticket=t-1&return_to=${encodeURIComponent(authorize)}`;

function request(overrides: Partial<ConsentRequest> = {}): ConsentRequest {
  return {
    client: {
      clientId: "wiki",
      displayName: "Wiki",
      policyUri: "https://wiki.example/privacy",
      tosUri: "https://wiki.example/terms",
      host: "wiki.example",
    },
    account: { displayName: "Alice" },
    scopes: ["openid", "profile", "email", "wiki:write"],
    ...overrides,
  };
}

let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  queryClient = createQueryClient(() => undefined);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
  vi.mocked(loadDocument).mockReset();
});

function consentApi(body: ConsentRequest = request()) {
  return fakeApi({
    ...publicApi(testSession),
    "GET /api/prohibitorum/consent": () => Response.json(body),
  });
}

function rowTitles(list: HTMLElement) {
  return within(list)
    .getAllByRole("listitem")
    .map(
      (item) =>
        item.querySelector('[data-slot="scope-row-title"]')?.textContent,
    );
}

function factNames() {
  return rowTitles(screen.getByRole("list", { name: "Wiki will be able to" }));
}

function inStageFooter(name: string) {
  const footer = document.querySelector('[data-slot="stage-footer"]');
  if (!(footer instanceof HTMLElement)) throw new Error("no stage footer");
  return within(footer).getByRole("button", { name });
}

describe("OIDC consent", () => {
  it("asks about every requested scope the first time, naming an app-defined one as written", async () => {
    const api = consentApi();
    renderApp(address, queryClient);
    expect(
      await screen.findByRole("heading", {
        name: "Allow Wiki to access your account?",
      }),
    ).toHaveFocus();
    expect(api.sent("GET", "/api/prohibitorum/consent")[0]?.url.search).toBe(
      "?ticket=t-1",
    );
    const names = factNames();
    expect(names).toEqual([
      "Confirm who you are",
      "Your profile",
      "Your email address",
      "wiki:write",
    ]);
    expect(screen.getByText("wiki:write")).toHaveClass("font-mono");
    expect(
      screen.getByText("Know which Test instance account is yours"),
    ).toBeVisible();
    expect(screen.getByText(/Signed in as/)).toHaveTextContent(
      "Signed in as Alice",
    );
    expect(screen.queryByText(/Already allowed/)).not.toBeInTheDocument();
    expect(
      document.querySelector('[data-slot="stage-strip"]'),
    ).toHaveTextContent("Asking for accessWikiwiki.example");
  });

  it("names the provider of an identity scope, and shows an unknown one by its slug", async () => {
    consentApi(
      request({
        scopes: [
          "openid",
          "identity:vrchat",
          "identity:gone",
          "identity:constructor",
        ],
        providers: { vrchat: "VRChat" },
      }),
    );
    renderApp(address, queryClient);
    await screen.findByRole("heading", {
      name: "Allow Wiki to access your account?",
    });
    expect(factNames()).toEqual([
      "Confirm who you are",
      "Your VRChat identity",
      "Your gone identity",
      "Your constructor identity",
    ]);
    expect(
      screen.getByText("Your VRChat ID, display name and profile link"),
    ).toBeVisible();
  });

  it("names an identity scope among the scopes allowed before", async () => {
    const user = userEvent.setup();
    consentApi(
      request({
        scopes: ["openid", "identity:vrchat", "groups"],
        alreadyGranted: ["openid", "identity:vrchat"],
        providers: { vrchat: "VRChat" },
      }),
    );
    renderApp(address, queryClient);
    await screen.findByRole("heading", {
      name: "Wiki is asking for more access",
    });
    await user.click(screen.getByRole("button", { name: /Already allowed/ }));
    expect(
      rowTitles(screen.getByRole("list", { name: /Already allowed/ })),
    ).toEqual(["Confirm who you are", "Your VRChat identity"]);
  });

  it("lists only the new scopes, with the allowed ones folded away until asked for", async () => {
    const user = userEvent.setup();
    consentApi(
      request({
        scopes: ["openid", "profile", "email", "groups", "wiki:write"],
        alreadyGranted: ["openid", "profile", "email"],
      }),
    );
    renderApp(address, queryClient);
    expect(
      await screen.findByRole("heading", {
        name: "Wiki is asking for more access",
      }),
    ).toBeVisible();
    expect(screen.getByRole("list", { name: "New for Wiki, 2" })).toBeVisible();
    expect(
      rowTitles(screen.getByRole("list", { name: /New for Wiki/ })),
    ).toEqual(["Your groups", "wiki:write"]);
    const trigger = screen.getByRole("button", { name: "Already allowed, 3" });
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(trigger).toHaveTextContent("3");
    await user.click(trigger);
    expect(trigger).toHaveAttribute("aria-expanded", "true");
    const allowed = screen.getByRole("list", { name: /Already allowed/ });
    expect(rowTitles(allowed)).toEqual([
      "Confirm who you are",
      "Your profile",
      "Your email address",
    ]);
    expect(
      within(allowed).getByText("Your display name, username and avatar"),
    ).toBeVisible();
  });

  it("links only the policies the application has", async () => {
    consentApi(
      request({
        client: {
          clientId: "wiki",
          displayName: "Wiki",
          tosUri: "https://wiki.example/terms",
        },
      }),
    );
    renderApp(address, queryClient);
    const terms = await screen.findByRole("link", {
      name: "Wiki's terms of service",
    });
    expect(terms).toHaveAttribute("href", "https://wiki.example/terms");
    expect(terms).toHaveAttribute("target", "_blank");
    expect(
      screen.queryByRole("link", { name: /privacy policy/ }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Connected applications" }),
    ).toHaveAttribute("href", "/apps");
  });

  it("sends the answer with the authorization to resume, and holds both buttons while it is out", async () => {
    let settle!: (response: Response) => void;
    const api = consentApi();
    api.routes["POST /api/prohibitorum/consent"] = () =>
      new Promise((resolve) => {
        settle = resolve;
      });
    const user = userEvent.setup();
    renderApp(address, queryClient);
    await screen.findByRole("heading", { name: /^Allow Wiki/ });
    expect(inStageFooter("Deny")).toBeEnabled();
    await user.click(inStageFooter("Allow"));
    await waitFor(() =>
      expect(api.sent("POST", "/api/prohibitorum/consent")).toHaveLength(1),
    );
    const [sent] = api.sent("POST", "/api/prohibitorum/consent");
    expect(sent?.url.searchParams.get("return_to")).toBe(authorize);
    expect(sent?.body).toEqual({ ticket: "t-1", decision: "approve" });
    expect(screen.getByRole("button", { name: "Deny" })).toBeDisabled();
    expect(screen.getByRole("button", { name: /Allow/ })).toHaveAttribute(
      "aria-busy",
      "true",
    );
    settle(Response.json({ redirect: authorize }));
    await waitFor(() => expect(loadDocument).toHaveBeenCalledWith(authorize));
    // The page is leaving, so the pressed button stays pending.
    expect(screen.getByRole("button", { name: "Deny" })).toBeDisabled();
  });

  it("denies, follows the application's callback, and gives the buttons back after a failure", async () => {
    const api = consentApi();
    const callback = "https://wiki.example/cb?error=access_denied";
    api.routes["POST /api/prohibitorum/consent"] = () =>
      apiError("server_error", 500);
    const user = userEvent.setup();
    renderApp(address, queryClient);
    await user.click(await screen.findByRole("button", { name: "Deny" }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Deny" })).toBeEnabled(),
    );
    expect(screen.getByRole("button", { name: "Allow" })).toBeEnabled();
    expect(loadDocument).not.toHaveBeenCalled();

    api.routes["POST /api/prohibitorum/consent"] = () =>
      Response.json({ redirect: callback });
    await user.click(screen.getByRole("button", { name: "Deny" }));
    await waitFor(() => expect(loadDocument).toHaveBeenCalledWith(callback));
    expect(api.sent("POST", "/api/prohibitorum/consent")[1]?.body).toEqual({
      ticket: "t-1",
      decision: "deny",
    });
  });

  it("offers to start the authorization again when the ticket is no longer usable", async () => {
    const api = consentApi();
    api.routes["GET /api/prohibitorum/consent"] = () =>
      apiError("invalid_consent_ticket", 400);
    const user = userEvent.setup();
    renderApp(address, queryClient);
    expect(
      await screen.findByRole("heading", { name: "Unable to continue" }),
    ).toHaveFocus();
    expect(
      screen.getByText(
        "This authorization request has expired or was already used.",
      ),
    ).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(loadDocument).toHaveBeenCalledWith(authorize);
  });

  it("offers the same restart when the answer itself used up the ticket", async () => {
    const api = consentApi();
    api.routes["POST /api/prohibitorum/consent"] = () =>
      apiError("invalid_consent_ticket", 400);
    const user = userEvent.setup();
    renderApp(address, queryClient);
    await user.click(await screen.findByRole("button", { name: "Allow" }));
    expect(
      await screen.findByRole("heading", { name: "Unable to continue" }),
    ).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(loadDocument).toHaveBeenCalledWith(authorize);
  });

  it("explains an unusable ticket without a usable authorization address", async () => {
    const api = fakeApi({
      ...publicApi(testSession),
      "GET /api/prohibitorum/consent": () =>
        apiError("invalid_consent_ticket", 400),
    });
    renderApp(
      `/consent?ticket=t-1&return_to=${encodeURIComponent("https://evil.example/authorize")}`,
      queryClient,
    );
    expect(
      await screen.findByRole("heading", { name: "Unable to continue" }),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Try again" }),
    ).not.toBeInTheDocument();
    expect(api.sent("GET", "/api/prohibitorum/consent")).toHaveLength(1);
  });

  it("sends an anonymous reader to sign in and come back to this page", async () => {
    const api = fakeApi({
      ...publicApi(null),
      "GET /api/prohibitorum/consent": () => apiError("no_session", 401),
    });
    const router = renderApp(address, queryClient);
    expect(
      await screen.findByRole("heading", { name: /^Sign in to / }),
    ).toBeVisible();
    expect(router.state.location.pathname).toBe("/login");
    expect(
      new URLSearchParams(router.state.location.searchStr).get("return_to"),
    ).toBe(address);
    expect(api.sent("GET", "/api/prohibitorum/consent")).toHaveLength(0);
  });

  it.each([
    ["the ticket", `/consent?return_to=${encodeURIComponent(authorize)}`],
    ["the authorization", "/consent?ticket=t-1"],
    [
      "exactly one ticket",
      `/consent?ticket=t-1&ticket=t-2&return_to=${encodeURIComponent(authorize)}`,
    ],
  ])(
    "refuses a link without %s and asks nothing of the server",
    async (_, path) => {
      const api = consentApi();
      renderApp(path, queryClient);
      expect(
        await screen.findByRole("heading", {
          name: "Unable to load this page",
        }),
      ).toBeVisible();
      expect(
        screen.getByText(
          "This authorization request has expired or was already used. Go back to the app and sign in again.",
        ),
      ).toBeVisible();
      expect(api.sent("GET", "/api/prohibitorum/consent")).toHaveLength(0);
    },
  );

  it("signs out and returns to the authorization after another account signs in", async () => {
    const api = consentApi();
    api.routes["POST /api/prohibitorum/auth/logout"] = () => {
      api.routes["GET /api/prohibitorum/me"] = () =>
        apiError("no_session", 401);
      return new Response(null, { status: 204 });
    };
    const user = userEvent.setup();
    const router = renderApp(address, queryClient);
    await user.click(
      await screen.findByRole("button", { name: "Switch account" }),
    );
    expect(
      await screen.findByRole("heading", { name: /^Sign in with / }),
    ).toBeVisible();
    expect(api.sent("POST", "/api/prohibitorum/auth/logout")).toHaveLength(1);
    expect(router.state.location.pathname).toBe("/login");
    expect(
      new URLSearchParams(router.state.location.searchStr).get("return_to"),
    ).toBe(authorize);
  });
});
