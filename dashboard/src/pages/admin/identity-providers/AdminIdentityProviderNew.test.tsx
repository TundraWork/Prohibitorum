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
import { providerFixture } from "@/test/identity-provider";

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
const createPath = `${base}/identity-providers`;

function serve(
  create: () => Response | Promise<Response> = () =>
    Response.json(providerFixture({ ready: false, mode: "invite_only" }), {
      status: 201,
    }),
): FakeApi {
  return fakeApi({
    ...publicApi({ ...testSession, role: "admin" }),
    [`POST ${createPath}`]: create,
    [`GET ${createPath}`]: () =>
      Response.json({ items: [providerFixture()], nextCursor: "" }),
    [`GET ${createPath}/corp`]: () => Response.json(providerFixture()),
    [`GET ${base}/me/sudo/methods`]: () =>
      Response.json({ methods: ["password_totp"], fresh: true }),
  });
}

async function mountForm() {
  const user = userEvent.setup();
  const router = renderApp("/admin/identity-providers/new", queryClient);
  await screen.findByRole("heading", {
    level: 1,
    name: "Add an identity provider",
  });
  return { user, router };
}

/** One provisioning card; the chosen one is the checked radio. */
function modeRadio(name: string): HTMLElement {
  return screen.getByRole("radio", { name });
}

async function chooseProtocol(
  user: ReturnType<typeof userEvent.setup>,
  name: string,
) {
  await user.click(screen.getByRole("radio", { name }));
}

/** The fields an OIDC provider cannot be created without, for a public client. */
async function fillOidc(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText("Name"), "Corp");
  await user.type(screen.getByLabelText("Identifier"), "corp");
  await user.type(screen.getByLabelText("Issuer URL"), "https://idp.example");
  await user.type(screen.getByLabelText("Client ID"), "client-1");
  // A public client needs no secret.
  await user.click(screen.getByRole("radio", { name: "Public client" }));
}

describe("the form's shape", () => {
  it("groups an OpenID Connect provider's fields into sections a reader can name", async () => {
    serve();
    await mountForm();

    expect(
      screen
        .getAllByRole("heading", { level: 2 })
        .map((heading) => heading.textContent),
    ).toEqual(["Protocol", "Provider", "Connection"]);
  });

  it("offers the three protocols as described choices, OpenID Connect first and chosen", async () => {
    serve();
    await mountForm();

    const group = screen.getByRole("radiogroup", { name: "Protocol" });
    const oidc = within(group).getByRole("radio", { name: "OpenID Connect" });
    expect(oidc).toBeChecked();
    expect(oidc).toHaveAccessibleDescription(/Google, Microsoft Entra ID/);
    expect(
      within(group).getByRole("radio", { name: "Steam" }),
    ).not.toBeChecked();
    expect(
      within(group).getByRole("radio", { name: "VRChat" }),
    ).toHaveAccessibleDescription(/VRChat operator account/);
  });

  it("asks a Steam provider for its Web API key and a VRChat provider for nothing more", async () => {
    serve();
    const { user } = await mountForm();

    await chooseProtocol(user, "Steam");
    expect(screen.getByLabelText("Web API key")).toBeVisible();
    expect(screen.queryByLabelText("Issuer URL")).toBeNull();

    await chooseProtocol(user, "VRChat");
    expect(screen.queryByLabelText("Web API key")).toBeNull();
    expect(screen.queryByLabelText("Issuer URL")).toBeNull();
    expect(
      screen
        .getAllByRole("heading", { level: 2 })
        .map((heading) => heading.textContent),
    ).toEqual(["Protocol", "Provider"]);
  });

  it("shows the callback addresses in their own section once the identifier is valid", async () => {
    serve();
    const { user } = await mountForm();

    expect(
      screen.queryByRole("heading", { level: 2, name: "Callback addresses" }),
    ).toBeNull();
    await user.type(screen.getByLabelText("Identifier"), "corp");

    expect(
      screen.getByRole("heading", { level: 2, name: "Callback addresses" }),
    ).toBeVisible();
    expect(
      screen.getByRole("group", { name: "Callback addresses" }),
    ).toBeVisible();
    expect(screen.getByText("Register these with the provider.")).toBeVisible();
  });

  it("says what the identifier is for and what it may contain", async () => {
    serve();
    const { user } = await mountForm();

    const field = screen.getByLabelText("Identifier");
    expect(field).toHaveAccessibleDescription(/cannot be changed later/);
    await user.type(field, "Corp SSO");
    await user.click(screen.getByRole("button", { name: "Add provider" }));

    expect(
      await screen.findByText("Use only the allowed characters."),
    ).toBeVisible();
  });
});

describe("the new provider form's sign-in switch", () => {
  it("defaults the switch off for VRChat and on for the others, with the mode following", async () => {
    serve();
    const { user } = await mountForm();

    const switchEl = screen.getByRole("switch", { name: "Allow sign-in" });
    expect(switchEl).toBeChecked();
    expect(modeRadio("Creates accounts")).toBeChecked();

    await chooseProtocol(user, "VRChat");
    expect(
      screen.getByRole("switch", { name: "Allow sign-in" }),
    ).not.toBeChecked();
    expect(modeRadio("Invitation only")).toBeChecked();

    await chooseProtocol(user, "Steam");
    expect(screen.getByRole("switch", { name: "Allow sign-in" })).toBeChecked();
    expect(modeRadio("Creates accounts")).toBeChecked();
  });

  it("shows the weak-proof note while VRChat is chosen, whoever flips the switch", async () => {
    serve();
    const { user } = await mountForm();

    await chooseProtocol(user, "VRChat");
    expect(
      screen.getByText(/VRChat proves a profile through a link in its bio/),
    ).toBeInTheDocument();
    // The note stays even if the operator turns sign-in back on: what it
    // warns about is the proof, not the button.
    await user.click(screen.getByRole("switch", { name: "Allow sign-in" }));
    expect(
      screen.getByText(/VRChat proves a profile through a link in its bio/),
    ).toBeInTheDocument();
  });

  it("drops the creates-accounts mode when the switch turns off, resetting a live selection", async () => {
    serve();
    const { user } = await mountForm();

    await user.click(screen.getByRole("switch", { name: "Allow sign-in" }));
    await waitFor(() => expect(modeRadio("Invitation only")).toBeChecked());
    expect(modeRadio("Creates accounts")).toHaveAttribute(
      "aria-disabled",
      "true",
    );
    // The reason is written outside the dimmed card.
    expect(
      screen.getByText("Turn sign-in on to create accounts."),
    ).toBeVisible();
  });

  it("sends allowSignIn and the reset mode in the create body", async () => {
    const api = serve();
    const { user } = await mountForm();

    await fillOidc(user);
    await user.click(screen.getByRole("switch", { name: "Allow sign-in" }));
    await user.click(screen.getByRole("button", { name: "Add provider" }));

    await waitFor(() => {
      expect(api.sent("POST", createPath)).toHaveLength(1);
    });
    const body = api.sent("POST", createPath)[0]?.body as {
      allowSignIn?: boolean;
      mode?: string;
    };
    expect(body.allowSignIn).toBe(false);
    expect(body.mode).toBe("invite_only");
  });
});

describe("validation", () => {
  it("asks for a name, an identifier and the connection's required values before sending anything", async () => {
    const api = serve();
    const { user } = await mountForm();

    await user.click(screen.getByRole("button", { name: "Add provider" }));

    expect(await screen.findByText("Enter a name.")).toBeVisible();
    expect(screen.getByText("Enter an identifier.")).toBeVisible();
    expect(screen.getByText("Enter the client secret.")).toBeVisible();
    expect(api.sent("POST", createPath)).toHaveLength(0);
  });
});

describe("creating", () => {
  it("sends an OpenID Connect provider and goes on to its page", async () => {
    const api = serve();
    const { user, router } = await mountForm();

    await fillOidc(user);
    await user.click(screen.getByRole("button", { name: "Add provider" }));

    await screen.findByRole("heading", { level: 1, name: "Corporate SSO" });
    expect(router.state.location.pathname).toBe(
      "/admin/identity-providers/corp",
    );
    expect(api.sent("POST", createPath)[0]?.body).toMatchObject({
      slug: "corp",
      displayName: "Corp",
      protocol: "oidc",
      allowSignIn: true,
      mode: "auto_provision",
      config: { issuerUrl: "https://idp.example", clientId: "client-1" },
    });
  });

  it("puts a taken identifier on its field, and says it once", async () => {
    serve(() => apiError("upstream_idp_already_exists", 409));
    const { user, router } = await mountForm();

    await fillOidc(user);
    await user.click(screen.getByRole("button", { name: "Add provider" }));

    const field = screen.getByLabelText("Identifier");
    await waitFor(() => expect(field).toHaveAttribute("aria-invalid", "true"));
    expect(field).toHaveAccessibleDescription(/already/i);
    expect(screen.queryByRole("alert")).toBeNull();
    expect(router.state.location.pathname).toBe(
      "/admin/identity-providers/new",
    );
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("says another failure on the form, and says it once", async () => {
    serve(() => apiError("internal_error", 500));
    const { user } = await mountForm();

    await fillOidc(user);
    await user.click(screen.getByRole("button", { name: "Add provider" }));

    await screen.findByRole("alert");
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("the list afterwards", () => {
  it("is invalidated by a creation, so it shows the new provider", async () => {
    serve();
    const { user } = await mountForm();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    await fillOidc(user);
    await user.click(screen.getByRole("button", { name: "Add provider" }));
    await screen.findByRole("heading", { level: 1, name: "Corporate SSO" });

    expect(invalidate).toHaveBeenCalledWith(
      expect.objectContaining({ queryKey: ["admin", "identity-providers"] }),
    );
  });
});

describe("leaving", () => {
  it("goes back to the list on Cancel without sending anything", async () => {
    const api = serve();
    const { user, router } = await mountForm();

    await user.click(screen.getByRole("link", { name: "Cancel" }));

    await screen.findByRole("heading", {
      level: 1,
      name: "Identity providers",
    });
    expect(router.state.location.pathname).toBe("/admin/identity-providers");
    expect(api.sent("POST", createPath)).toHaveLength(0);
  });
});
