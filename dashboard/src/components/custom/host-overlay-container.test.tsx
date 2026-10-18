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
      <div role="dialog" data-rac="" aria-label="host">
        <Probe />
      </div>,
    );
    expect(seen).toBe(getByRole("dialog"));
  });

  it("ignores a dialog that is not a react-aria overlay", () => {
    render(
      <div role="dialog" aria-label="coss">
        <Probe />
      </div>,
    );
    expect(seen).toBeUndefined();
  });

  it("returns undefined outside any dialog", () => {
    render(<Probe />);
    expect(seen).toBeUndefined();
  });
});
