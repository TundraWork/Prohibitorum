import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { Button } from "@/components/custom/Button";
import { PublicStep } from "@/components/custom/PublicStep";

it("moves focus to the title, which stays out of the Tab order", () => {
  render(<PublicStep title="Allow Wiki?" />);
  const heading = screen.getByRole("heading", {
    level: 1,
    name: "Allow Wiki?",
  });
  expect(heading).toHaveFocus();
  expect(heading).toHaveAttribute("tabindex", "-1");
});

it("takes focus again when the title's key changes", () => {
  const view = render(
    <>
      <button type="button">Elsewhere</button>
      <PublicStep title="Sign in" titleKey="password" />
    </>,
  );
  screen.getByRole("button", { name: "Elsewhere" }).focus();
  view.rerender(
    <>
      <button type="button">Elsewhere</button>
      <PublicStep title="Signing in as alice" titleKey="totp" />
    </>,
  );
  expect(
    screen.getByRole("heading", { name: "Signing in as alice" }),
  ).toHaveFocus();
});

it("names its back button", async () => {
  const onPress = vi.fn();
  render(
    <PublicStep
      title="Signing in"
      back={{ label: "Back to password", onPress }}
    />,
  );
  await userEvent.click(
    screen.getByRole("button", { name: "Back to password" }),
  );
  expect(onPress).toHaveBeenCalledOnce();
});

it("puts the answer that goes ahead on the right of a decision", () => {
  render(
    <PublicStep
      title="Allow Wiki?"
      actions={
        <PublicStep.Actions layout="decision">
          <Button>Deny</Button>
          <Button>Allow</Button>
        </PublicStep.Actions>
      }
    />,
  );
  const [deny, allow] = screen.getAllByRole("button");
  expect(deny).toHaveAccessibleName("Deny");
  expect(allow).toHaveAccessibleName("Allow");
  expect(allow?.parentElement).toHaveClass("grid", "grid-cols-2");
});
