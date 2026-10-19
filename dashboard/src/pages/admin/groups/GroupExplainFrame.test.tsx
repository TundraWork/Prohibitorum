import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ExplanationView } from "@/api/raw-admin-paths";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { GroupExplainFrame } from "@/pages/admin/groups/GroupExplainFrame";
import { apiError } from "@/test/app";

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

const accounts = [
  { id: 4, username: "ana", displayName: "Ana Lima", disabled: false },
  { id: 5, username: "ben", displayName: "Ben Ito", disabled: false },
  { id: 6, username: "old", displayName: "Old Hand", disabled: true },
];

const leaf = (
  path: string,
  label: string,
  result: boolean,
): ExplanationView => ({ path, label, result });

/** All of: a passkey, and any of (GitHub, OpenID Connect); the account has the passkey only. */
const outTree: ExplanationView = {
  path: "$",
  label: "all",
  result: false,
  children: [
    leaf("$.children[0]", "login_method=passkey", true),
    {
      path: "$.children[1]",
      label: "any",
      result: false,
      children: [
        leaf("$.children[1].children[0]", "connection.provider=github", false),
        leaf("$.children[1].children[1]", "connection.protocol=oidc", false),
      ],
    },
  ],
};

const inTree: ExplanationView = {
  path: "$",
  label: "all",
  result: true,
  children: [
    leaf("$.children[0]", "login_method=passkey", true),
    {
      path: "$.children[1]",
      label: "not",
      result: true,
      children: [leaf("$.children[1].child", "avatar=user_uploaded", false)],
    },
  ],
};

function mount(
  explain: (accountId: number) => Response | Promise<Response> = () =>
    Response.json({ account: accounts[0], explanation: outTree }),
) {
  fetchBoundary.mockImplementation(async (request) => {
    const path = new URL(request.url).pathname;
    if (path.endsWith("/accounts")) return Response.json({ items: accounts });
    if (path.endsWith("/groups/providers")) {
      return Response.json([{ slug: "github", displayName: "GitHub" }]);
    }
    const match = path.match(/\/groups\/7\/explain\/(\d+)$/);
    if (match) return explain(Number(match[1]));
    return new Response(null, { status: 404 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <GroupExplainFrame groupId={7} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

async function choose(user: ReturnType<typeof userEvent.setup>, name: RegExp) {
  await user.click(await screen.findByRole("combobox"));
  await user.click(await screen.findByRole("option", { name }));
}

const explains = () =>
  fetchBoundary.mock.calls
    .map(([request]) => new URL(request.url).pathname)
    .filter((path) => /\/explain\//.test(path));

describe("the account check", () => {
  it("asks for nothing until an account is chosen", async () => {
    mount();

    expect(
      await screen.findByRole("heading", {
        level: 2,
        name: "Check an account",
      }),
    ).toBeVisible();
    expect(screen.queryByRole("list", { name: "Conditions" })).toBeNull();
    expect(explains()).toHaveLength(0);
  });

  it("says an account is not in the group and why, condition by condition", async () => {
    const user = userEvent.setup();
    mount();

    await choose(user, /Ana Lima/);

    expect(await screen.findByText("Not in this group")).toBeVisible();
    expect(explains()).toEqual(["/api/prohibitorum/groups/7/explain/4"]);
    const tree = screen.getByRole("list", { name: "Conditions" });
    expect(
      within(tree).getByText("Every condition below applies"),
    ).toBeVisible();
    expect(within(tree).getByText("Signs in with a passkey")).toBeVisible();
    expect(
      within(tree).getByText("At least one condition below applies"),
    ).toBeVisible();
    // A provider is named, not left as its slug.
    expect(within(tree).getByText("Is connected to GitHub")).toBeVisible();
    expect(
      within(tree).getByText("Is connected through OpenID Connect"),
    ).toBeVisible();
  });

  it("nests each condition under the one it belongs to and says which applied", async () => {
    const user = userEvent.setup();
    mount();

    await choose(user, /Ana Lima/);
    await screen.findByRole("list", { name: "Conditions" });

    const github = screen.getByText("Is connected to GitHub").closest("li");
    const passkey = screen.getByText("Signs in with a passkey").closest("li");
    expect(github).not.toBeNull();
    // Two lists deep under the root, one deeper than the passkey row.
    expect((github as HTMLElement).closest("ul")).not.toBe(
      (passkey as HTMLElement).closest("ul"),
    );
    expect(github).toHaveTextContent("Does not apply:");
    expect(passkey).toHaveTextContent("Applies:");
  });

  it("says an account is in the group", async () => {
    const user = userEvent.setup();
    mount(() => Response.json({ account: accounts[1], explanation: inTree }));

    await choose(user, /Ben Ito/);

    expect(await screen.findByText("In this group")).toBeVisible();
    expect(
      screen.getByText("The condition below does not apply"),
    ).toBeVisible();
    expect(
      screen.getByText("Uploaded their own profile picture"),
    ).toBeVisible();
  });

  it("shows a label it has no words for as it arrived", async () => {
    const user = userEvent.setup();
    mount(() =>
      Response.json({
        account: accounts[0],
        explanation: leaf("$", "geo.region=nl", true),
      }),
    );

    await choose(user, /Ana Lima/);

    const raw = await screen.findByText("geo.region=nl");
    expect(raw).toHaveClass("font-mono");
  });

  it("reads the next account when another is chosen", async () => {
    const user = userEvent.setup();
    mount((id) =>
      Response.json({
        account: accounts[id === 4 ? 0 : 1],
        explanation: id === 4 ? outTree : inTree,
      }),
    );

    await choose(user, /Ana Lima/);
    await screen.findByText("Not in this group");
    await choose(user, /Ben Ito/);

    expect(await screen.findByText("In this group")).toBeVisible();
    expect(explains()).toEqual([
      "/api/prohibitorum/groups/7/explain/4",
      "/api/prohibitorum/groups/7/explain/5",
    ]);
  });

  it("lists a disabled account with the reason and cannot choose it", async () => {
    const user = userEvent.setup();
    mount();

    await user.click(await screen.findByRole("combobox"));
    const option = await screen.findByRole("option", { name: /Old Hand/ });

    expect(option).toHaveAttribute("aria-disabled", "true");
    expect(option).toHaveTextContent("Disabled account");
  });

  it("tells a failed check once, in place, and no toast", async () => {
    const user = userEvent.setup();
    mount(() => apiError("account_not_found", 404));

    await choose(user, /Ana Lima/);

    const alert = await screen.findByRole("alert");
    expect(alert).toBeVisible();
    expect(screen.queryByText("In this group")).toBeNull();
    expect(screen.queryByText("Not in this group")).toBeNull();
    await waitFor(() => expect(notifyError).not.toHaveBeenCalled());
    expect(screen.getAllByRole("alert")).toHaveLength(1);
  });
});
