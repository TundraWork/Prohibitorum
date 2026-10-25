import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { loadDocument } from "@/app/load-document";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { fakeApi, publicApi, renderApp, testSession } from "@/test/app";

let queryClient: QueryClient;

vi.mock("@/app/load-document", () => ({ loadDocument: vi.fn() }));

beforeEach(() => {
  i18n.activate("en");
  queryClient = createQueryClient(() => undefined);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
  vi.mocked(loadDocument).mockReset();
});
async function open(path: string, signedIn = true) {
  fakeApi(publicApi(signedIn ? testSession : null));
  const router = renderApp(path, queryClient);
  await screen.findByRole("heading", { level: 1 });
  return router;
}

describe("the error landing page", () => {
  it("names the application an account may not use", async () => {
    await open("/error?reason=app_access_denied&app=Wiki");
    expect(
      screen.getByRole("heading", { name: "You don't have access to Wiki" }),
    ).toHaveFocus();
    expect(
      screen.getByText(
        "An administrator has not granted your account access to this app.",
      ),
    ).toBeVisible();
  });

  it("draws the application strip only when the link names an app", async () => {
    await open("/error?reason=app_access_denied&app=Wiki");
    const strip = document.querySelector('[data-slot="stage-strip"]');
    expect(strip).toHaveTextContent("You're signing in to");
    expect(strip).toHaveTextContent("Wiki");
  });

  it("draws no strip for an ordinary failure even when the link names an app", async () => {
    await open("/error?error=server_error&app=Wiki");
    const strip = document.querySelector('[data-slot="stage-strip"]');
    expect(strip).not.toHaveTextContent("You're signing in to");
    expect(strip).not.toHaveTextContent("Wiki");
  });

  it("draws no strip for a failure that names no app", async () => {
    await open("/error?error=server_error");
    expect(screen.queryByText("You're signing in to")).not.toBeInTheDocument();
  });

  it("describes a code only a redirect carries, with the instance's name", async () => {
    await open("/error?error=saml_sp_unknown");
    expect(
      screen.getByRole("heading", { name: "Unable to continue" }),
    ).toBeVisible();
    expect(
      screen.getByText("This app is not registered with Test instance."),
    ).toBeVisible();
  });

  it("names the provider only when the link does", async () => {
    await open("/error?error=upstream_error&federationName=GitLab");
    expect(screen.getByText("GitLab refused this sign-in.")).toBeVisible();
  });

  it.each([
    "/error?error=upstream_error",
    "/error?error=upstream_error&federationName=",
    "/error?error=made_up_code",
    "/error",
  ])("falls back to a plain sentence for %s", async (path) => {
    await open(path);
    expect(screen.getByText("An error occurred during sign-in.")).toBeVisible();
  });

  it("shows a reference an administrator can look up", async () => {
    await open("/error?error=server_error&ref=3fa9c1d2");
    expect(screen.getByText("3fa9c1d2")).toHaveClass("font-mono");
    expect(screen.getByText(/^Reference/)).toBeVisible();
  });

  it.each(["12345678", "12e45678"])(
    "shows the reference %s as written, however it reads",
    async (ref) => {
      const router = await open(`/error?error=upstream_error&ref=${ref}`);
      expect(screen.getByText(ref)).toHaveClass("font-mono");
      expect(router.state.location.href).toBe(
        `/error?error=upstream_error&ref=${ref}`,
      );
    },
  );

  it("goes back to a page on this site", async () => {
    const user = userEvent.setup();
    const router = await open(
      `/error?error=server_error&return_to=${encodeURIComponent("/security")}`,
    );
    await user.click(screen.getByRole("button", { name: "Go back" }));
    await vi.waitFor(() =>
      expect(router.state.location.pathname).toBe("/security"),
    );
  });

  it("keeps offering to go back rather than retry an identity link", async () => {
    const user = userEvent.setup();
    const router = await open(
      `/error?error=federation_state_invalid&return_to=${encodeURIComponent("/security")}`,
    );
    await user.click(screen.getByRole("button", { name: "Go back" }));
    await vi.waitFor(() =>
      expect(router.state.location.pathname).toBe("/security"),
    );
  });

  it("starts the authorization again after a server error", async () => {
    const user = userEvent.setup();
    await open(
      `/error?error=server_error&return_to=${encodeURIComponent("/oauth/authorize?client_id=wiki")}`,
    );
    await user.click(screen.getByRole("button", { name: "Try again" }));
    await vi.waitFor(() =>
      expect(loadDocument).toHaveBeenCalledWith(
        "/oauth/authorize?client_id=wiki",
      ),
    );
  });

  it("does not turn a smuggled bypass inside an authorize address into a retry", async () => {
    await open(
      `/error?error=server_error&return_to=${encodeURIComponent("/oauth/authorize%2509//evil.example")}`,
    );
    expect(
      screen.queryByRole("button", { name: "Try again" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Go back" }),
    ).not.toBeInTheDocument();
  });

  it("keeps the retry for an authorize address whose state carries data characters", async () => {
    const user = userEvent.setup();
    await open(
      `/error?error=server_error&return_to=${encodeURIComponent("/oauth/authorize?client_id=wiki&state=100%25")}`,
    );
    await user.click(screen.getByRole("button", { name: "Try again" }));
    await vi.waitFor(() =>
      expect(loadDocument).toHaveBeenCalledWith(
        "/oauth/authorize?client_id=wiki&state=100%25",
      ),
    );
  });

  it("does not offer a retry when the code is not a server error", async () => {
    await open(
      `/error?error=federation_state_invalid&return_to=${encodeURIComponent("/oauth/authorize?client_id=wiki")}`,
    );
    expect(
      screen.queryByRole("button", { name: "Try again" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Go back" })).toBeVisible();
  });

  it("offers a denied user a retry once access may have been granted", async () => {
    const user = userEvent.setup();
    await open(
      `/error?reason=app_access_denied&app=Wiki&return_to=${encodeURIComponent("/oauth/authorize?client_id=wiki")}`,
    );
    expect(
      screen.getByText(
        "An administrator hasn't granted you access yet. Once access is granted, you can try again.",
      ),
    ).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Try again" }));
    await vi.waitFor(() =>
      expect(loadDocument).toHaveBeenCalledWith(
        "/oauth/authorize?client_id=wiki",
      ),
    );
  });

  it("re-runs the require-identity entry a denial carried", async () => {
    const user = userEvent.setup();
    await open(
      `/error?reason=app_access_denied&app=Wiki&return_to=${encodeURIComponent("/api/prohibitorum/require-identity?client_id=wiki")}`,
    );
    await user.click(screen.getByRole("button", { name: "Try again" }));
    await vi.waitFor(() =>
      expect(loadDocument).toHaveBeenCalledWith(
        "/api/prohibitorum/require-identity?client_id=wiki",
      ),
    );
  });

  it.each([
    "//evil.example",
    "/\\evil.example",
    "https://evil.example",
    "/oauth/authorize%2509//evil.example",
  ])("does not turn a denial with %s into a retry", async (target) => {
    await open(
      `/error?reason=app_access_denied&app=Wiki&return_to=${encodeURIComponent(target)}`,
    );
    expect(
      screen.queryByRole("button", { name: "Try again" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Go back" }),
    ).not.toBeInTheDocument();
  });

  it("does not offer a denied user a retry for another page on this site", async () => {
    await open(
      `/error?reason=app_access_denied&app=Wiki&return_to=${encodeURIComponent("/security")}`,
    );
    expect(
      screen.queryByRole("button", { name: "Try again" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Go back" })).toBeVisible();
  });

  it("does not turn a server error into a require-identity retry", async () => {
    await open(
      `/error?error=server_error&return_to=${encodeURIComponent("/api/prohibitorum/require-identity?client_id=wiki")}`,
    );
    expect(
      screen.queryByRole("button", { name: "Try again" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Go back" })).toBeVisible();
  });

  it.each(["//evil.example", "/\\evil.example", "https://evil.example"])(
    "does not treat %s as a page on this site",
    async (target) => {
      await open(
        `/error?error=server_error&return_to=${encodeURIComponent(target)}`,
      );
      expect(
        screen.queryByRole("button", { name: "Go back" }),
      ).not.toBeInTheDocument();
      expect(
        screen.getByRole("button", { name: "Go to my account" }),
      ).toBeVisible();
    },
  );

  it("offers the account to someone signed in and sign-in to anyone else", async () => {
    const user = userEvent.setup();
    const signedIn = await open("/error?error=server_error");
    await user.click(screen.getByRole("button", { name: "Go to my account" }));
    await vi.waitFor(() => expect(signedIn.state.location.pathname).toBe("/"));
  });

  it("offers sign-in without a session", async () => {
    const user = userEvent.setup();
    const router = await open("/error?error=server_error", false);
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(
      await screen.findByRole("heading", { name: /^Sign in to / }),
    ).toBeVisible();
    expect(router.state.location.pathname).toBe("/login");
  });

  it("names the application, icon and all, when the link it started from does", async () => {
    const api = fakeApi({
      ...publicApi(testSession),
      "GET /api/prohibitorum/forward-auth/login-context": () =>
        Response.json({
          application: {
            label: "Wiki",
            host: "wiki.example.test",
            iconUrl: "/icon/oidc_client/wiki?v=abcd1234",
          },
        }),
    });
    renderApp(
      `/error?error=federation_state_invalid&return_to=${encodeURIComponent("/oauth/authorize?client_id=wiki")}`,
      queryClient,
    );
    await screen.findByRole("heading", { level: 1 });
    const strip = await waitFor(() => {
      const element = document.querySelector('[data-slot="stage-strip"]');
      expect(element).toHaveTextContent("Wiki");
      return element as HTMLElement;
    });
    expect(strip).toHaveTextContent("wiki.example.test");
    const [read] = api.sent(
      "GET",
      "/api/prohibitorum/forward-auth/login-context",
    );
    expect(read?.url.searchParams.get("return_to")).toBe(
      `${window.location.origin}/oauth/authorize?client_id=wiki`,
    );
  });

  it("names a denied application by its registered name over the one in the link", async () => {
    fakeApi({
      ...publicApi(testSession),
      "GET /api/prohibitorum/forward-auth/login-context": () =>
        Response.json({ application: { label: "Wiki" } }),
    });
    renderApp(
      `/error?error=access_denied&reason=app_access_denied&app=Spoofed&return_to=${encodeURIComponent("/oauth/authorize?client_id=wiki")}`,
      queryClient,
    );
    expect(
      await screen.findByRole("heading", {
        name: "You don't have access to Wiki",
      }),
    ).toBeVisible();
    expect(screen.queryByText(/Spoofed/)).not.toBeInTheDocument();
  });

  it("says a sign-in that took too long has to start again from the app", async () => {
    await open("/error?error=saml_request_expired&ref=abcd1234");
    expect(
      screen.getByText(
        "The app's sign-in request has expired. Go back to the app and sign in again.",
      ),
    ).toBeVisible();
  });
});
