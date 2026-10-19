import { I18nProvider } from "@lingui/react";
import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Section } from "@/components/custom/Section";
import { SectionRegistryProvider } from "@/components/custom/SectionRegistry";
import { UnsavedBar } from "@/components/custom/UnsavedBar";
import { i18n } from "@/i18n";

let callback: IntersectionObserverCallback;
let options: IntersectionObserverInit | undefined;
const observed = new Set<Element>();

beforeEach(() => {
  i18n.activate("en");
  observed.clear();
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(
        cb: IntersectionObserverCallback,
        init?: IntersectionObserverInit,
      ) {
        callback = cb;
        options = init;
      }
      observe(el: Element) {
        observed.add(el);
      }
      unobserve(el: Element) {
        observed.delete(el);
      }
      disconnect() {
        observed.clear();
      }
    },
  );
  Element.prototype.scrollIntoView = vi.fn();
});
afterEach(() => vi.unstubAllGlobals());

function report(target: Element, isIntersecting: boolean) {
  act(() =>
    callback(
      [{ target, isIntersecting } as IntersectionObserverEntry],
      {} as IntersectionObserver,
    ),
  );
}

function Page({ dirty, saveIn }: { dirty: boolean; saveIn?: string }) {
  return (
    <I18nProvider i18n={i18n}>
      <SectionRegistryProvider>
        <UnsavedBar saveIn={saveIn} />
        <Section title="Accounts and claims" dirty={dirty}>
          x
        </Section>
        <Section title="Where Save is">y</Section>
      </SectionRegistryProvider>
    </I18nProvider>
  );
}

describe("UnsavedBar", () => {
  it("is hidden when nothing is dirty", () => {
    render(<Page dirty={false} />);
    expect(screen.queryByText(/Unsaved changes in/)).toBeNull();
  });

  it("shows while the dirty section is off screen and hides when it is back", () => {
    render(<Page dirty />);
    const section = document.getElementById("accounts-and-claims") as Element;
    expect(observed.has(section)).toBe(true);

    report(section, false);
    expect(
      screen.getByText("Unsaved changes in Accounts and claims"),
    ).toBeTruthy();

    report(section, true);
    expect(screen.queryByText(/Unsaved changes in/)).toBeNull();
  });

  it("keeps clear of the sticky header with a margin the browser accepts", () => {
    render(<Page dirty />);
    // A real observer throws on anything but pixels or percent.
    expect(options?.rootMargin).toMatch(/^(-?\d+(px|%)\s?)+$/);
  });

  it("jumps to the section and focuses its heading", async () => {
    render(<Page dirty />);
    const section = document.getElementById("accounts-and-claims") as Element;
    report(section, false);

    await userEvent.click(screen.getByRole("button", { name: "Jump to it" }));

    expect(section.scrollIntoView).toHaveBeenCalled();
    expect(document.activeElement).toBe(
      screen.getByRole("heading", { name: "Accounts and claims" }),
    );
  });

  it("follows the section that holds Save when one is named", async () => {
    render(<Page dirty saveIn="where-save-is" />);
    const edited = document.getElementById("accounts-and-claims") as Element;
    const save = document.getElementById("where-save-is") as Element;
    expect(observed.has(edited)).toBe(false);
    expect(observed.has(save)).toBe(true);

    // The edited block being out of view is not enough: Save is what matters.
    report(save, true);
    expect(screen.queryByText("You have unsaved changes.")).toBeNull();

    report(save, false);
    expect(screen.getByText("You have unsaved changes.")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Jump to Save" }));
    expect(save.scrollIntoView).toHaveBeenCalled();
  });
});
