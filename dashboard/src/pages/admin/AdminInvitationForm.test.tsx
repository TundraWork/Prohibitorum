import { I18nProvider } from "@lingui/react";
import type { QueryClient } from "@tanstack/react-query";
import { QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { I18nProvider as AriaI18nProvider } from "react-aria";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { searchSerialization } from "@/app/search-params";

// OverlayScrollbars measures computed styles jsdom cannot resolve; the list's
// scroll area is layout only, so a plain box stands in for it.
vi.mock("@/components/custom/ScrollArea", () => ({
  scrollAreaTheme: "os-theme-app",
  ScrollArea: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

import type { components } from "@/api/generated/schema";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { AdminInvitationForm } from "@/pages/admin/AdminInvitationForm";
import { apiError, type FakeApi, fakeApi } from "@/test/app";

type ProviderView = components["schemas"]["IdentityProviderView"];

let queryClient: QueryClient;
let api: FakeApi;

beforeEach(() => {
  i18n.activate("en");
});

function provider(
  slug: string,
  overrides: Partial<ProviderView> = {},
): ProviderView {
  return {
    slug,
    displayName: slug.toUpperCase(),
    protocol: "oidc",
    allowSignIn: true,
    pendingInvitationCount: 0,
    mode: "invite_only",
    disabled: false,
    ready: true,
    secretConfigured: true,
    secretStatus: "valid",
    secretValidatedAt: null,
    createdAt: "2026-01-01T00:00:00Z",
    config: {},
    supportsOperator: false,
    searchFields: [],
    linkedAccountCount: 0,
    ...overrides,
  };
}

async function mount() {
  queryClient = createQueryClient(vi.fn());
  // A successful create swaps the form for the secret reveal, whose leave
  // guard is the router's; the form needs one mounted.
  const root = createRootRoute({ component: Outlet });
  const page = createRoute({
    getParentRoute: () => root,
    path: "/",
    component: AdminInvitationForm,
  });
  const router = createRouter({
    ...searchSerialization,
    routeTree: root.addChildren([page]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  render(
    <I18nProvider i18n={i18n}>
      <AriaI18nProvider>
        <QueryClientProvider client={queryClient}>
          <RouterProvider router={router} />
        </QueryClientProvider>
      </AriaI18nProvider>
    </I18nProvider>,
  );
  // The router commits its first route asynchronously.
  await screen.findByRole("form", { name: "New invitation" });
}

/** The provider picker is a combobox named by its label. */
async function pickProvider(
  user: ReturnType<typeof userEvent.setup>,
  label: string,
) {
  await user.click(screen.getByRole("combobox", { name: /Upstream provider/ }));
  const listbox = await screen.findByRole("listbox");
  await user.click(within(listbox).getByText(label));
}

describe("the invitation form's local sign-in requirement", () => {
  it("locks the switch on, with its enforced copy, for a sign-in-off provider", async () => {
    const user = userEvent.setup();
    api = fakeApi({
      "GET /api/prohibitorum/groups": () => Response.json([]),
      "GET /api/prohibitorum/identity-providers": () =>
        Response.json({
          items: [
            provider("corp"),
            provider("verify-only", { allowSignIn: false }),
            provider("disabled-one", { disabled: true }),
            provider("linkable", { mode: "link_only" }),
            provider("unready", { ready: false }),
          ],
        }),
    });
    await mount();

    const switchEl = screen.getByRole("switch", {
      name: "Require a local sign-in method",
    });
    expect(switchEl).not.toBeChecked();

    await pickProvider(user, "VERIFY-ONLY");
    expect(switchEl).toBeChecked();
    expect(switchEl).toHaveAttribute("aria-disabled", "true");
    expect(
      screen.getByText(/can't be used to sign in, so a local/),
    ).toBeInTheDocument();

    // Switching back to an ordinary provider unlocks it again.
    await pickProvider(user, "CORP");
    expect(switchEl).not.toBeChecked();
    expect(switchEl).not.toHaveAttribute("aria-disabled", "true");
  });

  it("offers only providers that can back an invitation", async () => {
    const user = userEvent.setup();
    api = fakeApi({
      "GET /api/prohibitorum/groups": () => Response.json([]),
      "GET /api/prohibitorum/identity-providers": () =>
        Response.json({
          items: [
            provider("corp"),
            provider("verify-only", { allowSignIn: false }),
            provider("disabled-one", { disabled: true }),
            provider("linkable", { mode: "link_only" }),
            provider("unready", { ready: false }),
          ],
        }),
    });
    await mount();

    await user.click(
      screen.getByRole("combobox", { name: /Upstream provider/ }),
    );
    const listbox = await screen.findByRole("listbox");
    // Disabled, unready and link_only rows never appear; a sign-in-off
    // provider is offered because an invitation can still be verified with it.
    expect(within(listbox).getByText("CORP")).toBeInTheDocument();
    expect(within(listbox).getByText("VERIFY-ONLY")).toBeInTheDocument();
    expect(within(listbox).queryByText("DISABLED-ONE")).not.toBeInTheDocument();
    expect(within(listbox).queryByText("LINKABLE")).not.toBeInTheDocument();
    expect(within(listbox).queryByText("UNREADY")).not.toBeInTheDocument();
  });

  it("sends requireLocalCredential as the switch left it", async () => {
    const user = userEvent.setup();
    api = fakeApi({
      "GET /api/prohibitorum/groups": () => Response.json([]),
      "GET /api/prohibitorum/identity-providers": () =>
        Response.json({ items: [provider("corp")] }),
      "POST /api/prohibitorum/invitations": () =>
        Response.json({
          url: "https://test/enroll/tok",
          expiresAt: "2026-10-24T00:00:00Z",
          groupIds: [],
          requireLocalCredential: true,
        }),
    });
    await mount();

    await pickProvider(user, "CORP");
    await user.click(
      screen.getByRole("switch", { name: "Require a local sign-in method" }),
    );
    await user.click(screen.getByRole("button", { name: "Create invitation" }));

    await waitFor(() => {
      expect(api.sent("POST", "/api/prohibitorum/invitations")).toHaveLength(1);
    });
    const body = api.sent("POST", "/api/prohibitorum/invitations")[0]?.body as {
      requireLocalCredential?: boolean;
    };
    expect(body.requireLocalCredential).toBe(true);
  });

  it("puts a link_only rejection on the provider field", async () => {
    api = fakeApi({
      "GET /api/prohibitorum/groups": () => Response.json([]),
      "GET /api/prohibitorum/identity-providers": () =>
        Response.json({ items: [provider("corp")] }),
      "POST /api/prohibitorum/invitations": () =>
        apiError("upstream_idp_link_only", 400),
    });
    const user = userEvent.setup();
    await mount();

    await pickProvider(user, "CORP");
    await user.click(screen.getByRole("button", { name: "Create invitation" }));

    // The rejection lands on the provider field: its control goes invalid
    // rather than the form-level banner, which is what points the operator
    // back at the provider they chose.
    await waitFor(() => {
      expect(
        screen.getByRole("combobox", { name: /Upstream provider/ }),
      ).toHaveAttribute("aria-invalid", "true");
    });
  });
});
