import { I18nProvider } from "@lingui/react";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/i18n";
import { ColorControl } from "@/pages/admin/settings/sign-in-page/ColorControl";
import { suggestedColors } from "@/pages/admin/settings/sign-in-page/sign-in-page-form";

beforeEach(() => {
  i18n.activate("en");
});

function Harness({
  initial = "#1f6f8b",
  onChange = () => undefined,
  errors = [],
  disabled = false,
}: {
  initial?: string;
  onChange?: (hex: string) => void;
  errors?: unknown[];
  disabled?: boolean;
}) {
  const [value, setValue] = useState(initial);
  return (
    <I18nProvider i18n={i18n}>
      <ColorControl
        value={value}
        errors={errors}
        disabled={disabled}
        onChange={(next) => {
          setValue(next);
          onChange(next);
        }}
      />
    </I18nProvider>
  );
}

describe("the colour control", () => {
  it("shows the colour as hex and offers eight suggestions by name", () => {
    render(<Harness />);
    expect(screen.getByRole("textbox", { name: "Color" })).toHaveValue(
      "#1f6f8b",
    );
    const suggestions = screen.getByRole("radiogroup", {
      name: "Suggested colors",
    });
    expect(suggestions).toBeVisible();
    expect(screen.getAllByRole("radio")).toHaveLength(suggestedColors.length);
    expect(screen.getByRole("radio", { name: "Teal" })).toBeChecked();
  });

  it.each(["en", "zh"] as const)(
    "gives each suggestion its own name for screen readers in %s",
    (locale) => {
      i18n.activate(locale);
      render(<Harness />);
      const names = screen
        .getAllByRole("radio")
        .map((swatch) => swatch.getAttribute("aria-label"));
      expect(names).toHaveLength(8);
      expect(names.every((name) => name && !name.startsWith("#"))).toBe(true);
      expect(new Set(names).size).toBe(8);
    },
  );

  it("takes a suggested colour", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<Harness onChange={onChange} />);
    await user.click(screen.getByRole("radio", { name: "Clay" }));
    expect(onChange).toHaveBeenLastCalledWith("#8a5a44");
    expect(screen.getByRole("textbox", { name: "Color" })).toHaveValue(
      "#8a5a44",
    );
  });

  it("writes a typed colour in lowercase and hands over what was typed even while it is not yet a colour", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<Harness onChange={onChange} />);
    const hex = screen.getByRole("textbox", { name: "Color" });
    await user.clear(hex);
    await user.type(hex, "#A1B2C3");
    expect(onChange).toHaveBeenLastCalledWith("#a1b2c3");
    expect(hex).toHaveValue("#a1b2c3");
    await user.clear(hex);
    await user.type(hex, "#a1");
    expect(onChange).toHaveBeenLastCalledWith("#a1");
  });

  it("accepts a colour typed without its #", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<Harness onChange={onChange} />);
    const hex = screen.getByRole("textbox", { name: "Color" });
    await user.clear(hex);
    await user.type(hex, "A1B2C3");
    expect(onChange).toHaveBeenLastCalledWith("#a1b2c3");
    expect(hex).toHaveValue("#a1b2c3");
  });

  it("shows the hue in degrees beside its name", () => {
    render(<Harness initial="#00ff00" />);
    expect(screen.getByText("120°")).toBeVisible();
  });

  it("shows the form's error under the colour and marks it invalid", () => {
    render(
      <Harness
        initial="#zzz"
        errors={[
          {
            id: "settings.sign-in.color.invalid",
            message: "Enter a color as # followed by six hexadecimal digits.",
          },
        ]}
      />,
    );
    expect(
      screen.getByText(
        "Enter a color as # followed by six hexadecimal digits.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Color" })).toHaveAttribute(
      "aria-invalid",
      "true",
    );
  });

  it("moves only the hue with the hue slider", () => {
    const onChange = vi.fn();
    render(<Harness onChange={onChange} />);
    const hue = screen.getByRole("slider", { hidden: true });
    expect(hue).toHaveAttribute("aria-label", "Hue");
    fireEvent.keyDown(hue, { key: "ArrowRight" });
    const next = onChange.mock.calls.at(-1)?.[0] as string;
    expect(next).toMatch(/^#[0-9a-f]{6}$/);
    expect(next).not.toBe("#1f6f8b");
  });

  it("takes a colour from the browser's own picker", () => {
    const onChange = vi.fn();
    render(<Harness onChange={onChange} />);
    fireEvent.change(screen.getByLabelText("Pick a color"), {
      target: { value: "#336699" },
    });
    expect(onChange).toHaveBeenLastCalledWith("#336699");
  });

  it("holds every way of choosing while disabled", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<Harness disabled onChange={onChange} />);
    await user.click(screen.getByRole("radio", { name: "Clay" }));
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByRole("textbox", { name: "Color" })).toBeDisabled();
    expect(screen.getByLabelText("Pick a color")).toBeDisabled();
  });
});
