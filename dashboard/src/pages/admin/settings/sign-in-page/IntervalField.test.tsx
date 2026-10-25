import { I18nProvider } from "@lingui/react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/i18n";
import { IntervalField } from "@/pages/admin/settings/sign-in-page/IntervalField";

beforeEach(() => {
  i18n.activate("en");
});

function Harness({
  initial,
  errors = [],
  onChange = () => undefined,
}: {
  initial: number;
  errors?: unknown[];
  onChange?: (seconds: number) => void;
}) {
  const [value, setValue] = useState(initial);
  return (
    <I18nProvider i18n={i18n}>
      <IntervalField
        value={value}
        errors={errors}
        disabled={false}
        onChange={(next) => {
          setValue(next);
          onChange(next);
        }}
      />
    </I18nProvider>
  );
}

describe("the carousel's interval", () => {
  it("reads in seconds, grouped as the language groups numbers", () => {
    render(<Harness initial={3600} />);
    expect(screen.getByRole("textbox", { name: "Change every" })).toHaveValue(
      "3,600 seconds",
    );
  });

  it("writes the interval as 10秒 in Chinese", () => {
    i18n.activate("zh");
    render(<Harness initial={10} />);
    expect(screen.getByRole("textbox", { name: "切换间隔" })).toHaveValue(
      "10秒",
    );
  });

  it("takes a typed number of seconds", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<Harness initial={10} onChange={onChange} />);
    const field = screen.getByRole("textbox", { name: "Change every" });
    await user.clear(field);
    await user.type(field, "25");
    await user.tab();
    expect(onChange).toHaveBeenLastCalledWith(25);
  });

  it("steps by five seconds", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<Harness initial={10} onChange={onChange} />);
    await user.click(screen.getByRole("button", { name: "Increase" }));
    expect(onChange).toHaveBeenLastCalledWith(15);
    await user.click(screen.getByRole("button", { name: "Decrease" }));
    expect(onChange).toHaveBeenLastCalledWith(10);
  });

  it("hands over a cleared field as not a number, for the form to refuse", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<Harness initial={10} onChange={onChange} />);
    await user.clear(screen.getByRole("textbox", { name: "Change every" }));
    await user.tab();
    expect(onChange).toHaveBeenLastCalledWith(Number.NaN);
  });

  it("shows the form's error and marks the field invalid", () => {
    render(
      <Harness
        initial={10}
        errors={[
          {
            id: "settings.sign-in.interval.range",
            message: "Enter a value between 5 and 3600 seconds.",
          },
        ]}
      />,
    );
    expect(
      screen.getByText("Enter a value between 5 and 3600 seconds."),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("textbox", { name: "Change every" }),
    ).toHaveAttribute("aria-invalid", "true");
  });
});
