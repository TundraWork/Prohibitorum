import { afterEach, describe, expect, it, vi } from "vitest";
import { describeDock, installDevtoolsDock } from "@/devtools/dock/dock";

/** A stand-in for the shell's panel: the testid and `data-open` it publishes. */
function panel(top: number, height: number, open = true): HTMLElement {
  const element = document.createElement("div");
  element.dataset.testid = "tanstack-devtools-panel";
  if (open) {
    element.dataset.open = "true";
  }
  element.getBoundingClientRect = () => new DOMRect(0, top, 800, height);
  document.body.append(element);
  return element;
}

const watching: Array<() => void> = [];

function watch(): () => void {
  const stop = installDevtoolsDock();
  watching.push(stop);
  return stop;
}

function property(name: string): string {
  return document.documentElement.style.getPropertyValue(name);
}

afterEach(() => {
  for (const stop of watching.splice(0)) {
    stop();
  }
  document.body.replaceChildren();
});

describe("devtools dock", () => {
  it("anchors a panel to the edge its centre is nearer", () => {
    expect(describeDock({ top: 500, height: 268 }, 768)).toEqual({
      location: "bottom",
      height: 268,
    });
    expect(describeDock({ top: 0, height: 268 }, 768)).toEqual({
      location: "top",
      height: 268,
    });
  });

  it("reports whole pixels, and no dock for a panel that takes no room", () => {
    expect(describeDock({ top: 500, height: 267.6 }, 768)?.height).toBe(268);
    expect(describeDock({ top: 500, height: 0 }, 768)).toBeNull();
    expect(describeDock({ top: 0, height: 268 }, 0)).toBeNull();
  });

  it("makes room for an open panel at the bottom of the page", () => {
    panel(768 - 268, 268);
    watch();

    expect(property("--app-viewport-height")).toBe("calc(100dvh - 268px)");
    expect(property("--app-gutter-bottom")).toBe("268px");
    expect(property("--app-gutter-top")).toBe("0px");
    expect(property("--app-sticky-offset")).toBe("0px");
  });

  it("pushes the page down for a panel at the top of the page", () => {
    panel(0, 268);
    watch();

    expect(property("--app-viewport-height")).toBe("calc(100dvh - 268px)");
    expect(property("--app-gutter-top")).toBe("268px");
    expect(property("--app-gutter-bottom")).toBe("0px");
    expect(property("--app-sticky-offset")).toBe("268px");
  });

  it("restores the full-size page when the panel closes, and when the watch stops", async () => {
    const element = panel(768 - 268, 268);
    const stop = watch();
    expect(property("--app-gutter-bottom")).toBe("268px");

    element.dataset.open = "false";
    await vi.waitFor(() => expect(property("--app-gutter-bottom")).toBe(""));

    element.dataset.open = "true";
    await vi.waitFor(() =>
      expect(property("--app-gutter-bottom")).toBe("268px"),
    );

    stop();
    expect(property("--app-viewport-height")).toBe("");
  });

  it("leaves a replaced watch unable to shrink a page it no longer owns", () => {
    panel(768 - 268, 268);
    const first = watch();
    watch();

    first();

    expect(property("--app-gutter-bottom")).toBe("268px");
  });
});
