import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ManualDecisionView } from "@/api/raw-admin-paths";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { GroupDecisionsFrame } from "@/pages/admin/groups/GroupDecisionsFrame";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(vi.fn());
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

const decision = (id: number, effect: string): ManualDecisionView => ({
  account: { id, username: `user${id}`, displayName: `User ${id}` },
  effect,
  updatedAt: new Date().toISOString(),
});

function mount(reply: () => Response) {
  fetchBoundary.mockImplementation(async () => reply());
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <GroupDecisionsFrame groupId={7} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("the decision counts", () => {
  it("count the allows and the denies of every page loaded", async () => {
    mount(() =>
      Response.json({
        items: [
          decision(1, "allow"),
          decision(2, "allow"),
          decision(3, "deny"),
        ],
        nextCursor: "",
      }),
    );

    expect(await screen.findByText("2 allowed · 1 denied")).toBeVisible();
  });

  it("are not drawn while pages remain, since a partial count would read as the whole", async () => {
    mount(() =>
      Response.json({ items: [decision(1, "allow")], nextCursor: "c1" }),
    );

    await waitFor(() => expect(fetchBoundary).toHaveBeenCalled());
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(screen.queryByRole("heading")).not.toBeInTheDocument();
    expect(screen.queryByText(/allowed/)).not.toBeInTheDocument();
  });

  it("are not drawn with no decisions", async () => {
    mount(() => Response.json({ items: [], nextCursor: "" }));

    await waitFor(() => expect(fetchBoundary).toHaveBeenCalled());
    await waitFor(() =>
      expect(screen.queryByRole("heading")).not.toBeInTheDocument(),
    );
  });

  it("are not drawn when the list cannot be read", async () => {
    mount(() => Response.json({ code: "internal_error" }, { status: 500 }));

    await waitFor(() => expect(fetchBoundary).toHaveBeenCalled());
    expect(screen.queryByRole("heading")).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
