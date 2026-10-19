import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AppGroupView } from "@/api/raw-admin-paths";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { LegacyRuleSection } from "@/pages/admin/groups/RuleEditorLegacy";
import { apiError } from "@/test/app";
import { ruleGroupFixture } from "@/test/group";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifyError.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(
    notifyError,
    vi.fn<(message: MessageDescriptor) => void>(),
  );
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

function puts() {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter((request) => request.method === "PUT");
}

function mount(group: AppGroupView, refuse = false) {
  fetchBoundary.mockImplementation(async (request) => {
    const path = new URL(request.url).pathname;
    if (path.endsWith("/groups/providers")) return Response.json([]);
    if (path.endsWith("/groups/rule-preview")) {
      return Response.json({ matchedCount: 3, items: [], nextCursor: "" });
    }
    if (request.method === "PUT") {
      return refuse ? apiError("internal_error", 500) : Response.json(group);
    }
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <LegacyRuleSection group={group} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("the rule section", () => {
  it("offers no save until the rule changes, and previews the stored rule", async () => {
    mount(ruleGroupFixture());

    expect(
      screen.getByRole("heading", { level: 2, name: "Rule" }),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: "Save rule" })).toBeNull();
    expect(await screen.findByText("3 accounts match this rule")).toBeVisible();
  });

  it("saves the draft with the stored name, identifier and description", async () => {
    const user = userEvent.setup();
    mount(ruleGroupFixture({ description: "Who uses one" }));

    await user.click(screen.getByRole("button", { name: "Add condition" }));
    await user.click(screen.getByRole("button", { name: "Save rule" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(new URL(puts()[0]?.url ?? "").pathname).toBe(
      "/api/prohibitorum/groups/8",
    );
    const body = (await puts()[0]?.json()) as {
      rule: { condition: { children: unknown[] } };
    };
    expect(body).toMatchObject({
      slug: "passkey-users",
      displayName: "Passkey users",
      description: "Who uses one",
      exposedToDownstream: true,
    });
    expect(body.rule.condition.children).toHaveLength(2);
  });

  it("puts the stored rule back with Discard", async () => {
    const user = userEvent.setup();
    mount(ruleGroupFixture());

    await user.click(screen.getByRole("button", { name: "Add condition" }));
    await user.click(screen.getByRole("button", { name: "Discard" }));

    expect(screen.queryByRole("button", { name: "Save rule" })).toBeNull();
    expect(puts()).toHaveLength(0);
  });

  it("reports a refused save once, under the editor", async () => {
    const user = userEvent.setup();
    mount(ruleGroupFixture(), true);

    await user.click(screen.getByRole("button", { name: "Add condition" }));
    await user.click(screen.getByRole("button", { name: "Save rule" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});
