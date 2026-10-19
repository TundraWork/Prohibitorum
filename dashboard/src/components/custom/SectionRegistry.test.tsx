import { I18nProvider } from "@lingui/react";
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { OutlineRail } from "@/components/custom/OutlineRail";
import { Section } from "@/components/custom/Section";
import { SectionRegistryProvider } from "@/components/custom/SectionRegistry";
import { i18n } from "@/i18n";

beforeEach(() => i18n.activate("en"));

function Page({ dirty = "", only }: { dirty?: string; only?: string }) {
  return (
    <I18nProvider i18n={i18n}>
      <SectionRegistryProvider>
        <OutlineRail facts={<span>fact</span>} />
        <Section title="General" dirty={dirty === "General"}>
          x
        </Section>
        {only === undefined && (
          <>
            <Section title="Disable or delete" tone="danger">
              x
            </Section>
            <Section title="Connection" dirty={dirty === "Connection"}>
              x
            </Section>
          </>
        )}
      </SectionRegistryProvider>
    </I18nProvider>
  );
}

const links = () =>
  within(screen.getByRole("navigation", { name: "On this page" })).getAllByRole(
    "link",
  );

describe("OutlineRail", () => {
  it("lists the sections as in-page links, the danger one last", () => {
    render(<Page />);
    expect(links().map((link) => link.textContent)).toEqual([
      "General",
      "Connection",
      "Disable or delete",
    ]);
    expect(links()[0]?.getAttribute("href")).toBe("#general");
    expect(screen.getByText("fact")).toBeTruthy();
  });

  it("marks a dirty section's link as unsaved", () => {
    const { rerender } = render(<Page />);
    expect(screen.queryByLabelText("Unsaved changes")).toBeNull();
    rerender(<Page dirty="Connection" />);
    const dot = screen.getByLabelText("Unsaved changes");
    expect(links()[1]?.contains(dot)).toBe(true);
  });

  describe("current section", () => {
    const page = document.documentElement;
    const metrics = { height: 0, client: 0, top: 0 };

    /** Puts each section at a viewport top, and the page at a scroll position. */
    function layout(tops: Record<string, number>) {
      for (const [id, top] of Object.entries(tops)) {
        const element = document.getElementById(id);
        if (element === null) throw new Error(`no section ${id}`);
        vi.spyOn(element, "getBoundingClientRect").mockReturnValue({
          top,
        } as DOMRect);
      }
    }
    function scrollPage(top: number) {
      metrics.top = top;
      act(() => {
        fireEvent.scroll(document);
      });
    }

    beforeEach(() => {
      metrics.height = 1200;
      metrics.client = 800;
      metrics.top = 0;
      Object.defineProperty(page, "scrollHeight", {
        configurable: true,
        get: () => metrics.height,
      });
      Object.defineProperty(page, "clientHeight", {
        configurable: true,
        get: () => metrics.client,
      });
      Object.defineProperty(page, "scrollTop", {
        configurable: true,
        get: () => metrics.top,
      });
      vi.stubGlobal("innerHeight", 800);
    });
    afterEach(() => {
      vi.restoreAllMocks();
      vi.unstubAllGlobals();
      for (const name of ["scrollHeight", "clientHeight", "scrollTop"]) {
        Reflect.deleteProperty(page, name);
      }
    });

    const current = () =>
      links()
        .filter((link) => link.getAttribute("aria-current") === "location")
        .map((link) => link.textContent);

    it("follows the sections under the header while the page scrolls", () => {
      render(<Page />);
      layout({ general: 100, connection: 500, "disable-or-delete": 900 });
      scrollPage(100);
      expect(current()).toEqual(["General"]);
    });

    it("reaches the last sections when the page cannot scroll them to the header", () => {
      render(<Page />);
      // The last two sections stay below the header even at the very bottom.
      layout({ general: -500, connection: 300, "disable-or-delete": 600 });
      scrollPage(0);
      expect(current()).toEqual(["General"]);
      scrollPage(400);
      expect(current()).toEqual(["Disable or delete"]);
    });

    it("makes a clicked entry current at once and keeps it until the reader scrolls", async () => {
      render(<Page />);
      layout({ general: 100, connection: 500, "disable-or-delete": 900 });
      scrollPage(0);
      expect(current()).toEqual(["General"]);
      await userEvent.click(screen.getByRole("link", { name: "Connection" }));
      expect(current()).toEqual(["Connection"]);
      // The jump's own scroll does not undo it.
      scrollPage(50);
      expect(current()).toEqual(["Connection"]);
      act(() => {
        fireEvent.wheel(window);
      });
      expect(current()).toEqual(["General"]);
    });

    it("releases a clicked entry on a pointer press, as a scrollbar drag starts", async () => {
      render(<Page />);
      layout({ general: 100, connection: 500, "disable-or-delete": 900 });
      scrollPage(0);
      await userEvent.click(screen.getByRole("link", { name: "Connection" }));
      expect(current()).toEqual(["Connection"]);
      act(() => {
        fireEvent.pointerDown(window);
      });
      expect(current()).toEqual(["General"]);
    });

    it("forgets a clicked entry when the page's sections change", async () => {
      const { rerender } = render(<Page />);
      layout({ general: 100, connection: 500, "disable-or-delete": 900 });
      scrollPage(0);
      await userEvent.click(screen.getByRole("link", { name: "Connection" }));
      expect(current()).toEqual(["Connection"]);
      rerender(<Page only="General" />);
      layout({ general: 100 });
      scrollPage(0);
      expect(current()).toEqual(["General"]);
    });
  });
});
