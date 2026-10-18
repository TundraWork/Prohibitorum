import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { PageBody } from "@/components/custom/PageBody";

describe("PageBody", () => {
  it("wraps its children in a page-body column", () => {
    render(
      <PageBody>
        <p>Inside</p>
      </PageBody>,
    );

    const body = screen.getByText("Inside").parentElement as HTMLElement;
    expect(body).toHaveAttribute("data-slot", "page-body");
    expect(body).toContainElement(screen.getByText("Inside"));
  });
});
