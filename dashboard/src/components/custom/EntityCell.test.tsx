import { I18nProvider } from "@lingui/react";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { EntityCell } from "@/components/custom/EntityCell";
import { i18n } from "@/i18n";

beforeEach(() => {
  i18n.activate("en");
});

function renderCell(cell: React.ReactElement) {
  const root = createRootRoute();
  const list = createRoute({
    getParentRoute: () => root,
    path: "/",
    component: () => cell,
  });
  const detail = createRoute({
    getParentRoute: () => root,
    path: "/things/$id",
    component: () => <p>Detail page</p>,
  });
  const router = createRouter({
    routeTree: root.addChildren([list, detail]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  render(
    <I18nProvider i18n={i18n}>
      <RouterProvider router={router} />
    </I18nProvider>,
  );
  return router;
}

describe("EntityCell", () => {
  it("links the name with a real href and navigates inside the app", async () => {
    const router = renderCell(
      <EntityCell
        name="Wiki"
        link={{ to: "/things/$id", params: { id: "a b" } } as never}
      />,
    );
    const anchor = await screen.findByRole("link", { name: "Wiki" });
    expect(anchor.getAttribute("href")).toBe("/things/a%20b");

    // The router takes the click over; a plain anchor would leave it to the
    // browser, which reloads the page.
    expect(fireEvent.click(anchor)).toBe(false);
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/things/a b");
    });
    expect(await screen.findByText("Detail page")).toBeTruthy();
  });

  it("renders a plain name when there is no link", async () => {
    renderCell(<EntityCell name="Wiki" />);
    await screen.findByText("Wiki");
    expect(screen.queryByRole("link")).toBeNull();
  });

  it("draws the marks it is given after the name, beside the disabled badge", async () => {
    renderCell(
      <EntityCell
        name="Wiki"
        dimmed
        marks={<span data-testid="mark">Sign-in off</span>}
      />,
    );
    const mark = await screen.findByTestId("mark");
    expect(mark).toHaveTextContent("Sign-in off");
    expect(screen.getByText("Disabled")).toBeVisible();
  });

  it("keeps a long name whole in its title", async () => {
    const long = "A very long provider name that cannot fit beside its badges";
    renderCell(
      <EntityCell
        name={long}
        dimmed
        link={{ to: "/things/$id", params: { id: "x" } } as never}
      />,
    );
    const link = await screen.findByRole("link", { name: long });
    const nameCell = link.parentElement;
    expect(nameCell).toHaveAttribute("title", long);
    // The badge shares the name's row rather than sitting in a row of its own.
    expect(nameCell?.parentElement).toContainElement(
      screen.getByText("Disabled"),
    );
  });

  it("draws a person's avatar round and an application's as a tile", async () => {
    renderCell(
      <>
        <EntityCell name="Alice" shape="round" />
        <EntityCell name="Grafana" />
      </>,
    );
    const alice = (await screen.findByText("A")).closest(
      '[data-slot="avatar"]',
    );
    const grafana = screen.getByText("G").closest('[data-slot="avatar"]');
    expect(alice?.className).toContain("rounded-full");
    expect(grafana?.className).not.toContain("rounded-full");
  });
});
