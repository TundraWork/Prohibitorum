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
});
