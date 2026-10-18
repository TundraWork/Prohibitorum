import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppStrip } from "@/components/custom/AppStrip";

describe("AppStrip", () => {
  it("draws the lead, the name and the host", () => {
    render(<AppStrip lead="Asking for access" name="Wiki" host="wiki.test" />);
    expect(screen.getByText("Asking for access")).toBeInTheDocument();
    expect(screen.getByText("Wiki")).toBeInTheDocument();
    expect(screen.getByText("wiki.test")).toBeInTheDocument();
  });

  it("leaves the host out when there is none", () => {
    render(<AppStrip lead="Lead" name="Wiki" />);
    expect(screen.queryByText("wiki.test")).not.toBeInTheDocument();
  });

  it("shows the initials of the name without an icon", () => {
    render(<AppStrip lead="Lead" name="Northlight Wiki" />);
    expect(screen.getByText("NW")).toBeInTheDocument();
  });

  it("keeps the whole name as text and title when it is long", () => {
    const name =
      "A very long application name that will be truncated on screen";
    render(<AppStrip lead="Lead" name={name} iconUrl="/icon.png" />);
    expect(screen.getByText(name)).toHaveAttribute("title", name);
  });
});
