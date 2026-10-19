import type { MessageDescriptor } from "@lingui/core";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { useRuleProviders } from "@/pages/admin/groups/use-rule-providers";
import { apiError } from "@/test/app";
import { providerFixture } from "@/test/identity-provider";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
let queryClient: QueryClient;

beforeEach(() => {
  fetchBoundary.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(
    vi.fn(),
    vi.fn<(message: MessageDescriptor) => void>(),
  );
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
}

function answer(catalogue: () => Response | Promise<Response>) {
  fetchBoundary.mockImplementation(async (request) => {
    const path = new URL(request.url).pathname;
    if (path.endsWith("/groups/providers")) return catalogue();
    return Response.json({
      items: [
        providerFixture({
          slug: "uni",
          displayName: "Uni",
          allowSignIn: false,
        }),
        providerFixture({ slug: "old", displayName: "Old", disabled: true }),
      ],
      nextCursor: "",
    });
  });
}

const catalogue = [
  { slug: "uni", displayName: "University SSO" },
  { slug: "old", displayName: "Old SSO" },
  { slug: "new", displayName: "New SSO" },
];

describe("useRuleProviders", () => {
  it("is loading, with no known set, until the catalogue arrives", async () => {
    answer(() => new Promise<Response>(() => undefined));
    const { result } = renderHook(() => useRuleProviders(), { wrapper });

    expect(result.current.state).toBe("loading");
    expect(result.current.known).toBeUndefined();
    expect(result.current.providers).toEqual([]);
  });

  it("names each provider with the states the provider list holds for it", async () => {
    answer(() => Response.json(catalogue));
    const { result } = renderHook(() => useRuleProviders(), { wrapper });

    await waitFor(() => expect(result.current.state).toBe("loaded"));
    expect([...(result.current.known ?? [])]).toEqual(["uni", "old", "new"]);
    expect(result.current.providers).toEqual([
      {
        slug: "uni",
        displayName: "University SSO",
        disabled: false,
        signInOff: true,
      },
      { slug: "old", displayName: "Old SSO", disabled: true, signInOff: false },
      {
        slug: "new",
        displayName: "New SSO",
        disabled: false,
        signInOff: false,
      },
    ]);
  });

  it("says failed when the catalogue never arrived, and nothing is known", async () => {
    answer(() => apiError("internal_error", 500));
    const { result } = renderHook(() => useRuleProviders(), { wrapper });

    await waitFor(() => expect(result.current.state).toBe("failed"));
    expect(result.current.known).toBeUndefined();
  });

  it("trusts the list it already has when a refresh fails", async () => {
    let refuse = false;
    answer(() =>
      refuse ? apiError("internal_error", 500) : Response.json(catalogue),
    );
    const { result } = renderHook(() => useRuleProviders(), { wrapper });
    await waitFor(() => expect(result.current.state).toBe("loaded"));

    refuse = true;
    await queryClient.invalidateQueries({
      queryKey: ["admin", "groups", "providers"],
    });

    await waitFor(() =>
      expect(
        queryClient.getQueryState(["admin", "groups", "providers"])?.status,
      ).toBe("error"),
    );
    expect(result.current.state).toBe("loaded");
    expect(result.current.known?.has("new")).toBe(true);
  });
});
