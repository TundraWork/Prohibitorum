import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import { configureSudo, resetSudo } from "@/api/sudo";
import { createQueryClient } from "@/app/query-client";
import { searchSerialization } from "@/app/search-params";
import { i18n } from "@/i18n";
import { TokensPanel } from "@/pages/security/TokensPanel";

type Session = components["schemas"]["SessionView"];
type App = components["schemas"]["MyForwardAuthApp"];
type Token = components["schemas"]["PersonalAccessTokenView"];

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
let queryClient: QueryClient;

const plaintext = `prohibitorum_pat_${"A".repeat(43)}`;

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(() => undefined);
  configureSudo({
    queryClient,
    set: () => {},
    setFresh: () => {},
    getFresh: () => true,
  });
});

afterEach(() => {
  resetSudo();
  queryClient.clear();
  vi.unstubAllGlobals();
});

function mount({
  apps = [{ clientId: "wiki", displayName: "Wiki" }],
  role = "user",
  tokens = [],
}: {
  apps?: App[];
  role?: Session["role"];
  tokens?: Token[];
} = {}) {
  const session: Session = {
    id: 1,
    username: "alice",
    displayName: "Alice",
    role,
  };
  fetchBoundary.mockImplementation(async (request) => {
    const path = new URL(request.url).pathname;
    if (path.endsWith("/me")) return Response.json(session);
    if (path.endsWith("/me/forward-auth-apps")) return Response.json(apps);
    if (path.endsWith("/me/sudo/methods")) {
      return Response.json({ methods: ["password_totp"], fresh: true });
    }
    if (path.endsWith("/me/tokens") && request.method === "POST") {
      return Response.json({
        token: plaintext,
        pat: {
          id: 1,
          name: "CI",
          tokenHint: "prohibitorum_pat_…AAAA",
          access: "sudo",
          apps: [],
          createdAt: new Date().toISOString(),
        },
      });
    }
    if (path.endsWith("/me/tokens")) return Response.json(tokens);
    return new Response(null, { status: 204 });
  });
  // The reveal guards against leaving with the token unsaved, and that guard
  // is the router's, so the panel needs one mounted.
  const root = createRootRoute({ component: Outlet });
  const security = createRoute({
    getParentRoute: () => root,
    path: "/",
    component: TokensPanel,
  });
  const router = createRouter({
    ...searchSerialization,
    routeTree: root.addChildren([security]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

function creates() {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter(
      (request) =>
        request.method === "POST" &&
        new URL(request.url).pathname.endsWith("/me/tokens"),
    );
}

async function openDialog(user: UserEvent) {
  await user.click(
    await screen.findByRole("button", { name: "Create a token" }),
  );
  return screen.findByRole("dialog", { name: "New access token" });
}

describe("creating an access token", () => {
  it("confirms a sudo token before sending it", async () => {
    const user = userEvent.setup();
    mount();
    const dialog = await openDialog(user);
    await user.type(within(dialog).getByLabelText("Name"), "CI");
    await user.click(
      within(dialog).getByRole("radio", {
        name: "Full access, without confirming it is you",
      }),
    );
    await user.click(
      within(dialog).getByRole("button", { name: "Create token" }),
    );

    const confirm = await screen.findByRole("alertdialog", {
      name: "Create a token that skips confirming it is you?",
    });
    expect(creates()).toHaveLength(0);

    // Cancelling leaves the form as it was, unsent.
    await user.click(within(confirm).getByRole("button", { name: "Cancel" }));
    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(creates()).toHaveLength(0);
    expect(within(dialog).getByLabelText("Name")).toHaveValue("CI");

    await user.click(
      within(dialog).getByRole("button", { name: "Create token" }),
    );
    const again = await screen.findByRole("alertdialog");
    await user.click(
      within(again).getByRole("button", { name: "Create token" }),
    );

    const reveal = await screen.findByRole("dialog", {
      name: "Copy your new access token",
    });
    expect(creates()).toHaveLength(1);
    expect(await creates()[0]?.json()).toEqual({ name: "CI", access: "sudo" });
    expect(within(reveal).getByLabelText("New access token")).toHaveValue(
      plaintext,
    );
    expect(
      within(reveal).getByText("Send it in the X-Prohibitorum-PAT header."),
    ).toBeInTheDocument();
    expect(reveal).toContainElement(document.activeElement as HTMLElement);
  });

  it("marks the application list when none is chosen", async () => {
    const user = userEvent.setup();
    mount();
    const dialog = await openDialog(user);
    await user.click(
      within(dialog).getByRole("radio", { name: "Applications you choose" }),
    );
    await user.click(
      within(dialog).getByRole("button", { name: "Create token" }),
    );

    const group = within(dialog).getByRole("group", { name: "Applications" });
    expect(
      await within(group).findByText(
        "Choose at least one application, or pick another level of access.",
      ),
    ).toBeInTheDocument();
    expect(
      within(group).getByRole("checkbox", { name: "Wiki" }),
    ).toHaveAttribute("aria-invalid", "true");
    // Both fields say what is missing at once, not one per attempt.
    expect(
      within(dialog).getByText(
        "Give the token a name so you can recognise it later.",
      ),
    ).toBeInTheDocument();
    expect(creates()).toHaveLength(0);

    await user.click(within(group).getByRole("checkbox", { name: "Wiki" }));
    expect(
      within(group).queryByText(
        "Choose at least one application, or pick another level of access.",
      ),
    ).not.toBeInTheDocument();
  });

  it("rules out chosen applications when there are none to choose", async () => {
    const user = userEvent.setup();
    mount({ apps: [] });
    const dialog = await openDialog(user);
    const radio = within(dialog).getByRole("radio", {
      name: "Applications you choose",
    });
    await waitFor(() => expect(radio).toBeDisabled());
    expect(
      within(dialog).getByText("You have no applications to choose from."),
    ).toBeInTheDocument();
  });

  it("says what full access covers for an administrator", async () => {
    const user = userEvent.setup();
    mount({ role: "admin" });
    const dialog = await openDialog(user);
    expect(
      await within(dialog).findByText(
        "Applications, your account and what you manage as an administrator. Anything that asks you to confirm it is you is refused.",
      ),
    ).toBeInTheDocument();
  });

  it("selects a level from its description as well as its name", async () => {
    const user = userEvent.setup();
    mount();
    const dialog = await openDialog(user);
    await user.click(
      within(dialog).getByText(
        "Applications and your account. Anything that asks you to confirm it is you is refused.",
      ),
    );
    expect(
      within(dialog).getByRole("radio", { name: "Full access" }),
    ).toBeChecked();
  });

  it("starts from a blank form after Cancel", async () => {
    const user = userEvent.setup();
    mount();
    let dialog = await openDialog(user);
    await user.type(within(dialog).getByLabelText("Name"), "CI");
    await user.click(
      within(dialog).getByRole("radio", { name: "Full access" }),
    );
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );

    dialog = await openDialog(user);
    expect(within(dialog).getByLabelText("Name")).toHaveValue("");
    expect(
      within(dialog).getByRole("radio", {
        name: "Every application you can use",
      }),
    ).toBeChecked();
  });
});

describe("the token list", () => {
  it("names the token on its revoke button and shows a sudo token once", async () => {
    mount({
      tokens: [
        {
          id: 7,
          name: "CI deploy",
          tokenHint: "prohibitorum_pat_…a1b2",
          access: "sudo",
          apps: [],
          createdAt: new Date().toISOString(),
        },
      ],
    });
    expect(
      await screen.findByRole("button", { name: "Revoke CI deploy" }),
    ).toBeInTheDocument();
    // The hint is the server's own display string, drawn as it comes.
    expect(screen.getByText("prohibitorum_pat_…a1b2")).toBeInTheDocument();
    expect(screen.getByText("Skips confirming it is you")).toBeInTheDocument();
    expect(screen.getByText("Full access")).toBeInTheDocument();
  });
});
