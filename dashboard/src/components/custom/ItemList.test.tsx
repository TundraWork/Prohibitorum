import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";

describe("ItemList", () => {
  it("draws one list item per row, with the details on one line in order", () => {
    render(
      <ItemList label="Passkeys" empty={<p>Nothing here</p>}>
        <ItemListRow
          key="a"
          title="Laptop"
          details={["Added today", undefined, false, "Last used now"]}
          actions={<button type="button">Remove</button>}
        />
        <ItemListRow key="b" title="Phone" />
      </ItemList>,
    );

    const list = screen.getByRole("list", { name: "Passkeys" });
    const items = within(list).getAllByRole("listitem");
    expect(items).toHaveLength(2);
    // Absent details are skipped rather than drawn as empty separators.
    expect(items[0]).toHaveTextContent("Laptop");
    expect(items[0]).toHaveTextContent("Added today·Last used now");
    expect(
      within(items[0] as HTMLElement).getByRole("button", { name: "Remove" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("Nothing here")).not.toBeInTheDocument();
  });

  it("renders its rows in one divided panel, and its details as secondary text", () => {
    const { container } = render(
      <ItemList label="Passkeys" empty={null}>
        <ItemListRow key="a" title="Laptop" details={["Added today"]} />
      </ItemList>,
    );

    const panels = container.querySelectorAll("[data-slot='frame-panel']");
    expect(panels).toHaveLength(1);
    expect(panels[0]).toContainElement(screen.getByRole("list"));
    expect(
      screen.getByText("Added today").closest("[data-slot='item-details']"),
    ).not.toBeNull();
  });

  it("shows the empty state instead of a list when there are no rows", () => {
    render(<ItemList label="Passkeys" empty={<p>Nothing here</p>} />);

    expect(screen.getByText("Nothing here")).toBeInTheDocument();
    expect(screen.queryByRole("list")).not.toBeInTheDocument();
  });

  it("stands in with a spinner while the first rows load, not the empty state", () => {
    render(<ItemList label="Passkeys" loading empty={<p>Nothing here</p>} />);

    expect(screen.queryByText("Nothing here")).not.toBeInTheDocument();
    expect(screen.queryByRole("list")).not.toBeInTheDocument();
  });

  it("names a titled list with a heading, and renders a linked row as its link", () => {
    render(
      <ItemList label="Access tokens" title="Access tokens" empty={null}>
        <ItemListRow
          key="a"
          title="CI"
          link={({ className, children }) => (
            <a className={className} href="/tokens/1">
              {children}
            </a>
          )}
        />
      </ItemList>,
    );

    expect(
      screen.getByRole("heading", { level: 2, name: "Access tokens" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "CI" })).toHaveAttribute(
      "href",
      "/tokens/1",
    );
  });

  it("mutes a dimmed row's name and leaves its actions alone", () => {
    render(
      <ItemList label="User groups" empty={null}>
        <ItemListRow
          key="a"
          title="Kept"
          dimmed
          actions={<button type="button">Remove</button>}
        />
        <ItemListRow key="b" title="Active" />
      </ItemList>,
    );

    expect(screen.getByText("Kept")).toHaveAttribute("data-dimmed", "true");
    expect(screen.getByText("Active")).not.toHaveAttribute("data-dimmed");
    // Nothing in the row is faded by opacity, the action included.
    expect(document.querySelector("[class*='opacity-']")).toBeNull();
  });

  it("places the badge and the meta in their own columns, apart from the details", () => {
    render(
      <ItemList label="Passkeys" empty={null}>
        <ItemListRow
          key="a"
          title="Laptop"
          details={["Added today"]}
          badge={<span>Synced</span>}
          meta={<span>Used now</span>}
          actions={<button type="button">More</button>}
        />
      </ItemList>,
    );

    const badge = screen
      .getByText("Synced")
      .closest("[data-slot='item-badge']");
    const meta = screen
      .getByText("Used now")
      .closest("[data-slot='item-meta']");
    expect(badge).not.toBeNull();
    expect(meta).not.toBeNull();
    const details = screen
      .getByText("Added today")
      .closest("[data-slot='item-details']");
    expect(details).not.toContainElement(badge as HTMLElement);
    expect(details).not.toContainElement(meta as HTMLElement);
  });

  it("draws no badge or meta column for a row without them", () => {
    const { container } = render(
      <ItemList label="Passkeys" empty={null}>
        <ItemListRow key="a" title="Laptop" />
      </ItemList>,
    );

    expect(container.querySelector("[data-slot='item-badge']")).toBeNull();
    expect(container.querySelector("[data-slot='item-meta']")).toBeNull();
  });
});
