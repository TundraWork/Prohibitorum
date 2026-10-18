import { I18nProvider } from "@lingui/react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ValueSet } from "@/components/custom/ValueSet";
import { i18n } from "@/i18n";

beforeEach(() => i18n.activate("en"));

function setup(props: Partial<React.ComponentProps<typeof ValueSet>> = {}) {
  const onAdd = vi.fn();
  const onRemove = vi.fn();
  render(
    <I18nProvider i18n={i18n}>
      <ValueSet
        values={["a.com", "b.com"]}
        onAdd={onAdd}
        onRemove={onRemove}
        addLabel="Add domain"
        placeholder="example.com"
        {...props}
      />
    </I18nProvider>,
  );
  return { onAdd, onRemove };
}

const openPopover = () =>
  userEvent.click(screen.getByRole("button", { name: "Add domain" }));
const input = () => screen.getByRole("textbox", { name: "Add domain" });

describe("ValueSet", () => {
  it("renders a chip per value and removes one", async () => {
    const { onRemove } = setup();
    expect(screen.getByText("a.com")).toBeTruthy();
    expect(screen.getByText("b.com")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Remove a.com" }));
    expect(onRemove).toHaveBeenCalledWith("a.com");
  });

  it("adds the trimmed draft on Enter and clears the input", async () => {
    const { onAdd } = setup();
    await openPopover();
    await userEvent.type(input(), "  c.com {Enter}");
    expect(onAdd).toHaveBeenCalledWith("c.com");
    expect((input() as HTMLInputElement).value).toBe("");
  });

  it("adds with the Add button", async () => {
    const { onAdd } = setup();
    await openPopover();
    await userEvent.type(input(), "c.com");
    await userEvent.click(screen.getByRole("button", { name: "Add" }));
    expect(onAdd).toHaveBeenCalledWith("c.com");
  });

  it("blocks an add the validator refuses and says why", async () => {
    const { onAdd } = setup({
      validate: (draft) => (draft === "bad" ? "Not allowed." : undefined),
    });
    await openPopover();
    await userEvent.type(input(), "bad{Enter}");
    expect(onAdd).not.toHaveBeenCalled();
    expect(screen.getByText("Not allowed.")).toBeTruthy();
    expect(input().getAttribute("aria-invalid")).toBe("true");
  });

  it("shows the empty text only for an empty list", () => {
    setup({ values: [], emptyText: "Every domain is accepted." });
    expect(screen.getByText("Every domain is accepted.")).toBeTruthy();
  });

  it("does not show the empty text when there are values", () => {
    setup({ emptyText: "Every domain is accepted." });
    expect(screen.queryByText("Every domain is accepted.")).toBeNull();
  });

  it("offers suggestions when given and adds one on press", async () => {
    const { onAdd } = setup({
      suggestions: [{ value: "profile", description: "Basic profile" }],
      noSuggestionsNote: "No list published.",
    });
    await openPopover();
    expect(screen.queryByText("No list published.")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: /profile/ }));
    expect(onAdd).toHaveBeenCalledWith("profile");
  });

  it("shows the note when no suggestions are published", async () => {
    setup({ noSuggestionsNote: "No list published." });
    await openPopover();
    expect(screen.getByText("No list published.")).toBeTruthy();
  });

  it("can hold its draft in the caller", async () => {
    function Controlled() {
      const [draft, setDraft] = useState("");
      return (
        <I18nProvider i18n={i18n}>
          <ValueSet
            values={[]}
            onAdd={() => {}}
            onRemove={() => {}}
            addLabel="Add domain"
            placeholder="x"
            draft={draft}
            onDraftChange={setDraft}
          />
          <output>{draft}</output>
        </I18nProvider>
      );
    }
    render(<Controlled />);
    await openPopover();
    await userEvent.type(input(), "abc");
    expect(screen.getByRole("status").textContent).toBe("abc");
  });
});
