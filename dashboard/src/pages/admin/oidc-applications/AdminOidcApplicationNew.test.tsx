import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import {
  apiError,
  type FakeApi,
  fakeApi,
  publicApi,
  renderApp,
  testSession,
} from "@/test/app";
import { oidcAppFixture } from "@/test/oidc-app";

const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  notifyError.mockReset();
  queryClient = createQueryClient(notifyError);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

const base = "/api/prohibitorum";

function serve(
  create: () => Response | Promise<Response>,
  role: "admin" | "user" = "admin",
): FakeApi {
  const created = oidcAppFixture({ clientId: "wiki", displayName: "Wiki" });
  return fakeApi({
    ...publicApi({ ...testSession, role }),
    [`POST ${base}/oidc-applications`]: create,
    [`GET ${base}/oidc-applications`]: () =>
      Response.json({ items: [created], nextCursor: "" }),
    [`GET ${base}/oidc-applications/wiki`]: () => Response.json(created),
    [`GET ${base}/saml-applications`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${base}/forward-auth-apps`]: () =>
      Response.json({ items: [], nextCursor: "" }),
  });
}

async function mountForm() {
  const user = userEvent.setup();
  const router = renderApp("/admin/oidc-applications/new", queryClient);
  await screen.findByRole("heading", {
    level: 1,
    name: "New OIDC application",
  });
  return { user, router };
}

async function fill(
  user: ReturnType<typeof userEvent.setup>,
  name = "Wiki",
  clientId = "wiki",
) {
  await user.type(screen.getByRole("textbox", { name: "Name" }), name);
  await user.type(
    screen.getByRole("textbox", { name: /^Client ID/ }),
    clientId,
  );
  await user.type(
    screen.getByRole("textbox", { name: "Redirect URIs 1" }),
    "https://wiki.example.test/callback",
  );
}

describe("the form's shape", () => {
  it("groups the fields into sections a reader can name", async () => {
    serve(() => Response.json({}));
    await mountForm();

    expect(
      screen
        .getAllByRole("heading", { level: 2 })
        .map((heading) => heading.textContent),
    ).toEqual(["Identity", "Client type", "Sign-in", "Access"]);
  });

  it("offers the two client types as described choices, confidential first and chosen", async () => {
    serve(() => Response.json({}));
    await mountForm();

    const group = screen.getByRole("radiogroup", { name: "Client type" });
    const confidential = within(group).getByRole("radio", {
      name: "Confidential",
    });
    const publicClient = within(group).getByRole("radio", { name: "Public" });
    expect(confidential).toBeChecked();
    expect(publicClient).not.toBeChecked();
    expect(confidential).toHaveAccessibleDescription(/keep a secret/);
    expect(publicClient).toHaveAccessibleDescription(/cannot keep a secret/);
  });

  it("states the restriction's consequence beside its switch", async () => {
    serve(() => Response.json({}));
    await mountForm();

    const restrict = screen.getByRole("switch", { name: "Restrict access" });
    expect(restrict).not.toBeChecked();
    expect(restrict).toHaveAccessibleDescription(
      /Nobody can use the application until you select a user group/,
    );
  });
});

describe("the PKCE switch", () => {
  it("is free for a confidential client", async () => {
    serve(() => Response.json({}));
    await mountForm();

    const pkce = screen.getByRole("switch", { name: "Require PKCE" });
    expect(pkce).not.toHaveAttribute("aria-disabled", "true");
    expect(pkce).toBeChecked();
    expect(
      screen.queryByText(
        "A public client always proves the request it started.",
      ),
    ).toBeNull();
  });

  it("is locked on for a public client, with the reason in view", async () => {
    serve(() => Response.json({}));
    const { user } = await mountForm();

    await user.click(screen.getByRole("radio", { name: "Public" }));

    const pkce = screen.getByRole("switch", { name: "Require PKCE" });
    expect(pkce).toHaveAttribute("aria-disabled", "true");
    expect(pkce).toBeChecked();
    expect(
      screen.getByText("A public client always proves the request it started."),
    ).toBeVisible();
  });
});

describe("the PKCE switch when the type changes", () => {
  it("holds on after being turned off for a confidential client and then made public", async () => {
    const api = serve(() =>
      Response.json({ clientId: "wiki", displayName: "Wiki" }, { status: 201 }),
    );
    const { user } = await mountForm();

    await user.click(screen.getByRole("switch", { name: "Require PKCE" }));
    expect(
      screen.getByRole("switch", { name: "Require PKCE" }),
    ).not.toBeChecked();
    await user.click(screen.getByRole("radio", { name: "Public" }));

    expect(screen.getByRole("switch", { name: "Require PKCE" })).toBeChecked();
    await fill(user);
    await user.click(
      screen.getByRole("button", { name: "Create application" }),
    );
    await screen.findByRole("heading", { level: 1, name: "Wiki" });
    expect(
      api.sent("POST", `${base}/oidc-applications`)[0]?.body,
    ).toMatchObject({ public: true, requirePkce: true });
  });
});

describe("validation", () => {
  it("asks for a name and a Client ID before sending anything", async () => {
    const api = serve(() => Response.json({}));
    const { user } = await mountForm();

    await user.click(
      screen.getByRole("button", { name: "Create application" }),
    );

    expect(await screen.findByText("Enter a name.")).toBeVisible();
    expect(screen.getByText("Enter a Client ID.")).toBeVisible();
    expect(api.sent("POST", `${base}/oidc-applications`)).toHaveLength(0);
  });

  it("refuses a Client ID that would not survive as a path segment", async () => {
    serve(() => Response.json({}));
    const { user } = await mountForm();

    await fill(user, "Wiki", "not valid!");
    await user.click(
      screen.getByRole("button", { name: "Create application" }),
    );

    expect(
      await screen.findByText(/Use letters, digits, dots, underscores/),
    ).toBeVisible();
  });

  it("marks the redirect row that is not an address", async () => {
    const api = serve(() => Response.json({}));
    const { user } = await mountForm();

    await fill(user);
    const row = screen.getByRole("textbox", { name: "Redirect URIs 1" });
    await user.clear(row);
    await user.type(row, "wiki");
    await user.click(
      screen.getByRole("button", { name: "Create application" }),
    );

    expect(
      await screen.findByText(/Use an absolute http or https address/),
    ).toBeVisible();
    expect(api.sent("POST", `${base}/oidc-applications`)).toHaveLength(0);
  });
});

describe("creating", () => {
  it("shows a confidential client's secret before leaving, and goes on from the dialog", async () => {
    const api = serve(() =>
      Response.json(
        { clientId: "wiki", displayName: "Wiki", secret: "s3cret-value" },
        { status: 201 },
      ),
    );
    const { user, router } = await mountForm();

    await fill(user);
    await user.click(
      screen.getByRole("button", { name: "Create application" }),
    );

    const dialog = await screen.findByRole("dialog", {
      name: "Copy the client secret",
    });
    expect(router.state.location.pathname).toBe("/admin/oidc-applications/new");
    expect(within(dialog).getByText("s3cret-value")).toBeVisible();
    expect(
      api.sent("POST", `${base}/oidc-applications`)[0]?.body,
    ).toMatchObject({
      clientId: "wiki",
      displayName: "Wiki",
      redirectUris: ["https://wiki.example.test/callback"],
      scopes: ["openid", "profile", "email"],
      public: false,
      requirePkce: true,
      requireConsent: false,
      accessRestricted: false,
    });

    await user.click(
      within(dialog).getByRole("checkbox", {
        name: "I have saved the client secret",
      }),
    );
    await user.click(
      within(dialog).getByRole("button", { name: "Go to the application" }),
    );

    await screen.findByRole("heading", { level: 1, name: "Wiki" });
    expect(router.state.location.pathname).toBe(
      "/admin/oidc-applications/wiki",
    );
  });

  it("sends a public client straight to its page, with PKCE on", async () => {
    const api = serve(() =>
      Response.json({ clientId: "wiki", displayName: "Wiki" }, { status: 201 }),
    );
    const { user, router } = await mountForm();

    await fill(user);
    await user.click(screen.getByRole("radio", { name: "Public" }));
    await user.click(
      screen.getByRole("button", { name: "Create application" }),
    );

    await screen.findByRole("heading", { level: 1, name: "Wiki" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(router.state.location.pathname).toBe(
      "/admin/oidc-applications/wiki",
    );
    expect(
      api.sent("POST", `${base}/oidc-applications`)[0]?.body,
    ).toMatchObject({ public: true, requirePkce: true });
  });

  it("puts a taken Client ID on its field, and says it once", async () => {
    serve(() => apiError("oidc_client_already_exists", 409));
    const { user, router } = await mountForm();

    await fill(user);
    await user.click(
      screen.getByRole("button", { name: "Create application" }),
    );

    const field = screen.getByRole("textbox", { name: /^Client ID/ });
    await waitFor(() => expect(field).toHaveAttribute("aria-invalid", "true"));
    expect(field).toHaveAccessibleDescription(/already/i);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(router.state.location.pathname).toBe("/admin/oidc-applications/new");
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("says another failure on the form, and says it once", async () => {
    serve(() => apiError("internal_error", 500));
    const { user } = await mountForm();

    await fill(user);
    await user.click(
      screen.getByRole("button", { name: "Create application" }),
    );

    await screen.findByRole("alert");
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("the list afterwards", () => {
  it("is invalidated by a creation, so it shows the new application", async () => {
    serve(() =>
      Response.json({ clientId: "wiki", displayName: "Wiki" }, { status: 201 }),
    );
    const { user } = await mountForm();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    await fill(user);
    await user.click(
      screen.getByRole("button", { name: "Create application" }),
    );
    await screen.findByRole("heading", { level: 1, name: "Wiki" });

    expect(invalidate).toHaveBeenCalledWith(
      expect.objectContaining({ queryKey: ["admin", "oidc-applications"] }),
    );
  });
});

describe("leaving", () => {
  it("goes back to the list on Cancel without sending anything", async () => {
    const api = serve(() => Response.json({}));
    const { user, router } = await mountForm();

    await user.click(screen.getByRole("link", { name: "Cancel" }));

    await screen.findByRole("heading", { level: 1, name: "OIDC applications" });
    expect(router.state.location.pathname).toBe("/admin/oidc-applications");
    expect(api.sent("POST", `${base}/oidc-applications`)).toHaveLength(0);
  });
});
