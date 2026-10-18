import { I18nProvider } from "@lingui/react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { DangerZone, DangerZoneFrame } from "@/components/custom/DangerZone";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { i18n } from "@/i18n";

describe("DangerZoneFrame", () => {
  it("is a destructive-toned frame titled at level 2 whose rows share one panel", () => {
    const { container } = render(
      <DangerZoneFrame title="Danger zone">
        <ItemList label="Danger zone" empty={null}>
          <ItemListRow
            key="a"
            title="Delete it"
            details={["This cannot be undone."]}
            actions={<button type="button">Delete</button>}
          />
        </ItemList>
      </DangerZoneFrame>,
    );

    const frame = container.querySelector("[data-slot='frame']");
    expect(frame).toHaveAttribute("data-tone", "danger");
    expect(
      screen.getByRole("heading", { level: 2, name: "Danger zone" }),
    ).toBeInTheDocument();
    expect(
      container.querySelectorAll("[data-slot='frame-panel']"),
    ).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Delete" })).toBeInTheDocument();
  });
});

describe("DangerZone", () => {
  it("confirms before acting, and a disabled one acts on nothing", async () => {
    i18n.activate("en");
    const onConfirm = vi.fn();
    const props = {
      label: "Delete",
      title: "Delete it?",
      body: <p>Gone for good.</p>,
      confirmLabel: "Delete forever",
      description: "Removes it.",
      onConfirm,
    };
    const { rerender } = render(
      <I18nProvider i18n={i18n}>
        <DangerZone {...props} />
      </I18nProvider>,
    );
    const user = userEvent.setup();

    expect(screen.getByText("Removes it.")).toHaveClass(
      "text-muted-foreground",
    );
    await user.click(screen.getByRole("button", { name: "Delete" }));
    expect(onConfirm).not.toHaveBeenCalled();
    await user.click(
      await screen.findByRole("button", { name: "Delete forever" }),
    );
    expect(onConfirm).toHaveBeenCalledTimes(1);

    rerender(
      <I18nProvider i18n={i18n}>
        <DangerZone {...props} disabled disabledReason="Not allowed." />
      </I18nProvider>,
    );
    expect(screen.getByRole("button", { name: "Delete" })).toBeDisabled();
  });
});
