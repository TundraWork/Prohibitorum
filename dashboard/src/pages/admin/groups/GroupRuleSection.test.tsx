import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AppAccessRule, AppGroupView } from "@/api/raw-admin-paths";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { GroupRuleSection } from "@/pages/admin/groups/GroupRuleSection";
import { apiError } from "@/test/app";
import { ruleGroupFixture } from "@/test/group";
import { providerFixture } from "@/test/identity-provider";

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

const catalogue = [
  { slug: "github", displayName: "GitHub" },
  { slug: "uni", displayName: "University SSO" },
];

type Handler = (request: Request) => Response | Promise<Response> | undefined;

function requests(method: string, suffix: string) {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter(
      (request) =>
        request.method === method &&
        new URL(request.url).pathname.endsWith(suffix),
    );
}
const puts = () => requests("PUT", "/groups/8");
const previews = () => requests("POST", "/groups/rule-preview");

function rejection(path: string, reason: string) {
  return Response.json(
    { code: "invalid_group_rule", requestId: "r", details: { path, reason } },
    { status: 422 },
  );
}

function mount(group: AppGroupView, handler?: Handler) {
  fetchBoundary.mockImplementation(async (request) => {
    const canned = handler?.(request);
    if (canned) return canned;
    const path = new URL(request.url).pathname;
    if (path.endsWith("/groups/providers")) return Response.json(catalogue);
    if (path.endsWith("/identity-providers")) {
      return Response.json({
        items: [
          providerFixture({
            slug: "uni",
            displayName: "University SSO",
            allowSignIn: false,
          }),
        ],
        nextCursor: "",
      });
    }
    if (path.endsWith("/groups/rule-preview")) {
      return Response.json({ matchedCount: 3, items: [], nextCursor: "" });
    }
    if (request.method === "PUT") return Response.json(group);
    return new Response(null, { status: 204 });
  });
  return render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <GroupRuleSection group={group} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

const passkey = { fact: "login_method", method: "passkey" };
const github = { fact: "connection.provider", provider: "github" };

function ruleGroup(condition: AppAccessRule["condition"]) {
  return ruleGroupFixture({ rule: { version: 1, condition } });
}

const addCondition = () =>
  screen
    .getAllByRole("button", { name: "Add condition" })
    .at(-1) as HTMLElement;
const saveButton = () => screen.getByRole("button", { name: "Save rule" });

describe("the rule section", () => {
  it("is titled by what it decides, with a builder and a JSON view", () => {
    mount(ruleGroupFixture());

    expect(
      screen.getByRole("heading", { level: 2, name: "Who this group matches" }),
    ).toBeVisible();
    expect(
      screen.getByRole("tab", { name: "Builder", selected: true }),
    ).toBeVisible();
    expect(screen.getByRole("tab", { name: "JSON" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "Save rule" })).toBeNull();
    expect(previews()).toHaveLength(0);
  });

  it("shows the JSON view of the stored rule", async () => {
    const user = userEvent.setup();
    mount(ruleGroupFixture());

    await user.click(screen.getByRole("tab", { name: "JSON" }));

    const box = screen.getByRole("textbox", { name: "Rule document" });
    expect(JSON.parse((box as HTMLTextAreaElement).value)).toEqual({
      version: 1,
      condition: { op: "all", children: [passkey] },
    });
  });

  it("names a provider with sign-in off beside it", async () => {
    const user = userEvent.setup();
    mount(ruleGroup({ op: "all", children: [github] }));

    await user.click(await screen.findByRole("combobox", { name: "Value" }));

    expect(
      (await screen.findAllByRole("option")).map(
        (option) => option.textContent,
      ),
    ).toEqual(["GitHub", "University SSO · sign-in off"]);
  });

  it("states the consequence and saves with the stored details and the draft", async () => {
    const user = userEvent.setup();
    mount(ruleGroupFixture({ description: "Who uses one" }));

    await user.click(addCondition());

    expect(
      screen.getByText("Saving changes who is in this group right away."),
    ).toBeVisible();
    await user.click(saveButton());

    await waitFor(() => expect(puts()).toHaveLength(1));
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
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("is clean again once the server answers with the rule it stored", async () => {
    const user = userEvent.setup();
    const stored = ruleGroup({ op: "all", children: [passkey, passkey] });
    mount(ruleGroupFixture(), (request) =>
      request.method === "PUT" ? Response.json(stored) : undefined,
    );

    await user.click(addCondition());
    await user.click(saveButton());

    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Save rule" })).toBeNull(),
    );
  });

  it("puts the stored rule back with Discard", async () => {
    const user = userEvent.setup();
    mount(ruleGroupFixture());

    await user.click(addCondition());
    await user.click(screen.getByRole("button", { name: "Discard" }));

    expect(screen.queryByRole("button", { name: "Save rule" })).toBeNull();
    expect(
      screen.getAllByRole("group", { name: /Signs in with/ }),
    ).toHaveLength(1);
    expect(puts()).toHaveLength(0);
  });

  it("reports a refused save once, under the builder", async () => {
    const user = userEvent.setup();
    mount(ruleGroupFixture(), (request) =>
      request.method === "PUT" ? apiError("internal_error", 500) : undefined,
    );

    await user.click(addCondition());
    await user.click(saveButton());

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("marks the row a refused save names instead of raising an alert", async () => {
    const user = userEvent.setup();
    mount(ruleGroup({ op: "all", children: [passkey] }), (request) =>
      request.method === "PUT"
        ? rejection("$.condition.children[1]", "provider_not_found")
        : undefined,
    );

    await user.click(addCondition());
    // The preview answers first, so a late answer cannot clear the refusal.
    await screen.findByText("3 accounts match the edited rule");
    await user.click(saveButton());

    await waitFor(() => expect(puts()).toHaveLength(1));
    const row = screen.getAllByRole("group", { name: /Signs in with/ })[1];
    expect(
      await within(row as HTMLElement).findByText(
        "That provider is not available on this instance.",
      ),
    ).toBeVisible();
    // One problem is stated in the footer too, so it is the reason Save is held.
    expect(saveButton()).toHaveAccessibleDescription(
      /That provider is not available on this instance\./,
    );
    expect(saveButton()).toBeDisabled();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("the live preview", () => {
  it("counts who the edited rule matches, once the edits stop", async () => {
    const user = userEvent.setup();
    mount(ruleGroupFixture());

    await user.click(addCondition());
    await user.click(addCondition());

    expect(
      await screen.findByText("3 accounts match the edited rule"),
    ).toBeVisible();
    expect(previews()).toHaveLength(1);
    const body = (await previews()[0]?.json()) as {
      version: number;
      condition: { children: unknown[] };
    };
    expect(body.version).toBe(1);
    expect(body.condition.children).toHaveLength(3);
  });

  it("says one account in the singular", async () => {
    const user = userEvent.setup();
    mount(ruleGroupFixture(), (request) =>
      new URL(request.url).pathname.endsWith("/rule-preview")
        ? Response.json({ matchedCount: 1, items: [], nextCursor: "" })
        : undefined,
    );

    await user.click(addCondition());

    expect(
      await screen.findByText("1 account matches the edited rule"),
    ).toBeVisible();
  });

  it("lets only the newest answer count", async () => {
    const user = userEvent.setup();
    const answers: ((response: Response) => void)[] = [];
    mount(ruleGroupFixture(), (request) => {
      if (!new URL(request.url).pathname.endsWith("/rule-preview"))
        return undefined;
      return new Promise<Response>((resolve) => answers.push(resolve));
    });

    await user.click(addCondition());
    await waitFor(() => expect(answers).toHaveLength(1));
    await user.click(addCondition());
    await waitFor(() => expect(answers).toHaveLength(2));

    answers[1]?.(Response.json({ matchedCount: 7, items: [], nextCursor: "" }));
    expect(
      await screen.findByText("7 accounts match the edited rule"),
    ).toBeVisible();

    // The older request answers last, and is ignored.
    answers[0]?.(Response.json({ matchedCount: 3, items: [], nextCursor: "" }));
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(screen.getByText("7 accounts match the edited rule")).toBeVisible();
    expect(screen.queryByText("3 accounts match the edited rule")).toBeNull();
  });

  it("marks the row the server names and holds Save back with the reason", async () => {
    const user = userEvent.setup();
    mount(ruleGroup({ op: "all", children: [passkey, github] }), (request) =>
      new URL(request.url).pathname.endsWith("/rule-preview")
        ? rejection("$.condition.children[1]", "provider_not_found")
        : undefined,
    );

    await user.click(addCondition());

    const row = await screen.findByRole("group", { name: /Connected to/ });
    expect(
      await within(row).findByText(
        "That provider is not available on this instance.",
      ),
    ).toBeVisible();
    expect(within(row).getByRole("combobox", { name: "Value" })).toBeInvalid();
    expect(saveButton()).toBeDisabled();
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("says once what went wrong when the preview itself failed, and still allows saving", async () => {
    const user = userEvent.setup();
    mount(ruleGroupFixture(), (request) =>
      new URL(request.url).pathname.endsWith("/rule-preview")
        ? apiError("rate_limited", 429)
        : undefined,
    );

    await user.click(addCondition());

    expect(
      await screen.findAllByText("Too many requests. Wait a moment."),
    ).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
    expect(saveButton()).toBeEnabled();
  });

  it("keeps one status line mounted, so a change in it is announced", async () => {
    const user = userEvent.setup();
    mount(ruleGroupFixture());

    await user.click(addCondition());
    const status = screen.getByRole("status");
    expect(status).toBeEmptyDOMElement();

    await screen.findByText("3 accounts match the edited rule");
    expect(screen.getByRole("status")).toBe(status);
    expect(status).toHaveTextContent("3 accounts match the edited rule");
  });

  it("drops a refusal once the draft changes, so it cannot point at a row that is gone", async () => {
    const user = userEvent.setup();
    mount(ruleGroup({ op: "all", children: [passkey] }), (request) =>
      request.method === "PUT"
        ? rejection("$.condition.children[1]", "provider_not_found")
        : undefined,
    );

    await user.click(addCondition());
    await screen.findByText("3 accounts match the edited rule");
    await user.click(saveButton());
    expect(
      await screen.findAllByText(
        "That provider is not available on this instance.",
      ),
    ).toHaveLength(2);

    await user.click(
      screen.getAllByRole("button", {
        name: /Remove the condition/,
      })[1] as HTMLElement,
    );

    expect(
      screen.queryByText("That provider is not available on this instance."),
    ).toBeNull();
  });

  it("lets a late preview answer leave a refusal of the same draft standing", async () => {
    const user = userEvent.setup();
    const previews: ((response: Response) => void)[] = [];
    mount(ruleGroup({ op: "all", children: [passkey] }), (request) => {
      if (request.method === "PUT") {
        return rejection("$.condition.children[1]", "provider_not_found");
      }
      if (new URL(request.url).pathname.endsWith("/rule-preview")) {
        return new Promise<Response>((resolve) => previews.push(resolve));
      }
      return undefined;
    });

    await user.click(addCondition());
    await waitFor(() => expect(previews).toHaveLength(1));
    await user.click(saveButton());
    await screen.findAllByText(
      "That provider is not available on this instance.",
    );

    previews[0]?.(
      Response.json({ matchedCount: 3, items: [], nextCursor: "" }),
    );
    await new Promise((resolve) => setTimeout(resolve, 50));

    expect(
      screen.getAllByText("That provider is not available on this instance."),
    ).toHaveLength(2);
    expect(saveButton()).toBeDisabled();
  });
});

describe("a rule the builder knows is invalid", () => {
  it("holds Save back with the reason beside it, and sends nothing to preview", async () => {
    const user = userEvent.setup();
    mount(ruleGroupFixture());

    await user.click(addCondition());
    const added = screen.getAllByRole("group", {
      name: /Signs in with/,
    })[1] as HTMLElement;
    await user.click(
      within(added).getByRole("combobox", { name: "Condition" }),
    );
    await user.click(
      await screen.findByRole("option", { name: "Connected to" }),
    );

    expect(
      screen.getByText("Choose a provider for the highlighted condition."),
    ).toBeVisible();
    expect(saveButton()).toBeDisabled();
    expect(saveButton()).toHaveAccessibleDescription(
      "Choose a provider for the highlighted condition.",
    );
    const value = within(
      screen.getByRole("group", { name: /Connected to/ }),
    ).getByRole("combobox", { name: "Value" });
    expect(value).toBeInvalid();
    await new Promise((resolve) => setTimeout(resolve, 500));
    expect(previews()).toHaveLength(0);
    expect(puts()).toHaveLength(0);
  });

  it("cannot save a rule with no conditions, and says what to do", async () => {
    const user = userEvent.setup();
    mount(ruleGroupFixture());

    await user.click(
      screen.getByRole("button", {
        name: "Remove the condition: Signs in with is A passkey",
      }),
    );

    expect(saveButton()).toBeDisabled();
    expect(saveButton()).toHaveAccessibleDescription(
      "Add a condition to say who this group matches.",
    );
    expect(
      screen.getAllByText("Add a condition to say who this group matches."),
    ).not.toHaveLength(0);
  });

  it("marks a saved provider the catalogue no longer holds as unknown", async () => {
    mount(
      ruleGroup({
        op: "all",
        children: [{ fact: "connection.provider", provider: "gone" }],
      }),
    );

    const value = await screen.findByRole("combobox", { name: "Value" });
    await waitFor(() =>
      expect(value).toHaveTextContent("Unknown provider (gone)"),
    );
    expect(value).toBeInvalid();
  });
});

describe("the JSON view", () => {
  it("feeds the builder: a document applied there is the draft that gets saved", async () => {
    const user = userEvent.setup();
    mount(ruleGroupFixture());

    await user.click(screen.getByRole("tab", { name: "JSON" }));
    const box = screen.getByRole("textbox", { name: "Rule document" });
    await user.clear(box);
    await user.click(box);
    await user.paste(
      JSON.stringify({
        version: 1,
        condition: { op: "any", children: [passkey, passkey] },
      }),
    );
    await user.tab();
    await user.click(await screen.findByRole("button", { name: "Save rule" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    const body = (await puts()[0]?.json()) as { rule: AppAccessRule };
    expect(body.rule.condition.op).toBe("any");
  });
});

describe("a JSON document that was refused", () => {
  it("counts as an unsaved edit that cannot be saved, with the reason in the footer", async () => {
    const user = userEvent.setup();
    mount(ruleGroupFixture());
    expect(screen.queryByRole("button", { name: "Save rule" })).toBeNull();

    await user.click(screen.getByRole("tab", { name: "JSON" }));
    const box = screen.getByRole("textbox", { name: "Rule document" });
    await user.clear(box);
    await user.click(box);
    await user.paste("{");
    await user.tab();

    expect(saveButton()).toBeDisabled();
    expect(saveButton()).toHaveAccessibleDescription(
      /This is not valid JSON\. The builder still holds your rule\./,
    );
    expect(
      screen
        .getByRole("heading", { level: 2, name: /Who this group matches/ })
        .closest("section"),
    ).toHaveAttribute("data-dirty", "true");
    expect(previews()).toHaveLength(0);
    expect(puts()).toHaveLength(0);

    await user.click(screen.getByRole("button", { name: "Discard" }));
    expect(screen.queryByRole("button", { name: "Save rule" })).toBeNull();
    expect(
      JSON.parse(
        (
          screen.getByRole("textbox", {
            name: "Rule document",
          }) as HTMLTextAreaElement
        ).value,
      ),
    ).toEqual({ version: 1, condition: { op: "all", children: [passkey] } });
  });
});

describe("the footer's reason", () => {
  it("states the one problem in its own words", async () => {
    const user = userEvent.setup();
    mount(ruleGroup({ op: "all", children: [passkey] }), (request) =>
      request.method === "PUT"
        ? rejection("$.condition.children[1]", "provider_not_found")
        : undefined,
    );
    await user.click(addCondition());
    await screen.findByText("3 accounts match the edited rule");
    await user.click(saveButton());

    await waitFor(() =>
      expect(saveButton()).toHaveAccessibleDescription(
        /That provider is not available on this instance\./,
      ),
    );
  });
});
