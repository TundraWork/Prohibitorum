import { I18nProvider } from "@lingui/react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/i18n";
import { RuleJsonTab } from "@/pages/admin/groups/RuleJsonTab";
import {
  parseRule,
  type RuleGroup,
  serializeRule,
} from "@/pages/admin/groups/rule-model";

const passkey = { fact: "login_method", method: "passkey" };
const github = { fact: "connection.provider", provider: "github" };

beforeEach(() => {
  i18n.activate("en");
});

function mount(value: RuleGroup) {
  const onApply = vi.fn<(next: RuleGroup) => void>();
  const view = render(
    <I18nProvider i18n={i18n}>
      <RuleJsonTab value={value} onApply={onApply} />
    </I18nProvider>,
  );
  return { onApply, ...view };
}

const box = () => screen.getByRole("textbox", { name: "Rule document" });

/** Types text into the box and leaves it. */
async function leaveWith(text: string) {
  const user = userEvent.setup();
  await user.clear(box());
  // A brace opens a key chord in userEvent; paste keeps the text literal.
  await user.click(box());
  await user.paste(text);
  await user.tab();
}

describe("the JSON tab", () => {
  it("shows the document the tree would save", () => {
    const root = parseRule({
      version: 1,
      condition: { op: "all", children: [passkey] },
    });
    mount(root);

    expect(JSON.parse((box() as HTMLTextAreaElement).value)).toEqual(
      serializeRule(root),
    );
  });

  it("applies a document when the box is left", async () => {
    const { onApply } = mount(
      parseRule({ version: 1, condition: { op: "all", children: [passkey] } }),
    );

    await leaveWith(
      JSON.stringify({
        version: 1,
        condition: { op: "any", children: [github, passkey] },
      }),
    );

    expect(onApply).toHaveBeenCalledTimes(1);
    expect(serializeRule(onApply.mock.calls[0]?.[0] as RuleGroup)).toEqual({
      version: 1,
      condition: { op: "any", children: [github, passkey] },
    });
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("wraps a single condition in a root all", async () => {
    const { onApply } = mount(
      parseRule({ version: 1, condition: { op: "all", children: [github] } }),
    );

    await leaveWith(JSON.stringify({ version: 1, condition: passkey }));

    expect(
      serializeRule(onApply.mock.calls[0]?.[0] as RuleGroup).condition,
    ).toEqual({ op: "all", children: [passkey] });
  });

  it("leaves the tree alone when the box was not changed", async () => {
    const { onApply } = mount(
      parseRule({ version: 1, condition: { op: "all", children: [passkey] } }),
    );

    const user = userEvent.setup();
    await user.click(box());
    await user.tab();

    expect(onApply).not.toHaveBeenCalled();
  });

  it("says text that is not JSON is not, and keeps the tree", async () => {
    const { onApply } = mount(
      parseRule({ version: 1, condition: { op: "all", children: [passkey] } }),
    );

    await leaveWith("{ nope");

    expect(onApply).not.toHaveBeenCalled();
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(
      screen.getByText(
        "This is not valid JSON. The form view still holds your rule.",
      ),
    ).toBeVisible();
    expect(box()).toBeInvalid();
    expect(box()).toHaveAccessibleDescription(
      "This is not valid JSON. The form view still holds your rule.",
    );
  });

  it("says a document with no condition is not a rule document", async () => {
    const { onApply } = mount(
      parseRule({ version: 1, condition: { op: "all", children: [passkey] } }),
    );

    await leaveWith('{"version":1}');

    expect(onApply).not.toHaveBeenCalled();
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(
      screen.getByText(
        "A rule document needs a version and a condition. The form view still holds your rule.",
      ),
    ).toBeVisible();
  });

  it("clears the message once a document applies", async () => {
    const { onApply } = mount(
      parseRule({ version: 1, condition: { op: "all", children: [passkey] } }),
    );
    await leaveWith("{ nope");
    expect(screen.getByRole("alert")).toBeVisible();

    await leaveWith(
      JSON.stringify({
        version: 1,
        condition: { op: "all", children: [github] },
      }),
    );

    expect(onApply).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("follows the tree when it changes elsewhere", () => {
    const first = parseRule({
      version: 1,
      condition: { op: "all", children: [passkey] },
    });
    const second = parseRule({
      version: 1,
      condition: { op: "any", children: [github] },
    });
    const { rerender } = mount(first);

    rerender(
      <I18nProvider i18n={i18n}>
        <RuleJsonTab value={second} onApply={vi.fn()} />
      </I18nProvider>,
    );

    expect(JSON.parse((box() as HTMLTextAreaElement).value)).toEqual(
      serializeRule(second),
    );
  });
});
