import type { QueryClient } from "@tanstack/react-query";
import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import {
  apiError,
  fakeApi,
  publicApi,
  renderApp,
  testConfig,
  testSession,
} from "@/test/app";

const maintenance = {
  ...testConfig,
  maintenanceMode: true,
  maintenanceMessage: "Back at 18:00.",
};

let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  queryClient = createQueryClient(() => undefined);
});

afterEach(() => {
  vi.useRealTimers();
  queryClient.clear();
  vi.unstubAllGlobals();
});

describe("the maintenance page", () => {
  it("names the instance and shows the administrators' message", async () => {
    fakeApi(publicApi(testSession, maintenance));
    renderApp("/maintenance", queryClient);
    expect(
      await screen.findByRole("heading", {
        name: "Test instance is under maintenance",
      }),
    ).toHaveFocus();
    expect(screen.getByText("Back at 18:00.")).toBeVisible();
  });

  it("signs a member out and then offers the administrators' sign-in", async () => {
    const api = fakeApi({
      ...publicApi(testSession, maintenance),
      "POST /api/prohibitorum/auth/logout": () => {
        api.routes["GET /api/prohibitorum/me"] = () =>
          apiError("no_session", 401);
        return new Response(null, { status: 204 });
      },
    });
    const user = userEvent.setup();
    const router = renderApp("/maintenance", queryClient);
    await user.click(await screen.findByRole("button", { name: "Sign out" }));
    const adminSignIn = await screen.findByRole("button", {
      name: "Administrator sign-in",
    });
    expect(router.state.location.pathname).toBe("/maintenance");

    await user.click(adminSignIn);
    // The administrators' way in is let through while maintenance is on.
    expect(
      await screen.findByRole("heading", { name: "Sign in" }),
    ).toBeVisible();
    expect(router.state.location.pathname).toBe("/login");
    expect(router.state.location.searchStr).toBe("?admin=1");
    expect(
      screen.getByText(
        "The service is undergoing maintenance. Only administrators can sign in.",
      ),
    ).toBeVisible();
  });

  it("moves on once a recheck finds maintenance over", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const api = fakeApi(publicApi(null, maintenance));
    const router = renderApp("/maintenance", queryClient);
    await screen.findByRole("heading", {
      name: "Test instance is under maintenance",
    });
    api.routes["GET /api/prohibitorum/config"] = () =>
      Response.json(testConfig);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(180_000);
    });
    // Home is the console, which sends the anonymous reader on to sign in.
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
  });

  it("moves on when a retry finds maintenance over", async () => {
    const api = fakeApi(publicApi(null, maintenance));
    const user = userEvent.setup();
    const router = renderApp("/maintenance", queryClient);
    await screen.findByRole("heading", {
      name: "Test instance is under maintenance",
    });
    api.routes["GET /api/prohibitorum/config"] = () =>
      Response.json(testConfig);
    await user.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
  });

  it("is left for home when maintenance has already ended", async () => {
    fakeApi(publicApi(null));
    const router = renderApp("/maintenance", queryClient);
    expect(
      await screen.findByRole("heading", { name: "Sign in" }),
    ).toBeVisible();
    expect(router.state.location.pathname).toBe("/login");
  });
});
