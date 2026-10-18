import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { EntityAvatar } from "@/components/custom/EntityAvatar";

function avatar(container: HTMLElement): HTMLElement {
  const root = container.querySelector<HTMLElement>("[data-slot=avatar]");
  if (!root) throw new Error("no avatar");
  return root;
}

describe("EntityAvatar", () => {
  it("steps through 24, 32, 48 and 56 pixels, 32 by default", () => {
    const sizes = { sm: "size-6", md: "size-8", lg: "size-12", xl: "size-14" };
    for (const [size, className] of Object.entries(sizes)) {
      const { container, unmount } = render(
        <EntityAvatar size={size as keyof typeof sizes} name="Wiki" />,
      );
      expect(avatar(container)).toHaveClass(className);
      unmount();
    }
    const { container } = render(<EntityAvatar name="Wiki" />);
    expect(avatar(container)).toHaveClass("size-8");
  });

  it("falls back to the initials of the first two words of the name", () => {
    const { container } = render(
      <EntityAvatar size="xl" name="University SSO" />,
    );
    expect(avatar(container)).toHaveTextContent("US");
  });

  it("falls back to one letter for a one-word name", () => {
    const { container } = render(<EntityAvatar name="grafana" />);
    expect(avatar(container)).toHaveTextContent("G");
  });

  it("draws a tile for an application and a circle for a person", () => {
    const tile = render(<EntityAvatar name="Wiki" />);
    expect(avatar(tile.container)).toHaveClass("rounded-lg");
    tile.unmount();
    const person = render(<EntityAvatar name="Mei Tanaka" shape="round" />);
    expect(avatar(person.container)).toHaveClass("rounded-full");
  });

  it("prefers an explicit fallback over the name, and shows an icon when it has neither", () => {
    const given = render(<EntityAvatar name="Wiki" fallback="?" />);
    expect(avatar(given.container)).toHaveTextContent("?");
    given.unmount();
    const none = render(<EntityAvatar />);
    expect(avatar(none.container).querySelector("svg")).not.toBeNull();
  });
});
