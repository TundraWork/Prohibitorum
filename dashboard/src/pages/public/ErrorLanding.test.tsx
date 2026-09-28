import type { QueryClient } from "@tanstack/react-query";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { fakeApi, publicApi, renderApp, testSession } from "@/test/app";

let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  queryClient = createQueryClient(() => undefined);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
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
        "An administrator hasn't given your account access to this app.",
      ),
    ).toBeVisible();
  });

  it("describes a code only a redirect carries, with the instance's name", async () => {
    await open("/error?error=saml_sp_unknown");
    expect(
      screen.getByRole("heading", { name: "Unable to continue" }),
    ).toBeVisible();
    expect(
      screen.getByText("This app isn't registered with Test instance."),
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
    expect(
      screen.getByText("Something went wrong while signing in."),
    ).toBeVisible();
  });

  it("shows a reference an administrator can look up", async () => {
    await open("/error?error=server_error&ref=3fa9c1d2");
    expect(screen.getByText("3fa9c1d2")).toHaveClass("font-mono");
    expect(screen.getByText(/^Reference/)).toBeVisible();
  });

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
      await screen.findByRole("heading", { name: "Sign in" }),
    ).toBeVisible();
    expect(router.state.location.pathname).toBe("/login");
  });
});
