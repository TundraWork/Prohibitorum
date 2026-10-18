import { msg } from "@lingui/core/macro";
import { I18nProvider } from "@lingui/react";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it } from "vitest";
import {
  OptionSelectField,
  type SelectOption,
} from "@/components/custom/OptionSelectField";
import { useAppForm } from "@/forms/use-app-form";
import { i18n } from "@/i18n";

type Mode = "a" | "b" | "c";

const options: SelectOption<Mode>[] = [
  {
    value: "a",
    label: msg({ id: "test.option.a", message: "Alpha" }),
    description: msg({ id: "test.option.a.hint", message: "Does alpha." }),
  },
  {
    value: "b",
    label: msg({ id: "test.option.b", message: "Beta" }),
    description: msg({ id: "test.option.b.hint", message: "Does beta." }),
  },
  {
    value: "c",
    label: msg({ id: "test.option.c", message: "Gamma" }),
    description: msg({ id: "test.option.c.hint", message: "Does gamma." }),
  },
];

beforeEach(() => {
  i18n.activate("en");
});

function Harness({ disabledKeys }: { disabledKeys?: Mode[] }) {
  const form = useAppForm({ defaultValues: { mode: "a" as Mode } });
  return (
    <I18nProvider i18n={i18n}>
      <form.AppForm>
        <form.AppField name="mode">
          {() => (
            <OptionSelectField<Mode>
              label="Mode"
              options={options}
              disabledKeys={disabledKeys}
            />
          )}
        </form.AppField>
      </form.AppForm>
      <form.Subscribe selector={(state) => state.values.mode}>
        {(mode) => <output aria-label="form value">{mode}</output>}
      </form.Subscribe>
    </I18nProvider>
  );
}

describe("OptionSelectField", () => {
  it("shows the chosen option's name and explanation before it is opened", () => {
    render(<Harness />);

    expect(screen.getByRole("combobox", { name: "Mode" })).toHaveTextContent(
      "Alpha",
    );
    expect(screen.getByRole("combobox")).not.toHaveTextContent("Does alpha.");
    expect(screen.getByRole("combobox")).toHaveAccessibleDescription(
      /Does alpha\./,
    );
  });

  it("lists every option with its explanation when opened", async () => {
    const user = userEvent.setup();
    render(<Harness />);

    await user.click(screen.getByRole("combobox", { name: "Mode" }));
    const listbox = await screen.findByRole("listbox");

    const items = within(listbox).getAllByRole("option");
    expect(items.map((item) => item.textContent)).toEqual([
      "AlphaDoes alpha.",
      "BetaDoes beta.",
      "GammaDoes gamma.",
    ]);
  });

  it("writes the chosen option to the form and shows its explanation", async () => {
    const user = userEvent.setup();
    render(<Harness />);

    await user.click(screen.getByRole("combobox", { name: "Mode" }));
    await user.click(
      within(await screen.findByRole("listbox")).getByRole("option", {
        name: /Beta/,
      }),
    );

    await waitFor(() =>
      expect(screen.queryByRole("listbox")).not.toBeInTheDocument(),
    );
    expect(screen.getByLabelText("form value")).toHaveTextContent("b");
    expect(screen.getByRole("combobox")).toHaveTextContent("Beta");
    expect(screen.getByRole("combobox")).toHaveAccessibleDescription(
      /Does beta\./,
    );
    expect(screen.getByRole("combobox")).not.toHaveAccessibleDescription(
      /Does alpha\./,
    );
  });

  it("chooses with the arrow keys and Enter", async () => {
    const user = userEvent.setup();
    render(<Harness />);

    screen.getByRole("combobox", { name: "Mode" }).focus();
    await user.keyboard("{ArrowDown}");
    await screen.findByRole("listbox");
    await user.keyboard("{ArrowDown}{Enter}");

    expect(screen.getByLabelText("form value")).toHaveTextContent("b");
  });

  it("refuses an option that is disabled", async () => {
    const user = userEvent.setup();
    render(<Harness disabledKeys={["c"]} />);

    await user.click(screen.getByRole("combobox", { name: "Mode" }));
    await user.click(
      within(await screen.findByRole("listbox")).getByRole("option", {
        name: /Gamma/,
      }),
    );

    expect(screen.getByLabelText("form value")).toHaveTextContent("a");
  });
});
