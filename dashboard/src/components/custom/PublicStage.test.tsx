import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { PublicStage } from "@/components/custom/PublicStage";
import { PublicStep } from "@/components/custom/PublicStep";

const stage = () => {
  const found = document.querySelector<HTMLElement>('[data-slot="stage"]');
  if (!found) throw new Error("no stage");
  return found;
};
const region = (slot: string) =>
  document.querySelector<HTMLElement>(`[data-slot="${slot}"]`);

it("renders the card body inside the card", () => {
  render(
    <PublicStage frame="frosted">
      <p>Body</p>
    </PublicStage>,
  );
  expect(region("stage-card")).toContainElement(screen.getByText("Body"));
});

it("puts a step's strip and footer around the card, not inside it", () => {
  render(
    <PublicStage frame="frosted">
      <PublicStep
        title="Allow Wiki?"
        strip={<span>Requesting app</span>}
        footer={<span>Remember me</span>}
      >
        <p>Body</p>
      </PublicStep>
    </PublicStage>,
  );
  const strip = region("stage-strip");
  const footer = region("stage-footer");
  const card = region("stage-card");
  expect(strip).toContainElement(screen.getByText("Requesting app"));
  expect(footer).toContainElement(screen.getByText("Remember me"));
  expect(card).not.toContainElement(screen.getByText("Requesting app"));
  expect(card).not.toContainElement(screen.getByText("Remember me"));
  expect(card).toContainElement(screen.getByText("Body"));
  expect(
    (strip as Node).compareDocumentPosition(card as Node) &
      Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
});

it("hides the strip and footer regions while nothing fills them", () => {
  render(
    <PublicStage frame="plain">
      <PublicStep title="Sign in" />
    </PublicStage>,
  );
  for (const slot of ["stage-strip", "stage-footer"]) {
    expect(region(slot)).toBeEmptyDOMElement();
    expect(region(slot)).toHaveClass("empty:hidden");
  }
});

it("takes the wide width while a wide step is mounted and restores it after", () => {
  const view = render(
    <PublicStage frame="frosted">
      <PublicStep title="Allow Wiki?" wide />
    </PublicStage>,
  );
  expect(stage()).toHaveAttribute("data-width", "wide");
  view.rerender(
    <PublicStage frame="frosted">
      <PublicStep title="Sign in" />
    </PublicStage>,
  );
  expect(stage()).toHaveAttribute("data-width", "default");
});

it("reflects its frame and position", () => {
  render(
    <PublicStage frame="plain" position="right">
      <p>Body</p>
    </PublicStage>,
  );
  expect(stage()).toHaveAttribute("data-frame", "plain");
  expect(stage()).toHaveAttribute("data-position", "right");
  expect(stage()).toHaveAttribute("data-width", "default");
});

it("draws the admin's card surface on the card, over the card fill", () => {
  render(
    <PublicStage
      frame="frosted"
      card={{ className: "bg-test-surface", style: { "--x": "1" } as never }}
    >
      <p>Body</p>
    </PublicStage>,
  );
  expect(region("stage-card")).toHaveClass("bg-test-surface");
  expect(region("stage-card")?.style.getPropertyValue("--x")).toBe("1");
});
