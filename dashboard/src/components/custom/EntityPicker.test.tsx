import { I18nProvider } from "@lingui/react";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { type ReactNode, useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  type EntityOption,
  EntityPicker,
} from "@/components/custom/EntityPicker";
import { i18n } from "@/i18n";

// OverlayScrollbars measures computed styles jsdom cannot resolve; the list's
// scroll area is layout only, so a plain box stands in for it.
vi.mock("@/components/custom/ScrollArea", () => ({
  ScrollArea: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

beforeEach(() => {
  i18n.activate("en");
});

const accounts: EntityOption[] = [
  { id: "2", label: "Mock User 2", description: "mock-user-2" },
  { id: "12", label: "Mock User 12", description: "mock-user-12" },
];

const groups: EntityOption[] = [
  { id: "1", label: "Engineering", description: "engineering" },
  { id: "2", label: "Rule based", description: "rule-based" },
  { id: "3", label: "Support", description: "support" },
];

/**
 * A controlled picker whose value lives in the harness, the way a form field
 * holds it, so a selection round-trips through `onValueChange` into `value`.
 */
function Harness({
  options,
  multiple = false,
  initial = [],
  onValueChange,
  onSearch,
}: {
  options: readonly EntityOption[];
  multiple?: boolean;
  initial?: string[];
  onValueChange?: (next: string[]) => void;
  onSearch?: (query: string) => void;
}) {
  const [value, setValue] = useState<string[]>(initial);
  return (
    <I18nProvider i18n={i18n}>
      <EntityPicker
        label="Picker"
        placeholder="Search"
        searchLabel="Search"
        value={value}
        onValueChange={(next) => {
          onValueChange?.(next);
          setValue(next);
        }}
        onSearch={onSearch}
        options={options}
        loading={false}
        multiple={multiple}
      />
    </I18nProvider>
  );
}

/** The selection as the trigger shows it, beside the button that opens it. */
function shown() {
  const value = document.querySelector('[data-slot="autocomplete-value"]');
  if (value === null) throw new Error("no autocomplete value");
  return value;
}

async function open(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: /Picker/ }));
  return screen.findByRole("listbox");
}

describe("EntityPicker", () => {
  it("hands a single selection over whole, not its first character", async () => {
    const user = userEvent.setup();
    const onValueChange = vi.fn();
    render(<Harness options={accounts} onValueChange={onValueChange} />);

    const listbox = await open(user);
    await user.click(within(listbox).getByRole("option", { name: /User 12/ }));

    expect(onValueChange).toHaveBeenLastCalledWith(["12"]);
    expect(shown()).toHaveTextContent("Mock User 12");
  });

  it("clears a single selection to an empty list", async () => {
    const user = userEvent.setup();
    const onValueChange = vi.fn();
    render(
      <Harness
        options={accounts}
        initial={["12"]}
        onValueChange={onValueChange}
      />,
    );

    await user.click(screen.getByRole("button", { name: /clear/i }));

    expect(onValueChange).toHaveBeenLastCalledWith([]);
    expect(shown()).toHaveTextContent("Search");
  });

  it("hands every id of a multiple selection over", async () => {
    const user = userEvent.setup();
    const onValueChange = vi.fn();
    render(<Harness options={groups} multiple onValueChange={onValueChange} />);

    const listbox = await open(user);
    await user.click(
      within(listbox).getByRole("option", { name: /Engineering/ }),
    );
    await user.click(within(listbox).getByRole("option", { name: /Support/ }));

    expect(onValueChange).toHaveBeenLastCalledWith(["1", "3"]);
  });

  it("shows a multiple selection as one line of names", () => {
    render(<Harness options={groups} multiple initial={["1", "3"]} />);

    expect(shown()).toHaveTextContent("Engineering and Support");
    expect(shown()).not.toHaveTextContent("engineering");
    expect(shown()).not.toHaveTextContent("support");
  });

  it("filters the options itself when nothing searches the server", async () => {
    const user = userEvent.setup();
    render(<Harness options={groups} multiple />);

    const listbox = await open(user);
    await user.keyboard("rule");

    expect(within(listbox).getAllByRole("option")).toHaveLength(1);
    expect(within(listbox).getByRole("option")).toHaveTextContent("Rule based");
  });

  it("leaves the options to the server when the caller searches", async () => {
    const user = userEvent.setup();
    const onSearch = vi.fn();
    render(<Harness options={accounts} onSearch={onSearch} />);

    const listbox = await open(user);
    await user.keyboard("zzz");

    expect(onSearch).toHaveBeenLastCalledWith("zzz");
    expect(within(listbox).getAllByRole("option")).toHaveLength(2);
  });

  it("lists an unavailable option with its reason and refuses it", async () => {
    const user = userEvent.setup();
    const onValueChange = vi.fn();
    render(
      <Harness
        options={[
          ...accounts,
          { id: "4", label: "Mock User 4", unavailableReason: "Disabled" },
          {
            id: "5",
            label: "Mock User 5",
            description: "mock-user-5",
            unavailableReason: "Already a manager",
          },
        ]}
        onValueChange={onValueChange}
      />,
    );

    const listbox = await open(user);
    const disabled = within(listbox).getByRole("option", { name: /User 4/ });
    expect(disabled).toHaveTextContent("Disabled");
    expect(disabled).toHaveAttribute("aria-disabled", "true");
    expect(
      within(listbox).getByRole("option", { name: /User 5/ }),
    ).toHaveTextContent("mock-user-5 · Already a manager");

    await user.click(disabled);
    expect(onValueChange).not.toHaveBeenCalled();
  });
});
