import { I18nProvider } from "@lingui/react";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { LoginSurface } from "@/api/raw-paths";
import { i18n } from "@/i18n";
import {
  PlacementControls,
  SurfaceFields,
} from "@/pages/admin/settings/sign-in-page/SurfaceControls";

beforeEach(() => {
  i18n.activate("en");
});

/**
 * The slider's thumb is hidden until the browser has laid it out, which jsdom
 * never does, and a hidden element has no computed name, so the name is read
 * from its label.
 */
function opacity() {
  const slider = screen.getByRole("slider", { hidden: true });
  expect(slider).toHaveAttribute("aria-label", "Opacity");
  return slider;
}

function Surface({
  initial,
  onChange = () => undefined,
  disabled = false,
}: {
  initial: LoginSurface;
  onChange?: (surface: LoginSurface) => void;
  disabled?: boolean;
}) {
  const [surface, setSurface] = useState(initial);
  return (
    <I18nProvider i18n={i18n}>
      <SurfaceFields
        surface={surface}
        disabled={disabled}
        onChange={(next) => {
          setSurface(next);
          onChange(next);
        }}
      />
    </I18nProvider>
  );
}

describe("the card's place and theme", () => {
  it("offers the three places and the three themes as named choices", async () => {
    const user = userEvent.setup();
    const onPosition = vi.fn();
    const onTheme = vi.fn();
    render(
      <I18nProvider i18n={i18n}>
        <PlacementControls
          position="center"
          theme="switchable"
          disabled={false}
          onPosition={onPosition}
          onTheme={onTheme}
        />
      </I18nProvider>,
    );

    const position = screen.getByRole("radiogroup", { name: "Position" });
    expect(screen.getByRole("radio", { name: "Center" })).toBeChecked();
    await user.click(screen.getByRole("radio", { name: "Left" }));
    expect(onPosition).toHaveBeenCalledWith("left");
    await user.click(screen.getByRole("radio", { name: "Right" }));
    expect(onPosition).toHaveBeenCalledWith("right");
    expect(position).toBeVisible();

    expect(screen.getByRole("radiogroup", { name: "Theme" })).toBeVisible();
    expect(
      screen.getByRole("radio", { name: "Visitor's choice" }),
    ).toBeChecked();
    await user.click(screen.getByRole("radio", { name: "Always dark" }));
    expect(onTheme).toHaveBeenCalledWith("dark");
    await user.click(screen.getByRole("radio", { name: "Always light" }));
    expect(onTheme).toHaveBeenCalledWith("light");
  });

  it("holds every choice while disabled", async () => {
    const user = userEvent.setup();
    const onPosition = vi.fn();
    render(
      <I18nProvider i18n={i18n}>
        <PlacementControls
          position="center"
          theme="switchable"
          disabled
          onPosition={onPosition}
          onTheme={vi.fn()}
        />
      </I18nProvider>,
    );
    await user.click(screen.getByRole("radio", { name: "Left" }));
    expect(onPosition).not.toHaveBeenCalled();
  });
});

describe("a translucent surface", () => {
  it("shows opacity and frosted glass only while translucent, and keeps their values", async () => {
    const user = userEvent.setup();
    render(
      <Surface initial={{ translucent: false, opacity: 35, blur: false }} />,
    );
    expect(
      screen.queryByRole("slider", { hidden: true }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("switch", { name: "Frosted glass" }),
    ).not.toBeInTheDocument();

    await user.click(screen.getByRole("switch", { name: "Translucent" }));
    expect(opacity()).toHaveValue("35");
    expect(
      screen.getByRole("switch", { name: "Frosted glass" }),
    ).not.toBeChecked();

    await user.click(screen.getByRole("switch", { name: "Translucent" }));
    expect(
      screen.queryByRole("slider", { hidden: true }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("switch", { name: "Translucent" }));
    expect(opacity()).toHaveValue("35");
  });

  it("shows the opacity as a percentage beside its name", () => {
    render(
      <Surface initial={{ translucent: true, opacity: 80, blur: true }} />,
    );
    expect(screen.getByText("80%")).toBeInTheDocument();
  });

  it("changes the opacity in steps of five, keeping the rest", () => {
    const onChange = vi.fn();
    render(
      <Surface
        initial={{ translucent: true, opacity: 80, blur: true }}
        onChange={onChange}
      />,
    );
    const slider = opacity();
    fireEvent.keyDown(slider, { key: "ArrowLeft" });
    expect(onChange).toHaveBeenLastCalledWith({
      translucent: true,
      opacity: 75,
      blur: true,
    });
    expect(screen.getByText("75%")).toBeInTheDocument();
  });

  it("turns frosted glass on and off", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <Surface
        initial={{ translucent: true, opacity: 70, blur: true }}
        onChange={onChange}
      />,
    );
    await user.click(screen.getByRole("switch", { name: "Frosted glass" }));
    expect(onChange).toHaveBeenLastCalledWith({
      translucent: true,
      opacity: 70,
      blur: false,
    });
  });

  it("holds its controls while disabled", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <Surface
        disabled
        initial={{ translucent: true, opacity: 70, blur: true }}
        onChange={onChange}
      />,
    );
    await user.click(screen.getByRole("switch", { name: "Frosted glass" }));
    expect(onChange).not.toHaveBeenCalled();
    expect(opacity()).toBeDisabled();
  });
});
