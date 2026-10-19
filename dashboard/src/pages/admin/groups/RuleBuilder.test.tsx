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

  it("stops offering groups at the depth the server allows, and says why", () => {
    // Root, five groups, and a sixth group whose condition sits at level 8.
    let condition: AppAccessRule["condition"] = {
      op: "all",
      children: [passkey],
    };
    for (let level = 0; level < 5; level += 1) {
      condition = { op: "any", children: [passkey, condition] };
    }
    mount(doc({ op: "all", children: [passkey, condition] }));

    const disabled = screen
      .getAllByRole("button", { name: "Add group" })
      .filter((button) => button.hasAttribute("disabled"));
    expect(disabled.length).toBeGreaterThan(0);
    expect(screen.getAllByText("Groups nest at most 8 deep.").length).toBe(
      disabled.length,
    );
    expect(disabled[0]).toHaveAccessibleDescription(
      "Groups nest at most 8 deep.",
    );
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
    expect(screen.queryByText("Groups nest at most 8 deep.")).toBeNull();
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
    expect(within(well).getByRole("combobox", { name: "Match" })).toBeInvalid();
    expect(
      within(well).getByText("A group needs at least one condition."),
    ).toBeVisible();
  });
});
