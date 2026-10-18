import { render } from "@testing-library/react";
import { useRef } from "react";
import { describe, expect, it } from "vitest";
import { useHostOverlayContainer } from "@/components/custom/host-overlay-container";

let seen: HTMLElement | undefined;
function Probe() {
  const ref = useRef<HTMLDivElement>(null);
  seen = useHostOverlayContainer(ref);
  return <div ref={ref} />;
}

describe("useHostOverlayContainer", () => {
  it("returns the hosting dialog element", () => {
    const { getByRole } = render(
      <div role="dialog" aria-label="host">
        <Probe />
      </div>,
    );
    expect(seen).toBe(getByRole("dialog"));
  });

  it("returns undefined outside any dialog", () => {
    render(<Probe />);
    expect(seen).toBeUndefined();
  });
});
