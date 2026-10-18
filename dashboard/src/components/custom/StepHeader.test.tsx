import { I18nProvider } from "@lingui/react";
import { render, screen, within } from "@testing-library/react";
import { beforeEach, expect, it } from "vitest";
import { StepHeader } from "@/components/custom/StepHeader";
import { i18n } from "@/i18n";

beforeEach(() => i18n.activate("en"));

function draw(current: number) {
  render(
    <I18nProvider i18n={i18n}>
      <StepHeader
        label="Setup steps"
        steps={["Password", "Authenticator", "Recovery codes"]}
        current={current}
      />
    </I18nProvider>,
  );
  return screen.getByRole("list", { name: "Setup steps" });
}

it("marks the current step and leaves the others unmarked", () => {
  const list = draw(1);
  const items = within(list).getAllByRole("listitem");
  expect(items).toHaveLength(3);
  expect(items[1]).toHaveAttribute("aria-current", "step");
  expect(items[0]).not.toHaveAttribute("aria-current");
  expect(items[2]).not.toHaveAttribute("aria-current");
});

it("numbers the steps still ahead and the current one", () => {
  const items = within(draw(0)).getAllByRole("listitem");
  expect(items[0]).toHaveTextContent("1");
  expect(items[1]).toHaveTextContent("2");
  expect(items[2]).toHaveTextContent("3");
});

it("shows earlier steps as done, without a number", () => {
  const items = within(draw(2)).getAllByRole("listitem");
  expect(items[0]).toHaveTextContent("Completed");
  expect(items[0]).not.toHaveTextContent("1");
  expect(items[1]).toHaveTextContent("Completed");
  expect(items[2]).not.toHaveTextContent("Completed");
  expect(items[2]).toHaveTextContent("3");
});

it("names every step", () => {
  draw(0);
  expect(screen.getByText("Password")).toBeVisible();
  expect(screen.getByText("Authenticator")).toBeVisible();
  expect(screen.getByText("Recovery codes")).toBeVisible();
});
