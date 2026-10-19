import { I18nProvider } from "@lingui/react";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { beforeEach, describe, expect, it } from "vitest";
import type { AppAccessRule } from "@/api/raw-admin-paths";
import { i18n } from "@/i18n";
import {
  RuleBuilder,
  type RuleProvider,
} from "@/pages/admin/groups/RuleBuilder";
import {
  checkRule,
  parseRule,
  type RuleGroup,
  serializeRule,
} from "@/pages/admin/groups/rule-model";

const providers: RuleProvider[] = [
  { slug: "github", displayName: "GitHub" },
  { slug: "uni", displayName: "University SSO", signInOff: true },
  { slug: "old", displayName: "Old SSO", disabled: true },
  { slug: "both", displayName: "Both SSO", disabled: true, signInOff: true },
];

const passkey = { fact: "login_method", method: "passkey" };
const github = { fact: "connection.provider", provider: "github" };

let latest: AppAccessRule | undefined;

beforeEach(() => {
  i18n.activate("en");
  latest = undefined;
});

function Harness({ rule }: { rule: AppAccessRule | undefined }) {
  const [root, setRoot] = useState<RuleGroup>(() => parseRule(rule));
  latest = serializeRule(root);
  const issues = checkRule(root, new Set(providers.map((entry) => entry.slug)));
  return (
    <I18nProvider i18n={i18n}>
      <RuleBuilder
        value={root}
        onChange={(next) => {
          setRoot(next);
          latest = serializeRule(next);
        }}
        issues={issues}
        providers={providers}
      />
    </I18nProvider>
  );
}

function mount(rule: AppAccessRule | undefined) {
  return render(<Harness rule={rule} />);
}

function doc(condition: AppAccessRule["condition"]): AppAccessRule {
  return { version: 1, condition };
}

const rowNamed = (name: RegExp | string) => screen.getByRole("group", { name });

async function choose(
  user: ReturnType<typeof userEvent.setup>,
  trigger: HTMLElement,
  option: string,
) {
  await user.click(trigger);
  await user.click(await screen.findByRole("option", { name: option }));
}

describe("a condition row", () => {
  it("reads in words, not in fact keys", () => {
    mount(doc({ op: "all", children: [passkey] }));

    const row = rowNamed("Signs in with is A passkey");
    expect(
      within(row).getByRole("combobox", { name: "Condition" }),
    ).toHaveTextContent("Signs in with");
    expect(
      within(row).getByRole("combobox", { name: "Value" }),
    ).toHaveTextContent("A passkey");
    expect(
      within(row).getByRole("radio", { name: "is", checked: true }),
    ).toBeVisible();
  });

  it("names every fact and value in words", async () => {
    const user = userEvent.setup();
    mount(doc({ op: "all", children: [passkey] }));
    const row = rowNamed(/Signs in with/);

    await user.click(within(row).getByRole("combobox", { name: "Condition" }));
    expect(
      (await screen.findAllByRole("option")).map(
        (option) => option.textContent,
      ),
    ).toEqual([
      "Signs in with",
      "Connected to",
      "Connected through",
      "Profile picture",
    ]);
    await user.keyboard("{Escape}");

    await user.click(within(row).getByRole("combobox", { name: "Value" }));
    expect(
      (await screen.findAllByRole("option")).map(
        (option) => option.textContent,
      ),
    ).toEqual(["A passkey", "Password and authenticator", "A linked provider"]);
  });

  it("names protocols and picture sources in words", async () => {
    const user = userEvent.setup();
    mount(
      doc({
        op: "all",
        children: [
          { fact: "connection.protocol", protocol: "oidc" },
          { fact: "avatar", source: "user_uploaded" },
        ],
      }),
    );
    expect(
      screen.getByRole("group", {
        name: "Connected through is OpenID Connect",
      }),
    ).toBeVisible();
    expect(
      screen.getByRole("group", {
        name: "Profile picture is Uploaded by them",
      }),
    ).toBeVisible();

    await user.click(
      within(rowNamed(/Connected through/)).getByRole("combobox", {
        name: "Value",
      }),
    );
    expect(
      (await screen.findAllByRole("option")).map(
        (option) => option.textContent,
      ),
    ).toEqual(["OpenID Connect", "Steam", "VRChat"]);
  });

  it("starts a new provider condition empty, and others on a real value", async () => {
    const user = userEvent.setup();
    mount(doc({ op: "all", children: [passkey] }));
    const row = rowNamed(/Signs in with/);

    await choose(
      user,
      within(row).getByRole("combobox", { name: "Condition" }),
      "Connected to",
    );

    const changed = rowNamed(/Connected to/);
    expect(
      within(changed).getByRole("combobox", { name: "Value" }),
    ).toHaveTextContent("Choose a provider");
    expect(
      within(changed).getByRole("combobox", { name: "Value" }),
    ).toBeInvalid();
    expect(latest?.condition.children?.[0]).toEqual({
      fact: "connection.provider",
      provider: "",
    });

    await choose(
      user,
      within(changed).getByRole("combobox", { name: "Condition" }),
      "Profile picture",
    );
    expect(latest?.condition.children?.[0]).toEqual({
      fact: "avatar",
      source: "any",
    });
  });

  it("negates with is not and back, never leaving neither chosen", async () => {
    const user = userEvent.setup();
    mount(doc({ op: "all", children: [passkey] }));
    const row = rowNamed(/Signs in with/);

    await user.click(within(row).getByRole("radio", { name: "is not" }));
    expect(latest?.condition.children?.[0]).toEqual({
      op: "not",
      child: passkey,
    });
    expect(
      within(rowNamed(/Signs in with is not/)).getByRole("radio", {
        name: "is not",
        checked: true,
      }),
    ).toBeVisible();

    await user.click(
      within(rowNamed(/Signs in with is not/)).getByRole("radio", {
        name: "is not",
      }),
    );
    expect(
      within(rowNamed(/Signs in with is not/)).getByRole("radio", {
        name: "is not",
        checked: true,
      }),
    ).toBeVisible();
    expect(latest?.condition.children?.[0]).toEqual({
      op: "not",
      child: passkey,
    });

    await user.click(
      within(rowNamed(/Signs in with is not/)).getByRole("radio", {
        name: "is",
      }),
    );
    expect(latest?.condition.children?.[0]).toEqual(passkey);
  });

  it("notes a disabled provider, and one with sign-in off, beside its name", async () => {
    const user = userEvent.setup();
    mount(doc({ op: "all", children: [github] }));
    const row = rowNamed(/Connected to/);

    await user.click(within(row).getByRole("combobox", { name: "Value" }));

    expect(
      (await screen.findAllByRole("option")).map(
        (option) => option.textContent,
      ),
    ).toEqual([
      "GitHub",
      "University SSO · sign-in off",
      "Old SSO · disabled",
      "Both SSO · disabled, sign-in off",
    ]);
  });

  it("shows a saved provider the catalogue no longer holds as unknown, and marks it", () => {
    mount(
      doc({
        op: "all",
        children: [{ fact: "connection.provider", provider: "gone" }],
      }),
    );

    const value = within(rowNamed(/Connected to/)).getByRole("combobox", {
      name: "Value",
    });
    expect(value).toHaveTextContent("Unknown provider (gone)");
    expect(value).toBeInvalid();
    expect(
      screen.getByText("That provider is not available on this instance."),
    ).toBeVisible();
  });

  it("removes a condition with a button that names it", async () => {
    const user = userEvent.setup();
    mount(doc({ op: "all", children: [passkey, github] }));

    await user.click(
      screen.getByRole("button", {
        name: "Remove the condition: Signs in with is A passkey",
      }),
    );

    expect(latest?.condition.children).toEqual([github]);
  });
});

describe("the root and nested groups", () => {
  it("lets the root say all or any", async () => {
    const user = userEvent.setup();
    mount(doc({ op: "all", children: [passkey] }));

    expect(screen.getByText(/Accounts that meet/)).toBeVisible();
    const root = screen.getAllByRole("combobox", { name: "Match" })[0];
    expect(root).toHaveTextContent("all");

    await choose(user, root as HTMLElement, "any");

    expect(latest?.condition.op).toBe("any");
  });

  it("adds a condition and a group to the root and to a nested group", async () => {
    const user = userEvent.setup();
    mount(doc({ op: "all", children: [passkey] }));

    await user.click(screen.getByRole("button", { name: "Add group" }));
    expect(latest?.condition.children).toHaveLength(2);
    const well = screen.getByRole("group", { name: "Group" });

    await user.click(
      within(well).getByRole("button", { name: "Add condition" }),
    );
    expect(latest?.condition.children?.[1]?.children).toHaveLength(2);

    await user.click(within(well).getByRole("button", { name: "Add group" }));
    expect(latest?.condition.children?.[1]?.children).toHaveLength(3);
    expect(latest?.condition.children?.[1]?.children?.[2]?.op).toBe("all");

    // The root's own buttons come last: they sit below the group they follow.
    await user.click(
      screen
        .getAllByRole("button", { name: "Add condition" })
        .at(-1) as HTMLElement,
    );
    expect(latest?.condition.children).toHaveLength(3);
  });

  it("removes a group with its remove button", async () => {
    const user = userEvent.setup();
    mount(
      doc({
        op: "all",
        children: [passkey, { op: "any", children: [passkey, github] }],
      }),
    );

    await user.click(screen.getByRole("button", { name: "Remove this group" }));

    expect(latest?.condition.children).toEqual([passkey]);
  });

  it("removes a nested group when its last condition is removed", async () => {
    const user = userEvent.setup();
    mount(
      doc({
        op: "all",
        children: [passkey, { op: "any", children: [github] }],
      }),
    );

    await user.click(
      screen.getByRole("button", {
        name: "Remove the condition: Connected to is GitHub",
      }),
    );

    expect(latest?.condition.children).toEqual([passkey]);
    expect(screen.queryByRole("group", { name: "Group" })).toBeNull();
  });

  it("states that a rule with no conditions has nothing to match", async () => {
    const user = userEvent.setup();
    mount(doc({ op: "all", children: [passkey] }));

    await user.click(
      screen.getByRole("button", {
        name: "Remove the condition: Signs in with is A passkey",
      }),
    );

    expect(
      screen.getAllByText("Add a condition to say who this group matches."),
    ).not.toHaveLength(0);
  });

  it("stops offering groups where a new group's first condition would pass level 8, and says why", () => {
    // Root (level 1) and six groups below it: the innermost sits at level 7.
    let condition: AppAccessRule["condition"] = {
      op: "all",
      children: [passkey],
    };
    for (let level = 0; level < 5; level += 1) {
      condition = { op: "any", children: [passkey, condition] };
    }
    mount(doc({ op: "all", children: [passkey, condition] }));

    // The innermost group's buttons come first in the page.
    const buttons = screen.getAllByRole("button", { name: "Add group" });
    expect(buttons).toHaveLength(7);
    expect(buttons[0]).toBeDisabled();
    for (const button of buttons.slice(1)) expect(button).toBeEnabled();
    const reason =
      "Conditions sit at most 8 levels deep, so no group fits here.";
    expect(screen.getAllByText(reason)).toHaveLength(1);
    expect(buttons[0]).toHaveAccessibleDescription(reason);
    // The group one level up still takes a condition, and its group button has no reason to give.
    expect(
      screen.getAllByRole("button", { name: "Add condition" })[0],
    ).toBeEnabled();
    expect(buttons[1]).not.toHaveAccessibleDescription();
  });

  it("offers groups everywhere above that depth", () => {
    mount(
      doc({
        op: "all",
        children: [passkey, { op: "any", children: [passkey] }],
      }),
    );
    for (const button of screen.getAllByRole("button", { name: "Add group" })) {
      expect(button).toBeEnabled();
    }
    expect(
      screen.queryByText(
        "Conditions sit at most 8 levels deep, so no group fits here.",
      ),
    ).toBeNull();
  });
});

describe("the root group", () => {
  const leaves = (count: number) =>
    Array.from({ length: count }, () => passkey);

  it("marks the root, not its any/all choice, and says why once when it holds too many conditions", () => {
    mount(doc({ op: "all", children: leaves(33) }));

    const root = screen.getAllByRole("combobox", { name: "Match" })[0];
    expect(root).toBeValid();
    expect(root).toHaveAccessibleDescription(
      "A group can hold at most 32 conditions.",
    );
    expect(
      screen.getAllByText("A group can hold at most 32 conditions."),
    ).toHaveLength(1);
    expect(root?.closest("[data-invalid]")).not.toBeNull();
    // The disabled add buttons point at that same message.
    expect(
      screen.getByRole("button", { name: "Add condition" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Add condition" }),
    ).toHaveAccessibleDescription("A group can hold at most 32 conditions.");
  });

  it("stops offering conditions and groups once a group holds 32, and says why", () => {
    mount(doc({ op: "all", children: leaves(32) }));

    const add = screen.getByRole("button", { name: "Add condition" });
    const group = screen.getByRole("button", { name: "Add group" });
    expect(add).toBeDisabled();
    expect(group).toBeDisabled();
    expect(add).toHaveAccessibleDescription(
      "A group can hold at most 32 conditions.",
    );
    expect(group).toHaveAccessibleDescription(
      "A group can hold at most 32 conditions.",
    );
  });

  it("still offers both with one fewer", () => {
    mount(doc({ op: "all", children: leaves(31) }));
    expect(screen.getByRole("button", { name: "Add condition" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Add group" })).toBeEnabled();
  });
});

describe("a saved provider before the list can name it", () => {
  const stored = doc({
    op: "all",
    children: [{ fact: "connection.provider", provider: "retired-sso" }],
  });

  function mountWith(state: "loading" | "failed" | "loaded") {
    render(
      <I18nProvider i18n={i18n}>
        <RuleBuilder
          value={parseRule(stored)}
          onChange={() => undefined}
          issues={[]}
          providers={state === "loaded" ? providers : []}
          providersState={state}
        />
      </I18nProvider>,
    );
    return within(rowNamed(/Connected to/)).getByRole("combobox", {
      name: "Value",
    });
  }

  it("shows only the slug while the list is loading, with no claim about it", () => {
    const value = mountWith("loading");
    expect(value).toHaveTextContent("retired-sso");
    expect(value).not.toHaveTextContent("Unknown");
    expect(value).toBeValid();
    expect(screen.queryByText(/could not be loaded/)).toBeNull();
  });

  it("shows the slug and a quiet note when the list could not be loaded", () => {
    const value = mountWith("failed");
    expect(value).toHaveTextContent("retired-sso");
    expect(value).not.toHaveTextContent("Unknown");
    expect(
      screen.getByText("The provider names could not be loaded."),
    ).toBeVisible();
    expect(value).toHaveAccessibleDescription(
      "The provider names could not be loaded.",
    );
  });

  it("puts the same note over an empty picker when the list could not be loaded", () => {
    render(
      <I18nProvider i18n={i18n}>
        <RuleBuilder
          value={parseRule(
            doc({
              op: "all",
              children: [{ fact: "connection.provider", provider: "" }],
            }),
          )}
          onChange={() => undefined}
          issues={[]}
          providers={[]}
          providersState="failed"
        />
      </I18nProvider>,
    );
    expect(
      screen.getByText("The provider names could not be loaded."),
    ).toBeVisible();
  });

  it("calls it unknown only once the list has loaded, drawing the slug alone in monospace", () => {
    const value = mountWith("loaded");
    expect(value).toHaveTextContent("Unknown provider (retired-sso)");
    const slug = within(value).getByText("retired-sso");
    expect(slug).toHaveAttribute("data-slot", "rule-provider-slug");
    expect(
      within(value).getAllByText(/./, { selector: "[data-slot]" }),
    ).toContain(slug);
  });
});

describe("problems on their rows", () => {
  it("marks a provider condition with no provider on its row", () => {
    mount(
      doc({
        op: "all",
        children: [passkey, { fact: "connection.provider", provider: "" }],
      }),
    );

    const row = rowNamed(/Connected to/);
    expect(within(row).getByRole("combobox", { name: "Value" })).toBeInvalid();
    expect(
      within(row).getByText("Choose a provider for this condition."),
    ).toBeVisible();
    expect(
      within(rowNamed(/Signs in with/)).getByRole("combobox", {
        name: "Value",
      }),
    ).toBeValid();
  });

  it("marks a group with a problem on the group itself", () => {
    mount(doc({ op: "all", children: [passkey, { op: "any", children: [] }] }));

    const well = screen.getByRole("group", { name: "Group" });
    expect(well).toHaveAccessibleDescription(
      "A group needs at least one condition.",
    );
    expect(within(well).getByRole("combobox", { name: "Match" })).toBeValid();
    expect(
      within(well).getByText("A group needs at least one condition."),
    ).toBeVisible();
  });
});

describe("limit problems", () => {
  // Seven nested groups put the condition at level 8, and its negation at 9.
  const deep = (negated: boolean) => {
    let node: AppAccessRule["condition"] = negated
      ? { op: "not", child: passkey }
      : passkey;
    for (let level = 0; level < 7; level += 1) {
      node = { op: "all", children: [node] };
    }
    return doc(node);
  };

  it("marks the row that is too deep, not its fact or value", () => {
    mount(deep(true));

    const row = screen.getByRole("group", { name: /Signs in with is not/ });
    expect(row).toHaveAccessibleDescription(
      "Conditions are nested too deeply. Use at most 8 levels.",
    );
    expect(row).toHaveAttribute("data-invalid");
    expect(
      within(row).getByRole("combobox", { name: "Condition" }),
    ).toBeValid();
    expect(within(row).getByRole("combobox", { name: "Value" })).toBeValid();
  });

  it("marks a nested group that holds too many conditions on its header", () => {
    mount(
      doc({
        op: "all",
        children: [
          { op: "any", children: Array.from({ length: 33 }, () => passkey) },
        ],
      }),
    );

    const well = screen.getByRole("group", { name: "Group" });
    expect(well).toHaveAccessibleDescription(
      "A group can hold at most 32 conditions.",
    );
    expect(within(well).getByRole("combobox", { name: "Match" })).toBeValid();
    expect(
      screen.getAllByText("A group can hold at most 32 conditions."),
    ).toHaveLength(1);
    expect(
      within(well).getByRole("button", { name: "Add condition" }),
    ).toHaveAccessibleDescription("A group can hold at most 32 conditions.");
  });
});
