import { I18nProvider } from "@lingui/react";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it } from "vitest";
import { CancelLink } from "@/components/custom/CancelLink";
import { i18n } from "@/i18n";

beforeEach(() => {
  i18n.activate("en");
});

async function mount(pending: boolean) {
  const root = createRootRoute({
    component: () => <CancelLink to="/admin/groups" pending={pending} />,
  });
  const list = createRoute({
    getParentRoute: () => root,
    path: "/admin/groups",
    component: () => <p>The list</p>,
  });
  const router = createRouter({
    routeTree: root.addChildren([list]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  render(
    <I18nProvider i18n={i18n}>
      <RouterProvider router={router} />
    </I18nProvider>,
  );
  await screen.findByText("Cancel");
  return router;
}

describe("the create page's cancel", () => {
  it("is a link to the list that goes there", async () => {
    const user = userEvent.setup();
    const router = await mount(false);

    const link = screen.getByRole("link", { name: "Cancel" });
    expect(link).toHaveAttribute("href", "/admin/groups");
    expect(link).not.toHaveAttribute("aria-disabled");
    await user.click(link);

    await waitFor(() =>
      expect(router.state.location.pathname).toBe("/admin/groups"),
    );
  });

  it("stays the same link while pending, held and going nowhere", async () => {
    const user = userEvent.setup();
    const router = await mount(true);

    const link = screen.getByRole("link", { name: "Cancel" });
    expect(link).toHaveAttribute("aria-disabled", "true");
    expect(link).toHaveAttribute("tabindex", "-1");
    await user.click(link);

    expect(router.state.location.pathname).toBe("/");
  });
});
