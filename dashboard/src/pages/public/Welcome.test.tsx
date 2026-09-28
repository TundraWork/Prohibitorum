import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { federationConfirmQueryOptions } from "@/api/queries";
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

const confirmPath = "/api/prohibitorum/auth/federation/confirm";
const key = federationConfirmQueryOptions().queryKey;

function account(
  overrides: Partial<FederationConfirm> = {},
): FederationConfirm {
  return {
    idpDisplayName: "GitLab",
    displayName: "Alice Liddell",
    username: "alice",
    email: "alice@example.com",
    avatarPending: false,
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

function skeleton() {
  return document.querySelector(".skeleton");
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
    expect(skeleton()).toBeNull();
  });

  it("polls every 1.5 seconds while the picture is pending, for 20 reads after the first", () => {
    const interval = federationConfirmQueryOptions().refetchInterval as (
      query: unknown,
    ) => number | false;
    const at = (data: FederationConfirm, dataUpdateCount: number) =>
      interval({ state: { data, dataUpdateCount } });
    const pending = account({ avatarPending: true });
    expect(at(pending, 1)).toBe(1500);
    expect(at(pending, 20)).toBe(1500);
    expect(at(pending, 21)).toBe(false);
    expect(at(account(), 2)).toBe(false);
  });

  it("holds the picture's place until it is ready", async () => {
    welcomeApi(account({ avatarPending: true }));
    renderApp("/welcome", queryClient);
    await screen.findByRole("heading", { name: "Is this your account?" });
    expect(skeleton()).not.toBeNull();
    queryClient.setQueryData(
      key,
      account({ avatarUrl: "https://id.example/avatar.png" }),
    );
    await waitFor(() => expect(skeleton()).toBeNull());
    expect(screen.getByText("A")).toBeInTheDocument();
  });

  it("stops waiting after the last read and shows the initial", async () => {
    welcomeApi(account({ avatarPending: true }));
    renderApp("/welcome", queryClient);
    await screen.findByRole("heading", { name: "Is this your account?" });
    // Every read after the first, the way the poll would land them.
    const reads = () => queryClient.getQueryState(key)?.dataUpdateCount ?? 0;
    while (reads() < 20) {
      queryClient.setQueryData(key, account({ avatarPending: true }));
    }
    await waitFor(() => expect(reads()).toBe(20));
    expect(skeleton()).not.toBeNull();
    queryClient.setQueryData(key, account({ avatarPending: true }));
    await waitFor(() => expect(skeleton()).toBeNull());
    expect(screen.getByText("A")).toBeInTheDocument();
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
      await screen.findByRole("heading", { name: "Sign in" }),
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
});
