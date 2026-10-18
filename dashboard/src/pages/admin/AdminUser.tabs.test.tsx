import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
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

it("switches the account tab by replacing the history entry", async () => {
  const base = "/api/prohibitorum";
  fakeApi({
    ...publicApi({ ...testSession, role: "admin" }),
    [`GET ${base}/accounts/2`]: () =>
      Response.json({
        id: 2,
        username: "bob",
        displayName: "Bob",
        oidcSubject: "00000000-0000-4000-8000-000000000002",
        role: "user",
        disabled: false,
        emailVerified: false,
        matchingIdentities: [],
        createdAt: "2026-09-01T00:00:00Z",
        updatedAt: "2026-09-01T00:00:00Z",
      }),
  });
  const router = renderApp("/admin/users/2?tab=profile", queryClient);
  const user = userEvent.setup();
  const tab = await screen.findByRole("tab", { name: "Access" });
  const navigate = vi.spyOn(router, "navigate");
  const entries = router.history.length;

  await user.click(tab);

  await waitFor(() =>
    expect(router.state.location.search).toMatchObject({ tab: "access" }),
  );
  expect(navigate).toHaveBeenCalledWith(
    expect.objectContaining({ search: { tab: "access" }, replace: true }),
  );
  expect(router.history.length).toBe(entries);
});
