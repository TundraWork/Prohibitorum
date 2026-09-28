import type { QueryClient } from "@tanstack/react-query";
import { screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { fakeApi, publicApi, renderApp } from "@/test/app";

let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  queryClient = createQueryClient(() => undefined);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

it("sends an anonymous reader to sign in and back to the console page they opened", async () => {
  fakeApi(publicApi(null));
  const router = renderApp("/security?tab=x", queryClient);
  expect(await screen.findByRole("heading", { name: "Sign in" })).toBeVisible();
  expect(router.state.location.pathname).toBe("/login");
  expect(router.state.location.searchStr).toBe(
    "?return_to=%2Fsecurity%3Ftab%3Dx",
  );
});
