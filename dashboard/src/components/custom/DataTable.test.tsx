import { I18nProvider } from "@lingui/react";
import type { SortingState } from "@tanstack/react-table";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it } from "vitest";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { i18n } from "@/i18n";

// The catalogs are not loaded until a locale is active, and an un-activated
// `i18n` makes every `Trans` throw — which React swallows by unmounting the
// tree, leaving a test that queries an empty document.
beforeEach(() => {
  i18n.activate("en");
});

interface Row {
  id: number;
  name: string;
  size: number;
}

const rows: Row[] = [
  { id: 1, name: "gamma", size: 30 },
  { id: 2, name: "alpha", size: 10 },
  { id: 3, name: "beta", size: 20 },
];

const opened: number[] = [];

function columns(): TableColumn<Row>[] {
  return [
    {
      id: "name",
      header: "Name",
      cell: (row) => row.name,
      sortValue: (row) => row.name,
    },
    {
      align: "end",
      id: "size",
      header: "Size",
      cell: (row) => String(row.size),
      sortValue: (row) => row.size,
    },
    // No `sortValue`: nothing to order by, so the header offers no control.
    { id: "note", header: "Note", cell: () => "—" },
    {
      id: "actions",
      header: "Actions",
      cell: (row) => (
        <button type="button" onClick={() => opened.push(row.id)}>
          Open
        </button>
      ),
      pinned: true,
    },
  ];
}

function mount(props: Partial<Parameters<typeof DataTable<Row>>[0]> = {}) {
  return render(
    <I18nProvider i18n={i18n}>
      <DataTable<Row>
        label="Rows"
        columns={columns()}
        rows={rows}
        rowId={(row) => row.id}
        empty={<span>Nothing here</span>}
        {...props}
      />
    </I18nProvider>,
  );
}

/** The rendered order of the first cell of each body row. */
function order(): string[] {
  const body = screen.getAllByRole("rowgroup")[1];
  if (!body) throw new Error("no table body");
  return within(body)
    .getAllByRole("row")
    .map((row) => within(row).getAllByRole("cell")[0]?.textContent ?? "")
    .filter((text) => text !== "");
}

describe("DataTable", () => {
  it("renders the rows, sorts by a column that offers a sort value, and leaves the rest unsortable", async () => {
    mount();
    expect(order()).toEqual(["gamma", "alpha", "beta"]);

    await userEvent.click(screen.getByText("Name"));
    expect(order()).toEqual(["alpha", "beta", "gamma"]);
    await userEvent.click(screen.getByText("Name"));
    expect(order()).toEqual(["gamma", "beta", "alpha"]);

    // A header with nothing to order by is a plain label, not a control.
    expect(
      screen.queryByRole("button", { name: /Note/ }),
    ).not.toBeInTheDocument();
  });

  it("draws a header row of column headers and marks only the sorted one with aria-sort", async () => {
    mount();
    const head = screen.getAllByRole("rowgroup")[0];
    if (!head) throw new Error("no table head");
    const headerRow = within(head).getByRole("row");
    const headers = within(headerRow).getAllByRole("columnheader");
    expect(headers.map((header) => header.textContent)).toEqual([
      "Name",
      "Size",
      "Note",
      "Actions",
    ]);
    // Offered but not applied yet, and absent where there is nothing to order.
    expect(headers[0]).toHaveAttribute("aria-sort", "none");
    expect(headers[2]).not.toHaveAttribute("aria-sort");

    await userEvent.click(screen.getByRole("button", { name: "Name" }));
    expect(headers[0]).toHaveAttribute("aria-sort", "ascending");
    await userEvent.click(screen.getByRole("button", { name: "Name" }));
    expect(headers[0]).toHaveAttribute("aria-sort", "descending");
    expect(headers[1]).toHaveAttribute("aria-sort", "none");
  });

  it("marks the pinned column's header and cells as pinned to the end", () => {
    mount();
    const header = screen.getByRole("columnheader", { name: "Actions" });
    expect(header).toHaveAttribute("data-pinned", "end");
    const body = screen.getAllByRole("rowgroup")[1];
    if (!body) throw new Error("no table body");
    const cells = within(body).getAllByRole("cell");
    const pinned = cells.filter(
      (cell) => cell.getAttribute("data-pinned") === "end",
    );
    expect(pinned).toHaveLength(rows.length);
    expect(
      screen.getByRole("columnheader", { name: "Name" }),
    ).not.toHaveAttribute("data-pinned");
  });

  it("hands the order back to a controlled caller instead of sorting itself", async () => {
    const seen: SortingState[] = [];
    mount({ sorting: [], onSortChange: (next) => seen.push(next) });
    await userEvent.click(screen.getByText("Size"));
    expect(seen.at(-1)).toEqual([{ desc: false, id: "size" }]);
    // The caller owns the order, so the rows stay as they were given.
    expect(order()).toEqual(["gamma", "alpha", "beta"]);
  });

  it("shows the empty state, a loading row instead while the first page is on its way, and keeps pinned actions working", async () => {
    const empty = mount({ rows: [] });
    expect(screen.getByText("Nothing here")).toBeInTheDocument();
    empty.unmount();

    const loading = mount({ rows: [], loading: true });
    expect(screen.queryByText("Nothing here")).not.toBeInTheDocument();
    // The stand-in is decorative on purpose, so it has no accessible name to
    // query: one placeholder per cell, three rows deep, holds the table's shape.
    expect(
      loading.container.querySelectorAll("[data-slot=skeleton]"),
    ).toHaveLength(3 * columns().length);
    loading.unmount();

    mount();
    const body = screen.getAllByRole("rowgroup")[1];
    if (!body) throw new Error("no table body");
    const first = within(body).getAllByRole("row")[0];
    if (!first) throw new Error("no first row");
    await userEvent.click(within(first).getByRole("button", { name: "Open" }));
    expect(opened).toEqual([1]);
  });

  it("opens a row's detail in a child row that spans every column, and offers no toggle for a row without one", async () => {
    mount({
      expandedRow: (row) =>
        row.id === 2 ? null : <span>Detail of {row.name}</span>,
    });

    // The detail is not rendered until its row is opened.
    expect(screen.queryByText("Detail of gamma")).not.toBeInTheDocument();
    // Each toggle is named with its row, so a screen reader hears which one.
    const toggles = screen.getAllByRole("button", { name: /^Show details/ });
    // gamma and beta have a detail; alpha does not.
    expect(toggles).toHaveLength(2);
    expect(
      screen.getByRole("button", { name: "Show details gamma" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Show details alpha" }),
    ).not.toBeInTheDocument();

    const toggle = screen.getByRole("button", { name: "Show details gamma" });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    await userEvent.click(toggle);

    const detail = await screen.findByText("Detail of gamma");
    const cell = detail.closest("td");
    expect(cell).not.toBeNull();
    expect(cell?.getAttribute("colspan")).toBe(String(columns().length));
    const open = screen.getByRole("button", { name: "Hide details gamma" });
    expect(open).toHaveAttribute("aria-expanded", "true");
    // The toggle points at the row it opened.
    expect(cell?.closest("tr")?.id).toBe(open.getAttribute("aria-controls"));
    expect(screen.queryByText("Detail of beta")).not.toBeInTheDocument();

    await userEvent.click(
      screen.getByRole("button", { name: "Hide details gamma" }),
    );
    expect(screen.queryByText("Detail of gamma")).not.toBeInTheDocument();
  });

  it("draws no toggle at all when the table has no details", () => {
    mount();
    expect(
      screen.queryByRole("button", { name: /^Show details/ }),
    ).not.toBeInTheDocument();
  });
});
