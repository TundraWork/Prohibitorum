import { I18nProvider } from "@lingui/react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ConsequenceFooter } from "@/components/custom/ConsequenceFooter";
import { i18n } from "@/i18n";

beforeEach(() => {
  i18n.activate("en");
});

function mount(props: Partial<Parameters<typeof ConsequenceFooter>[0]> = {}) {
  const onDiscard = vi.fn();
  render(
    <I18nProvider i18n={i18n}>
      <ConsequenceFooter
        consequence="Clients still asking for email will be refused."
        actionLabel="Remove scope and save"
        onDiscard={onDiscard}
        {...props}
      />
    </I18nProvider>,
  );
  return { onDiscard };
}

describe("ConsequenceFooter", () => {
  it("says what the save will do and names the primary button for it", () => {
    mount();
    expect(
      screen.getByText("Clients still asking for email will be refused."),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Remove scope and save" }),
    ).toHaveAttribute("type", "submit");
  });

  it("discards through its own button", async () => {
    const { onDiscard } = mount();
    await userEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(onDiscard).toHaveBeenCalledTimes(1);
  });

  it("leaves the discard button a plain button, so it never submits", () => {
    mount();
    expect(screen.getByRole("button", { name: "Discard" })).toHaveAttribute(
      "type",
      "button",
    );
  });

  it("marks the primary button busy and holds Discard while the save runs", () => {
    mount({ pending: true });
    expect(
      screen.getByRole("button", { name: "Remove scope and save" }),
    ).toHaveAttribute("aria-busy", "true");
    expect(screen.getByRole("button", { name: "Discard" })).toBeDisabled();
  });

  it("disables the primary button when told to", () => {
    mount({ disabled: true });
    expect(
      screen.getByRole("button", { name: "Remove scope and save" }),
    ).toBeDisabled();
  });

  it("points a button it disabled at the sentence that says why", () => {
    mount({ disabled: true });
    expect(
      screen.getByRole("button", { name: "Remove scope and save" }),
    ).toHaveAccessibleDescription(
      "Clients still asking for email will be refused.",
    );
  });

  it("leaves an enabled button without a description", () => {
    mount();
    expect(
      screen.getByRole("button", { name: "Remove scope and save" }),
    ).not.toHaveAccessibleDescription();
  });

  it("reads as a warning unless told it states a refusal", () => {
    const { container, rerender } = render(
      <I18nProvider i18n={i18n}>
        <ConsequenceFooter
          consequence="Choose a provider."
          actionLabel="Save"
          onDiscard={vi.fn()}
          tone="danger"
        />
      </I18nProvider>,
    );
    expect(container.firstElementChild).toHaveAttribute("data-tone", "danger");

    rerender(
      <I18nProvider i18n={i18n}>
        <ConsequenceFooter
          consequence="Saving changes who is in."
          actionLabel="Save"
          onDiscard={vi.fn()}
        />
      </I18nProvider>,
    );
    expect(container.firstElementChild).toHaveAttribute("data-tone", "warning");
  });

  it("draws no warning icon for a neutral note", () => {
    const { container } = render(
      <I18nProvider i18n={i18n}>
        <ConsequenceFooter
          consequence="2 unsaved changes"
          actionLabel="Save"
          onDiscard={vi.fn()}
          tone="neutral"
        />
      </I18nProvider>,
    );
    expect(container.firstElementChild).toHaveAttribute("data-tone", "neutral");
    expect(container.querySelector("svg")).toBeNull();
    expect(screen.getByText("2 unsaved changes")).toBeVisible();
  });

  it("still offers the buttons with no consequence to state", () => {
    mount({ consequence: undefined });
    expect(screen.getByRole("button", { name: "Discard" })).toBeInTheDocument();
  });
});
