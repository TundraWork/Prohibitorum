import { I18nProvider } from "@lingui/react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { TagListField } from "@/components/custom/TagListField";
import { useAppForm } from "@/forms/use-app-form";
import { i18n } from "@/i18n";
import {
  scopeProblem,
  scopesProblem,
  type TagListValue,
} from "@/pages/admin/identity-providers/provider-validation";

function Harness({
  onSave,
  initial = ["openid", "profile"],
}: {
  onSave: (value: TagListValue) => void;
  initial?: string[];
}) {
  const form = useAppForm({
    defaultValues: { scopes: { tags: initial, draft: "" } },
    onSubmit: ({ value }) => onSave(value.scopes),
  });

  return (
    <I18nProvider i18n={i18n}>
      <form.AppForm>
        <form.Form label="scopes">
          <form.AppField
            name="scopes"
            validators={{ onSubmit: ({ value }) => scopesProblem(value) }}
          >
            {() => (
              <TagListField
                label="Scopes"
                addLabel="Add"
                check={scopeProblem}
                lockedTags={["openid"]}
              />
            )}
          </form.AppField>
          <form.SubmitButton>Save</form.SubmitButton>
        </form.Form>
      </form.AppForm>
    </I18nProvider>
  );
}

const input = () => screen.getByRole("textbox", { name: "Add to Scopes" });

describe("TagListField", () => {
  beforeEach(() => i18n.activate("en"));

  it("adds a value on Enter and clears the input", async () => {
    const onSave = vi.fn();
    render(<Harness onSave={onSave} />);

    await userEvent.type(input(), "email{Enter}");

    expect(screen.getByRole("row", { name: "email" })).toBeTruthy();
    expect((input() as HTMLInputElement).value).toBe("");
    // Enter added the value; it did not submit the form.
    expect(onSave).not.toHaveBeenCalled();
  });

  it("adds a value with the button", async () => {
    render(<Harness onSave={vi.fn()} />);

    await userEvent.type(input(), "offline_access");
    await userEvent.click(screen.getByRole("button", { name: "Add" }));

    expect(screen.getByRole("row", { name: "offline_access" })).toBeTruthy();
  });

  it("keeps a refused value in the input with the reason under it", async () => {
    render(<Harness onSave={vi.fn()} />);

    await userEvent.type(input(), "read write{Enter}");

    expect(screen.queryByRole("row", { name: "read write" })).toBeNull();
    expect((input() as HTMLInputElement).value).toBe("read write");
    expect(input().getAttribute("aria-invalid")).toBe("true");
    expect(
      screen.getByText("A scope cannot contain spaces, quotes or backslashes."),
    ).toBeTruthy();

    await userEvent.clear(input());
    await userEvent.type(input(), "profile{Enter}");
    expect(screen.getByText("Already in the list.")).toBeTruthy();
  });

  it("removes a tag from its own button, but never a locked one", async () => {
    render(<Harness onSave={vi.fn()} />);

    expect(screen.queryByRole("button", { name: "Remove openid" })).toBeNull();
    await userEvent.click(
      screen.getByRole("button", { name: "Remove profile" }),
    );

    expect(screen.queryByRole("row", { name: "profile" })).toBeNull();
    expect(screen.getByRole("row", { name: "openid" })).toBeTruthy();
  });

  it("does not remove the last tag on Backspace in the empty input", async () => {
    render(<Harness onSave={vi.fn()} />);

    await userEvent.click(input());
    await userEvent.keyboard("{Backspace}");

    expect(screen.getByRole("row", { name: "profile" })).toBeTruthy();
  });

  it("refuses to submit while text is left in the input", async () => {
    const onSave = vi.fn();
    render(<Harness onSave={onSave} />);

    await userEvent.type(input(), "email");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(onSave).not.toHaveBeenCalled();
    expect(
      await screen.findByText("Press Enter to add it, or clear the box."),
    ).toBeTruthy();
    expect((input() as HTMLInputElement).value).toBe("email");

    await userEvent.clear(input());
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(onSave).toHaveBeenCalledWith({
      tags: ["openid", "profile"],
      draft: "",
    });
  });
});
