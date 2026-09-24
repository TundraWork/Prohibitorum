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
});
