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

  it("draws a badge cell only for the rows that have a badge", () => {
    render(
      <ItemList label="Factors" empty={null}>
        <ItemListRow key="a" title="Codes" badge={<span>Low</span>} meta="2" />
        <ItemListRow key="b" title="Passkeys" meta="3" />
      </ItemList>,
    );

    const [withBadge, without] = screen.getAllByRole("listitem");
    const slot = (item: HTMLElement | undefined) =>
      item?.querySelector("[data-slot='item-badge']");
    expect(slot(withBadge)).toHaveTextContent("Low");
    expect(slot(without)).toBeNull();
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

  it("keeps the badge and meta out of the details and beside the text block", () => {
    render(
      <ItemList label="Passkeys" empty={null}>
        <ItemListRow
          key="a"
          icon={<i />}
          title="Laptop"
          details={["Added today"]}
          badge="Synced"
          meta="Used now"
        />
      </ItemList>,
    );

    const extras = screen
      .getByText("Synced")
      .closest("[data-slot='item-extras']") as HTMLElement;
    expect(extras).toContainElement(screen.getByText("Used now"));
    const text = screen
      .getByText("Laptop")
      .closest("[data-slot='item-text']") as HTMLElement;
    expect(text).not.toContainElement(extras);
    expect(text).toContainElement(screen.getByText("Added today"));
  });

  it("draws a row's actions in their own cell", () => {
    render(
      <ItemList label="Passkeys" empty={null}>
        <ItemListRow
          key="a"
          title="Laptop"
          actions={<button type="button">More</button>}
        />
      </ItemList>,
    );

    expect(
      screen
        .getByRole("button", { name: "More" })
        .closest("[data-slot='item-actions']"),
    ).not.toBeNull();
  });

  it("treats a null badge or meta as absent", () => {
    render(
      <ItemList label="Passkeys" empty={null}>
        <ItemListRow key="a" title="Laptop" badge={null} meta={null} />
      </ItemList>,
    );

    expect(document.querySelector("[data-slot='item-badge']")).toBeNull();
  });

  it("treats a false badge or meta, as `cond && <Badge />` yields, as absent", () => {
    render(
      <ItemList label="Passkeys" empty={null}>
        <ItemListRow key="a" title="Laptop" badge={false} meta={false} />
      </ItemList>,
    );

    expect(document.querySelector("[data-slot='item-badge']")).toBeNull();
    expect(document.querySelector("[data-slot='item-meta']")).toBeNull();
  });

  it("places the badge and meta line after the details on a narrow screen", () => {
    render(
      <ItemList label="Managers" empty={null}>
        <ItemListRow
          key="a"
          title="Mock User"
          details={["@mock-user"]}
          meta="Last week"
        />
        <ItemListRow key="b" title="Other" meta="Today" />
      </ItemList>,
    );

    const [withDetails, without] = Array.from(
      document.querySelectorAll<HTMLElement>("[data-slot='item-extras']"),
    );
    // Name is row 1 and the details row 2, so the extras take row 3; with no
    // details the extras follow the name directly.
    expect(withDetails?.style.getPropertyValue("--item-extras")).toBe("3");
    expect(without?.style.getPropertyValue("--item-extras")).toBe("2");
  });

  it("puts a stacked row's action under its text only when asked", () => {
    render(
      <ItemList label="Danger" empty={null}>
        <ItemListRow
          key="a"
          title="Delete"
          details={["Removes it for good."]}
          stackActions
          actions={<button type="button">Delete</button>}
        />
        <ItemListRow
          key="b"
          title="Laptop"
          actions={<button type="button">More</button>}
        />
      </ItemList>,
    );

    const [stacked, beside] = Array.from(
      document.querySelectorAll<HTMLElement>("[data-slot='item-actions']"),
    );
    expect(stacked).toHaveAttribute("data-stacked", "true");
    expect(beside).not.toHaveAttribute("data-stacked");
    expect(stacked?.style.getPropertyValue("--item-actions-row")).toBe("3");
  });

  it("draws no footer for a false or null footer, as `cond && <Button />` yields", () => {
    const { container, rerender } = render(
      <ItemList label="Keys" empty={null} footer={false}>
        <ItemListRow key="a" title="Key" />
      </ItemList>,
    );
    const panel = container.querySelector("[data-slot='frame-panel']");
    // The list is the panel's only child, so `divide-y` draws no rule under it.
    expect(panel?.children).toHaveLength(1);

    rerender(
      <ItemList label="Keys" empty={null} footer={null}>
        <ItemListRow key="a" title="Key" />
      </ItemList>,
    );
    expect(panel?.children).toHaveLength(1);

    rerender(
      <ItemList
        label="Keys"
        empty={null}
        footer={<button type="button">More</button>}
      >
        <ItemListRow key="a" title="Key" />
      </ItemList>,
    );
    expect(panel?.children).toHaveLength(2);
    expect(screen.getByRole("button", { name: "More" })).toBeInTheDocument();
  });
});
