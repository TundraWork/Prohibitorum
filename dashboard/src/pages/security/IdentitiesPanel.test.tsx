import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import { configureSudo, resetSudo } from "@/api/sudo";
import { loadDocument } from "@/app/load-document";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { IdentitiesPanel } from "@/pages/security/IdentitiesPanel";

vi.mock("@/app/load-document", () => ({ loadDocument: vi.fn() }));

type Identity = components["schemas"]["AccountIdentityView"];
type Provider = components["schemas"]["FederationProvider"];

const daysAgo = (days: number) =>
  new Date(Date.now() - days * 86_400_000).toISOString();

const github: Identity = {
  id: 1,
  providerSlug: "github",
  providerDisplayName: "GitHub",
  protocol: "oidc",
  email: "alice@example.test",
  subject: "gh|1234567890",
  linkedAt: daysAgo(3),
  data: {},
};
const corporate: Identity = {
  id: 2,
  providerSlug: "corp",
  providerDisplayName: "Corporate SSO",
  protocol: "saml",
  subject: "alice@corp.test",
  linkedAt: daysAgo(40),
  data: {},
};

const providers: Provider[] = [
  {
    slug: "github",
    displayName: "GitHub",
    protocol: "oidc",
    allowSignIn: true,
  },
  {
    slug: "corp",
    displayName: "Corporate SSO",
    protocol: "saml",
    allowSignIn: true,
  },
  {
    slug: "gitlab",
    displayName: "GitLab",
    protocol: "oidc",
    allowSignIn: true,
  },
];

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifySuccess = vi.fn<(message: MessageDescriptor) => void>();
const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifySuccess.mockReset();
  notifyError.mockReset();
  vi.mocked(loadDocument).mockReset();
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

function mount(
  identities: Identity[],
  available: Provider[] = providers,
  extra?: (request: Request) => Response | undefined,
) {
  fetchBoundary.mockImplementation(async (request) => {
    const canned = extra?.(request);
    if (canned) return canned;
    const path = new URL(request.url).pathname;
    if (path.endsWith("/me/identities")) return Response.json(identities);
    if (path.endsWith("/auth/federation")) return Response.json(available);
    if (path.endsWith("/me/sudo/methods")) {
      return Response.json({ methods: ["password_totp"], fresh: true });
    }
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <IdentitiesPanel />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

async function rows() {
  const list = await screen.findByRole("list", { name: "Linked identities" });
  return within(list).getAllByRole("listitem");
}

async function openLinkMenu(user: UserEvent) {
  await user.click(
    await screen.findByRole("button", { name: "Link another identity" }),
  );
  return screen.findByRole("menu");
}

describe("the linked identity rows", () => {
  it("shows the provider, protocol, email, subject and when it was linked", async () => {
    mount([github, corporate]);

    const [first, second] = (await rows()) as [HTMLElement, HTMLElement];
    expect(first).toHaveTextContent("GitHub");
    expect(first.querySelector("[data-slot='item-badge']")).toHaveTextContent(
      "OIDC",
    );
    expect(first).toHaveTextContent("alice@example.test");
    expect(first).toHaveTextContent("gh|1234567890");
    expect(first.querySelector("[data-slot='item-meta']")).toHaveTextContent(
      "Linked 3 days ago",
    );
    expect(second.querySelector("[data-slot='item-badge']")).toHaveTextContent(
      "SAML",
    );
    expect(second).toHaveTextContent("alice@corp.test");
  });

  it("keeps the heading and its description around the list", async () => {
    mount([github]);

    await rows();
    expect(
      screen.getByRole("heading", { name: "Linked identities" }),
    ).toBeVisible();
    expect(
      screen.getByText(
        "Other accounts you can sign in with. Unlinking one leaves your other ways in as they are.",
      ),
    ).toBeVisible();
  });

  it("shows an empty state when nothing is linked", async () => {
    mount([]);

    expect(
      await screen.findByText("No other identities linked yet"),
    ).toBeVisible();
    expect(
      screen.queryByRole("list", { name: "Linked identities" }),
    ).not.toBeInTheDocument();
  });
});

describe("the link menu", () => {
  it("lists every provider and disables the ones already linked", async () => {
    const user = userEvent.setup();
    mount([github, corporate]);
    await rows();

    await openLinkMenu(user);

    const linked = screen.getByRole("menuitem", { name: /GitHub/ });
    expect(linked).toHaveAttribute("aria-disabled", "true");
    expect(linked).toHaveTextContent("Linked");
    expect(
      screen.getByRole("menuitem", { name: /Corporate SSO/ }),
    ).toHaveAttribute("aria-disabled", "true");
    const free = screen.getByRole("menuitem", { name: "GitLab" });
    expect(free).not.toHaveAttribute("aria-disabled", "true");
    expect(free).not.toHaveTextContent("Linked");
  });

  it("offers one disabled line when no provider is configured", async () => {
    const user = userEvent.setup();
    mount([], []);
    await screen.findByText("No other identities linked yet");

    await openLinkMenu(user);

    const items = screen.getAllByRole("menuitem");
    expect(items).toHaveLength(1);
    expect(items[0]).toHaveTextContent("No identities available to link");
    expect(items[0]).toHaveAttribute("aria-disabled", "true");
  });

  it("leaves for the provider and keeps the button pending until the page returns", async () => {
    const user = userEvent.setup();
    mount([github]);
    await rows();

    await openLinkMenu(user);
    await user.click(screen.getByRole("menuitem", { name: "GitLab" }));

    await waitFor(() => expect(loadDocument).toHaveBeenCalledTimes(1));
    expect(vi.mocked(loadDocument).mock.calls[0]?.[0]).toContain(
      "/me/identities/link/gitlab/begin",
    );
    const button = screen.getByRole("button", {
      name: "Link another identity",
    });
    expect(button).toHaveAttribute("aria-busy", "true");

    // The back-forward cache brings the page back as it was, still pending.
    window.dispatchEvent(
      Object.assign(new Event("pageshow"), { persisted: true }),
    );
    await waitFor(() => expect(button).not.toHaveAttribute("aria-busy"));
  });
});

describe("unlinking an identity", () => {
  async function askToUnlink(user: UserEvent) {
    const [first] = await rows();
    await user.click(
      within(first as HTMLElement).getByRole("button", { name: "Unlink" }),
    );
    return screen.findByRole("alertdialog", { name: "Unlink this identity?" });
  }

  it("asks first, then sends the unlink", async () => {
    const user = userEvent.setup();
    mount([github, corporate]);

    const dialog = await askToUnlink(user);
    expect(
      fetchBoundary.mock.calls.filter(([r]) => r.method === "POST"),
    ).toHaveLength(0);
    await user.click(within(dialog).getByRole("button", { name: "Unlink" }));

    await waitFor(() =>
      expect(
        fetchBoundary.mock.calls.filter(
          ([r]) =>
            r.method === "POST" &&
            new URL(r.url).pathname.endsWith("/me/identities/1/unlink"),
        ),
      ).toHaveLength(1),
    );
    await waitFor(() =>
      expect(notifySuccess).toHaveBeenCalledWith(
        expect.objectContaining({ id: "success.identity.unlinked" }),
      ),
    );
  });

  it("reports a failed unlink once, inline, with no toast", async () => {
    const user = userEvent.setup();
    mount([github, corporate], providers, (request) =>
      new URL(request.url).pathname.endsWith("/unlink")
        ? Response.json(
            { code: "rate_limited", requestId: "r1" },
            { status: 429 },
          )
        : undefined,
    );

    const dialog = await askToUnlink(user);
    await user.click(within(dialog).getByRole("button", { name: "Unlink" }));

    expect(
      await screen.findByText("Too many requests. Wait a moment."),
    ).toBeVisible();
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
  });
});

describe("when the identities cannot be loaded", () => {
  it("reports it once, in the section, with no false empty state", async () => {
    const user = userEvent.setup();
    let failing = true;
    mount([github], providers, (request) =>
      failing && new URL(request.url).pathname.endsWith("/me/identities")
        ? new Response(null, { status: 500 })
        : undefined,
    );

    const retry = await screen.findByRole("button", { name: "Try again" });
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(
      screen.queryByText("No other identities linked yet"),
    ).not.toBeInTheDocument();
    expect(notifyError).not.toHaveBeenCalled();

    failing = false;
    await user.click(retry);

    expect(await rows()).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("keeps the rows and reports nothing when only the provider lookup fails", async () => {
    mount([github], providers, (request) =>
      new URL(request.url).pathname.endsWith("/auth/federation")
        ? new Response(null, { status: 500 })
        : undefined,
    );

    expect(await rows()).toHaveLength(1);
    await waitFor(() =>
      expect(
        fetchBoundary.mock.calls.some(([request]) =>
          new URL(request.url).pathname.endsWith("/auth/federation"),
        ),
      ).toBe(true),
    );
    expect(notifyError).not.toHaveBeenCalled();
  });
});
