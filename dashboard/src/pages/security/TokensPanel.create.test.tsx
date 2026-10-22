import type { MessageDescriptor } from "@lingui/core";
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
const notifySuccess = vi.fn<(message: MessageDescriptor) => void>();
const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;

const plaintext = `prohibitorum_pat_${"A".repeat(43)}`;

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifySuccess.mockReset();
  notifyError.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(notifyError, notifySuccess);
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
  createFailure,
  failRead,
}: {
  apps?: App[];
  role?: Session["role"];
  tokens?: Token[];
  /** Answers the create request instead of making a token. */
  createFailure?: Response;
  /** Paths whose read answers with a server error. */
  failRead?: string[];
} = {}) {
  const session: Session = {
    id: 1,
    username: "alice",
    oidcSubject: "00000000-0000-4000-8000-000000000001",
    displayName: "Alice",
    role,
    preferences: { appearance: { scheme: null, mode: null } },
  };
  fetchBoundary.mockImplementation(async (request) => {
    const path = new URL(request.url).pathname;
    if (failRead?.some((suffix) => path.endsWith(suffix))) {
      return new Response(null, { status: 500 });
    }
    if (path.endsWith("/me")) return Response.json(session);
    if (path.endsWith("/me/forward-auth-apps")) return Response.json(apps);
    if (path.endsWith("/me/sudo/methods")) {
      return Response.json({ methods: ["password_totp"], fresh: true });
    }
    if (path.endsWith("/me/tokens") && request.method === "POST") {
      if (createFailure) return createFailure;
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
    await waitFor(() => expect(radio).toHaveAttribute("aria-disabled", "true"));
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

describe("the access levels", () => {
  it("offers four radios, each carrying its description", async () => {
    const user = userEvent.setup();
    mount();
    const dialog = await openDialog(user);

    const radios = within(dialog).getAllByRole("radio");
    expect(radios).toHaveLength(4);
    expect(radios[0]).toHaveAccessibleName("Applications you choose");
    expect(radios[0]).toHaveAccessibleDescription(
      "Works only on the applications you pick.",
    );
    expect(radios[3]).toHaveAccessibleName(
      "Full access, without confirming it is you",
    );
    expect(radios[3]).toHaveAccessibleDescription(
      "Everything, including changing how you sign in, without being asked to confirm it is you.",
    );
  });

  it("shows the application checklist only under the level that uses it", async () => {
    const user = userEvent.setup();
    mount({
      apps: [
        { clientId: "wiki", displayName: "Wiki" },
        { clientId: "grafana", displayName: "Grafana" },
      ],
    });
    const dialog = await openDialog(user);
    expect(
      within(dialog).queryByRole("group", { name: "Applications" }),
    ).not.toBeInTheDocument();

    await user.click(
      within(dialog).getByRole("radio", { name: "Applications you choose" }),
    );
    const group = await within(dialog).findByRole("group", {
      name: "Applications",
    });
    expect(within(group).getAllByRole("checkbox")).toHaveLength(2);

    await user.click(
      within(dialog).getByRole("radio", { name: "Full access" }),
    );
    expect(
      within(dialog).queryByRole("group", { name: "Applications" }),
    ).not.toBeInTheDocument();
  });

  it("sends only the applications that were ticked", async () => {
    const user = userEvent.setup();
    mount({
      apps: [
        { clientId: "wiki", displayName: "Wiki" },
        { clientId: "grafana", displayName: "Grafana" },
      ],
    });
    const dialog = await openDialog(user);
    await user.click(
      within(dialog).getByRole("radio", { name: "Applications you choose" }),
    );
    const group = await within(dialog).findByRole("group", {
      name: "Applications",
    });
    await user.click(within(group).getByRole("checkbox", { name: "Grafana" }));
    await user.type(within(dialog).getByLabelText("Name"), "Dash");
    await user.click(
      within(dialog).getByRole("button", { name: "Create token" }),
    );

    await screen.findByRole("dialog", { name: "Copy your new access token" });
    expect(await creates()[0]?.json()).toEqual({
      name: "Dash",
      access: "selected_apps",
      appClientIds: ["grafana"],
    });
  });

  it("turns the submit destructive for the level that skips the identity check", async () => {
    const user = userEvent.setup();
    mount();
    const dialog = await openDialog(user);
    const submit = within(dialog).getByRole("button", { name: "Create token" });
    expect(submit).toHaveAttribute("data-variant", "default");

    await user.click(
      within(dialog).getByRole("radio", {
        name: "Full access, without confirming it is you",
      }),
    );
    expect(submit).toHaveAttribute("data-variant", "destructive");

    await user.click(
      within(dialog).getByRole("radio", { name: "Full access" }),
    );
    expect(submit).toHaveAttribute("data-variant", "default");
  });
});

describe("the lifetime and a refused create", () => {
  it("sends the chosen lifetime in days", async () => {
    const user = userEvent.setup();
    mount();
    const dialog = await openDialog(user);
    await user.type(within(dialog).getByLabelText("Name"), "CI");
    await user.click(
      within(dialog).getByRole("combobox", { name: "Expires after" }),
    );
    await user.click(await screen.findByRole("option", { name: "90 days" }));
    await user.click(
      within(dialog).getByRole("button", { name: "Create token" }),
    );

    await screen.findByRole("dialog", { name: "Copy your new access token" });
    expect(await creates()[0]?.json()).toEqual({
      name: "CI",
      access: "all_apps",
      expiresInDays: 90,
    });
  });

  it("reports a refusal once, in the form, and keeps the dialog open", async () => {
    const user = userEvent.setup();
    mount({
      createFailure: Response.json(
        { code: "rate_limited", requestId: "r1" },
        { status: 429 },
      ),
    });
    const dialog = await openDialog(user);
    await user.type(within(dialog).getByLabelText("Name"), "CI");
    await user.click(
      within(dialog).getByRole("button", { name: "Create token" }),
    );

    expect(
      await within(dialog).findByText("Too many requests. Wait a moment."),
    ).toBeVisible();
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
    expect(within(dialog).getByLabelText("Name")).toHaveValue("CI");
  });
});

const base: Token = {
  id: 7,
  name: "CI deploy",
  tokenHint: "prohibitorum_pat_…a1b2",
  access: "sudo",
  apps: [],
  createdAt: new Date().toISOString(),
};
const daysFromNow = (days: number) =>
  new Date(Date.now() + days * 86_400_000).toISOString();

async function rows() {
  const list = await screen.findByRole("list", { name: "Access tokens" });
  return within(list).getAllByRole("listitem");
}

describe("the token list", () => {
  it("names the token on its revoke button and marks a sudo token", async () => {
    mount({ tokens: [base] });

    const [row] = (await rows()) as [HTMLElement];
    expect(
      within(row).getByRole("button", { name: "Revoke CI deploy" }),
    ).toBeInTheDocument();
    // The hint is the server's own display string, drawn as it comes.
    expect(row).toHaveTextContent("prohibitorum_pat_…a1b2");
    expect(row).toHaveTextContent("Skips confirming it is you");
    expect(row).toHaveTextContent("Full access");
  });

  it("says when a token was last used, or that it has not been", async () => {
    mount({
      tokens: [
        {
          ...base,
          id: 1,
          name: "Used",
          access: "full",
          lastUsedAt: new Date(Date.now() - 2 * 3_600_000).toISOString(),
        },
        { ...base, id: 2, name: "Fresh", access: "full" },
      ],
    });

    const [used, fresh] = (await rows()) as [HTMLElement, HTMLElement];
    expect(used).toHaveTextContent("Last used 2 hours ago");
    expect(fresh).toHaveTextContent("Not used yet");
  });

  it("says when a token expires, or that it never does", async () => {
    mount({
      tokens: [
        {
          ...base,
          id: 1,
          name: "Dated",
          access: "full",
          expiresAt: daysFromNow(40),
        },
        { ...base, id: 2, name: "Open", access: "all_apps" },
      ],
    });

    const [dated, open] = (await rows()) as [HTMLElement, HTMLElement];
    expect(dated.querySelector("[data-slot='item-meta']")).toHaveTextContent(
      /^Expires /,
    );
    expect(open.querySelector("[data-slot='item-meta']")).toHaveTextContent(
      "Never expires",
    );
    expect(within(dated).queryByText("Skips confirming it is you")).toBeNull();
  });

  it("marks a token whose expiry has passed", async () => {
    mount({
      tokens: [{ ...base, access: "full", expiresAt: daysFromNow(-2) }],
    });

    const [row] = (await rows()) as [HTMLElement];
    expect(within(row).getByText("Expired")).toHaveAttribute(
      "data-slot",
      "badge",
    );
  });

  it("shows an empty state when there are no tokens", async () => {
    mount();

    expect(await screen.findByText("No access tokens yet")).toBeVisible();
    expect(
      screen.queryByRole("list", { name: "Access tokens" }),
    ).not.toBeInTheDocument();
  });

  it("keeps the heading and its description around the list", async () => {
    mount({ tokens: [base] });

    await rows();
    expect(
      screen.getByRole("heading", { name: "Access tokens" }),
    ).toBeVisible();
    expect(
      screen.getByText(
        "Keys for scripts and tools that act as you. Each is shown once, when you create it.",
      ),
    ).toBeVisible();
  });

  it("asks before revoking, then sends the revoke", async () => {
    const user = userEvent.setup();
    mount({ tokens: [base] });

    const [row] = (await rows()) as [HTMLElement];
    await user.click(
      within(row).getByRole("button", { name: "Revoke CI deploy" }),
    );
    const dialog = await screen.findByRole("alertdialog", {
      name: "Revoke this token?",
    });
    const revokes = () =>
      fetchBoundary.mock.calls.filter(
        ([r]) =>
          r.method === "POST" &&
          new URL(r.url).pathname.endsWith("/me/tokens/revoke"),
      );
    expect(revokes()).toHaveLength(0);

    await user.click(within(dialog).getByRole("button", { name: "Revoke" }));
    await waitFor(() => expect(revokes()).toHaveLength(1));
    expect(await revokes()[0]?.[0].json()).toEqual({ id: 7 });
  });
});

describe("the token list read", () => {
  it("reports a failed read once, in the section, with no false empty state", async () => {
    mount({ failRead: ["/me/tokens"] });

    expect(
      await screen.findByRole("button", { name: "Try again" }),
    ).toBeVisible();
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(screen.queryByText("No access tokens yet")).not.toBeInTheDocument();
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("says the applications could not be loaded instead of claiming there are none", async () => {
    const user = userEvent.setup();
    mount({ failRead: ["/me/forward-auth-apps"] });
    const dialog = await openDialog(user);

    expect(
      await within(dialog).findByText(
        "Your applications could not be loaded. Close this and try again.",
      ),
    ).toBeVisible();
    expect(
      within(dialog).queryByText("You have no applications to choose from."),
    ).not.toBeInTheDocument();
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("gives focus back to the button that opened the dialog on Cancel and on Escape", async () => {
    const user = userEvent.setup();
    mount();
    const opener = await screen.findByRole("button", {
      name: "Create a token",
    });

    const dialog = await openDialog(user);
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(opener).toHaveFocus());

    await openDialog(user);
    await user.keyboard("{Escape}");
    await waitFor(() => expect(opener).toHaveFocus());
  });
});
