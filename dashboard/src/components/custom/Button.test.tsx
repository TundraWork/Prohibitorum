import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Button } from "@/components/custom/Button";

const spinner = () => document.querySelector('[data-slot="spinner"]');

describe("Button", () => {
  it("draws a spinner while pending", () => {
    render(<Button isPending>Save</Button>);
    expect(spinner()).not.toBeNull();
  });

  it("draws no spinner while idle", () => {
    render(<Button>Save</Button>);
    expect(spinner()).toBeNull();
  });

  it("keeps the visible label as the accessible name while pending", () => {
    render(<Button isPending>Save</Button>);
    expect(screen.getByRole("button", { name: "Save" })).toBeVisible();
  });

  it("replaces the icon of an icon-only button while pending", () => {
    render(
      <Button isPending isIconOnly aria-label="Sign out">
        <svg data-testid="icon" />
      </Button>,
    );
    expect(spinner()).not.toBeNull();
    expect(screen.queryByTestId("icon")).toBeNull();
    expect(screen.getByRole("button", { name: "Sign out" })).toBeVisible();
  });

  it("passes the pending flag to a caller that words its own label", () => {
    render(
      <Button isPending>
        {({ isPending }) => (isPending ? "Submitting…" : "Save")}
      </Button>,
    );
    expect(spinner()).not.toBeNull();
    expect(screen.getByRole("button", { name: "Submitting…" })).toBeVisible();
  });
});
