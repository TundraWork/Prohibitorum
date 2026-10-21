import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FederationConfirm } from "@/api/raw-paths";
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

const confirmPath = "/api/prohibitorum/auth/federation/-/confirm";

function account(
  overrides: Partial<FederationConfirm> = {},
): FederationConfirm {
  return {
    idpDisplayName: "GitLab",
    displayName: "Alice Liddell",
    username: "alice",
    email: "alice@example.com",
    returnTo: "/",
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

function welcomeApi(
  body: FederationConfirm = account(),
  session: typeof testSession | null = null,
) {
  return fakeApi({
    ...publicApi(session),
    [`GET ${confirmPath}`]: () => Response.json(body),
  });
}

describe("the account a first federated sign-in prepared", () => {
  it("shows the account and where it came from", async () => {
    welcomeApi();
    renderApp("/welcome", queryClient);
    expect(
      await screen.findByRole("heading", { name: "Is this your account?" }),
    ).toHaveFocus();
    expect(
      screen.getByText(
        "You're signing in through GitLab for the first time, and Test instance has prepared this account for you.",
      ),
    ).toBeVisible();
    expect(screen.getByText("Alice Liddell")).toBeVisible();
    expect(screen.getByText("@alice")).toBeVisible();
    expect(screen.getByText("alice@example.com")).toBeVisible();
  });

  it("shows the initials when the account has no picture", async () => {
    welcomeApi(account());
    renderApp("/welcome", queryClient);
    await screen.findByRole("heading", { name: "Is this your account?" });
    expect(screen.getByText("AL")).toBeInTheDocument();
    expect(document.querySelector("img")).toBeNull();
  });

  it("leaves the email out when the account has none", async () => {
    welcomeApi(account({ email: undefined }));
    renderApp("/welcome", queryClient);
    await screen.findByRole("heading", { name: "Is this your account?" });
    expect(screen.getByText("@alice")).toBeVisible();
    expect(screen.queryByText(/@example\.com/)).toBeNull();
  });

  it("offers a local sign-in when the server asks for it", async () => {
    const api = welcomeApi(account(), testSession);
    api.routes[`POST ${confirmPath}`] = () =>
      Response.json({ redirect: "/apps", offerLocalSignin: true });
    api.routes["GET /api/prohibitorum/me/identities"] = () =>
      Response.json([
        {
          id: 1,
          providerSlug: "gitlab",
          providerDisplayName: "GitLab",
          protocol: "oidc",
          subject: "1",
          linkedAt: "2026-09-01T00:00:00Z",
          data: {},
        },
      ]);
    const user = userEvent.setup();
    const router = renderApp("/welcome", queryClient);
    await user.click(await screen.findByRole("button", { name: "Continue" }));
    expect(
      await screen.findByRole("heading", { name: "Add a way to sign in" }),
    ).toBeVisible();
    expect(router.state.location.pathname).toBe("/setup-signin");
    expect(
      new URLSearchParams(router.state.location.searchStr).get("redirect"),
    ).toBe("/apps");
    expect(api.sent("POST", confirmPath)[0]?.body).toEqual({});
  });

  it("goes straight on otherwise, holding both buttons while it leaves", async () => {
    const api = welcomeApi();
    const target = "/oauth/authorize?client_id=wiki";
    api.routes[`POST ${confirmPath}`] = () =>
      Response.json({ redirect: target, offerLocalSignin: false });
    const user = userEvent.setup();
    renderApp("/welcome", queryClient);
    await user.click(await screen.findByRole("button", { name: "Continue" }));
    await waitFor(() => expect(loadDocument).toHaveBeenCalledWith(target));
    expect(
      screen.getByRole("button", { name: "This isn't me" }),
    ).toBeDisabled();
  });

  it("turns the account down and goes to the sign-in page", async () => {
    const api = welcomeApi();
    api.routes[`POST ${confirmPath}/decline`] = () =>
      new Response(null, { status: 204 });
    const user = userEvent.setup();
    const router = renderApp("/welcome", queryClient);
    await user.click(
      await screen.findByRole("button", { name: "This isn't me" }),
    );
    expect(
      await screen.findByRole("heading", { name: /^Sign in to / }),
    ).toBeVisible();
    expect(router.state.location.pathname).toBe("/login");
    expect(api.sent("POST", `${confirmPath}/decline`)).toHaveLength(1);
    expect(api.sent("POST", confirmPath)).toHaveLength(0);
  });

  it("says an expired sign-in has to start again", async () => {
    fakeApi({
      ...publicApi(null),
      [`GET ${confirmPath}`]: () => apiError("federation_state_invalid", 400),
    });
    renderApp("/welcome", queryClient);
    expect(
      await screen.findByText("This sign-in has expired. Sign in again."),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Back to sign-in" }),
    ).toBeVisible();
  });

  it("names the application the sign-in continues to", async () => {
    welcomeApi(
      account({
        application: {
          label: "Wiki",
          host: "wiki.example.test",
          iconUrl: "/icon/oidc_client/wiki?v=abcd1234",
        },
      }),
    );
    renderApp("/welcome", queryClient);
    await screen.findByRole("heading", { name: "Is this your account?" });
    const strip = document.querySelector('[data-slot="stage-strip"]');
    expect(strip).toHaveTextContent("You're signing in to");
    expect(strip).toHaveTextContent("Wiki");
    expect(strip).toHaveTextContent("wiki.example.test");
  });

  it("keeps the application when the account is turned down", async () => {
    const returnTo = "/oauth/authorize?client_id=wiki";
    const api = welcomeApi(account({ returnTo }));
    api.routes[`POST ${confirmPath}/decline`] = () =>
      new Response(null, { status: 204 });
    const user = userEvent.setup();
    const router = renderApp("/welcome", queryClient);
    await user.click(
      await screen.findByRole("button", { name: "This isn't me" }),
    );
    await screen.findByRole("heading", { name: /^Sign in with / });
    expect(router.state.location.pathname).toBe("/login");
    expect(router.state.location.search).toEqual({ return_to: returnTo });
  });
});
