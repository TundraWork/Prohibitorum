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

/**
 * The rendered order of the first cell of each body row.
 *
 * React Aria's grid names its parts differently from a plain HTML table: the
 * leading cell of a row is the row header and every other cell is a gridcell,
 * and the header row is a row as well — hence the rowgroup scoping and the
 * drop of the header row, whose first cell is empty.
 */
function order(): string[] {
  return screen
    .getAllByRole("rowgroup")
    .flatMap((group) => within(group).queryAllByRole("row"))
    .map((row) => {
      const header = within(row).queryByRole("rowheader");
      const first = header ?? within(row).queryAllByRole("gridcell")[0];
      return first?.textContent ?? "";
    })
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
    // query: the console's gray surface marks it as chrome rather than data.
    expect(
      loading.container.querySelector(".bg-surface-secondary"),
    ).not.toBeNull();
    loading.unmount();

    mount();
    const body = screen.getAllByRole("rowgroup")[1];
    if (!body) throw new Error("no table body");
    const first = within(body).getAllByRole("row")[0];
    if (!first) throw new Error("no first row");
    await userEvent.click(within(first).getByRole("button", { name: "Open" }));
    expect(opened).toEqual([1]);
  });
});
