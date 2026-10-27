import { I18nProvider } from "@lingui/react";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { RouteLayoutContext } from "@/components/custom/RouteFeedback";
import { RoutePending } from "@/components/custom/RoutePending";
import { i18n } from "@/i18n";

const loading = vi.hoisted(() => ({ routeIds: [] as string[] }));

vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  useRouterState: ({
    select,
  }: {
    select: (state: { matches: { routeId: string }[] }) => unknown;
  }) => select({ matches: loading.routeIds.map((routeId) => ({ routeId })) }),
}));

function mount(layout: "console" | "public" | null, routeIds: string[] = []) {
  loading.routeIds = routeIds;
  const wrap = (children: ReactNode) => (
    <I18nProvider i18n={i18n}>
      <RouteLayoutContext value={layout}>{children}</RouteLayoutContext>
    </I18nProvider>
  );
  return render(wrap(<RoutePending />));
}

describe("RoutePending", () => {
  beforeEach(() => {
    i18n.activate("en");
  });

  it("says the page is loading, and hides the bars from assistive technology", () => {
    mount("public");
    expect(screen.getByRole("status")).toHaveTextContent("Loading");
    const bars = document.querySelectorAll('[data-slot="skeleton"]');
    expect(bars.length).toBeGreaterThan(0);
    for (const bar of bars) {
      expect(bar.closest('[aria-hidden="true"]')).not.toBeNull();
    }
  });

  it("draws only a step inside the public layout, which already has its stage", () => {
    mount("public", ["__root__", "/_public", "/_public/consent"]);
    expect(document.querySelector('[data-slot="stage"]')).toBeNull();
    expect(document.querySelector('[data-slot="sidebar"]')).toBeNull();
  });

  it("draws a page header and sections inside the console, which already has its shell", () => {
    mount("console", ["__root__", "/_protected", "/_protected/profile"]);
    expect(document.querySelector('[data-slot="page-body"]')).not.toBeNull();
    expect(document.querySelector('[data-slot="sidebar"]')).toBeNull();
  });

  it("draws the console shell before any layout, while a console route loads", () => {
    mount(null, ["__root__", "/_protected", "/_protected/profile"]);
    expect(document.querySelector('[data-slot="sidebar"]')).not.toBeNull();
    expect(document.querySelector('[data-slot="page-body"]')).not.toBeNull();
    expect(document.querySelector('[data-slot="stage"]')).toBeNull();
  });

  it("draws the public toolbar and stage before any layout, while a public route loads", () => {
    mount(null, ["__root__", "/_public", "/_public/consent"]);
    expect(document.querySelector('[data-slot="stage"]')).not.toBeNull();
    expect(document.querySelector('[data-slot="sidebar"]')).toBeNull();
  });

  it("draws the launcher's pad before any layout, while the launcher loads", () => {
    mount(null, ["__root__", "/_launcher", "/_launcher/"]);
    expect(document.querySelector('[data-slot="stage"]')).toBeNull();
    expect(document.querySelector('[data-slot="sidebar"]')).toBeNull();
    expect(document.querySelectorAll("li").length).toBeGreaterThan(0);
  });
});
