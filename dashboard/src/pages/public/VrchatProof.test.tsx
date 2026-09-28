import { screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { fakeApi, publicApi, renderApp } from "@/test/app";

const proof = "proof-token-7c1e";
const queryClient = createQueryClient(() => undefined);

beforeEach(() => {
  i18n.activate("en");
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

describe("the verification link's page", () => {
  it("explains the link without reading, showing or sending its proof", async () => {
    const api = fakeApi(publicApi(null));
    const fetch = vi.mocked(globalThis.fetch);
    renderApp(`/verify/vrchat/${proof}`, queryClient);
    expect(
      await screen.findByRole("heading", {
        name: "This is a verification link",
      }),
    ).toHaveFocus();
    expect(
      screen.getByText(/to prove to Test instance that the profile is theirs/),
    ).toBeVisible();
    expect(
      screen.getByText(/If this is your profile, go back to Test instance/),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: /verify/i })).toBeNull();
    expect(document.body.innerHTML).not.toContain(proof);
    for (const [request] of fetch.mock.calls) {
      const sent = request as Request;
      expect(sent.url).not.toContain(proof);
      expect(await sent.clone().text()).not.toContain(proof);
    }
    expect(api.sent("GET", "/api/prohibitorum/config")).toHaveLength(1);
  });
});
