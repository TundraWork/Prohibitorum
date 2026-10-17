import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RequireIdentityRequest } from "@/api/raw-paths";
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

const address = "/require-identity/t-1";
const entry =
  "https://idp.example/api/prohibitorum/require-identity?client_id=wiki&providers=vrchat";

function request(
  overrides: Partial<RequireIdentityRequest> = {},
): RequireIdentityRequest {
  return {
    client: { displayName: "Wiki" },
    redirectUri: "https://wiki.example/after-verify",
    entryUrl: entry,
    mismatch: false,
    satisfied: false,
    providers: [
      { slug: "vrchat", displayName: "VRChat" },
      { slug: "steam", displayName: "Steam" },
    ],
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

function requireIdentityApi(
  reply: () => Response = () => Response.json(request()),
) {
  return fakeApi({
    ...publicApi(testSession),
    "GET /api/prohibitorum/require-identity/t-1": reply,
    "GET /api/prohibitorum/me/sudo/methods": () =>
      Response.json({ methods: ["password_totp"], fresh: true }),
  });
}

describe("require identity", () => {
  it("names the app and its providers, and links the one picked back to this page", async () => {
    requireIdentityApi();
    const user = userEvent.setup();
    renderApp(address, queryClient);
    expect(
      await screen.findByRole("heading", {
        name: "Wiki needs to verify your identity",
      }),
    ).toBeVisible();
    expect(screen.getByText("Verify with any one of these.")).toBeVisible();
    await user.click(
      screen.getByRole("button", { name: "Verify with VRChat" }),
    );
    await waitFor(() =>
      expect(loadDocument).toHaveBeenCalledWith(
        "/api/prohibitorum/me/identities/link/vrchat/begin?return_to=%2Frequire-identity%2Ft-1",
      ),
    );
  });

  it("says nothing about choosing when there is one provider", async () => {
    requireIdentityApi(() =>
      Response.json(
        request({ providers: [{ slug: "vrchat", displayName: "VRChat" }] }),
      ),
    );
    renderApp(address, queryClient);
    await screen.findByRole("heading", {
      name: "Wiki needs to verify your identity",
    });
    expect(
      screen.queryByText("Verify with any one of these."),
    ).not.toBeInTheDocument();
  });

  it("returns to the app without verifying", async () => {
    requireIdentityApi();
    const user = userEvent.setup();
    renderApp(address, queryClient);
    await user.click(
      await screen.findByRole("button", { name: "Return to Wiki" }),
    );
    expect(loadDocument).toHaveBeenCalledWith(
      "https://wiki.example/after-verify",
    );
  });

  it("goes straight back to the app once an identity is linked", async () => {
    requireIdentityApi(() => Response.json(request({ satisfied: true })));
    renderApp(address, queryClient);
    await waitFor(() =>
      expect(loadDocument).toHaveBeenCalledWith(
        "https://wiki.example/after-verify",
      ),
    );
    expect(
      screen.queryByRole("button", { name: /Verify with/ }),
    ).not.toBeInTheDocument();
  });

  it("offers to sign in as the app's account instead of linking to this one", async () => {
    const api = requireIdentityApi(() =>
      Response.json(request({ mismatch: true, satisfied: true })),
    );
    api.routes["POST /api/prohibitorum/auth/logout"] = () => {
      api.routes["GET /api/prohibitorum/me"] = () =>
        apiError("no_session", 401);
      return new Response(null, { status: 204 });
    };
    const user = userEvent.setup();
    const router = renderApp(address, queryClient);
    expect(
      await screen.findByRole("heading", {
        name: "You're signed in as a different account",
      }),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: /Verify with/ }),
    ).not.toBeInTheDocument();
    expect(loadDocument).not.toHaveBeenCalled();
    await user.click(
      screen.getByRole("button", { name: "Sign out and use another account" }),
    );
    expect(
      await screen.findByRole("heading", { name: "Sign in" }),
    ).toBeVisible();
    expect(router.state.location.pathname).toBe("/login");
    expect(
      new URLSearchParams(router.state.location.searchStr).get("return_to"),
    ).toBe(entry);
  });

  it("says an expired request is gone", async () => {
    requireIdentityApi(() => apiError("require_identity_ticket_expired", 410));
    renderApp(address, queryClient);
    expect(
      await screen.findByText(
        "This request has expired. Go back to the app and start again.",
      ),
    ).toBeVisible();
  });

  it("sends a signed-out reader to sign in and back", async () => {
    const api = fakeApi({ ...publicApi(null) });
    const router = renderApp(address, queryClient);
    await screen.findByRole("heading", { name: "Sign in" });
    expect(
      new URLSearchParams(router.state.location.searchStr).get("return_to"),
    ).toContain("/require-identity/t-1");
    expect(
      api.sent("GET", "/api/prohibitorum/require-identity/t-1"),
    ).toHaveLength(0);
  });
});
