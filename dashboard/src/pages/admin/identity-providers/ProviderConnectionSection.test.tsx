import { I18nProvider } from "@lingui/react";
import { QueryClientProvider, useSuspenseQuery } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Suspense } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { identityProviderQueryOptions } from "@/api/queries";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { ProviderConnectionSection } from "@/pages/admin/identity-providers/ProviderConnectionSection";
import { apiError, type FakeApi, fakeApi } from "@/test/app";
import {
  type Provider,
  providerFixture,
  publicOidcConfig,
} from "@/test/identity-provider";

type QueryClient = ReturnType<typeof createQueryClient>;

const slug = "public-idp";
const putPath = `/api/prohibitorum/identity-providers/${slug}`;
const rotatePath = "/api/prohibitorum/identity-providers/rotate-secret";

let queryClient: QueryClient;
let api: FakeApi;
const notifyError = vi.fn<(error: unknown) => void>();

beforeEach(() => {
  i18n.activate("en");
  notifyError.mockReset();
});

/** An OIDC provider saved as a public client, enabled and ready. */
function publicProvider(overrides: Partial<Provider> = {}): Provider {
  return providerFixture({
    slug,
    displayName: "Public IdP",
    secretConfigured: false,
    secretStatus: "unconfigured",
    config: publicOidcConfig,
    ...overrides,
  });
}

/** A confidential client whose endpoints are entered by hand. */
function manualProvider(
  config: Record<string, unknown> = {},
  overrides: Partial<Provider> = {},
): Provider {
  return publicProvider({
    secretConfigured: true,
    secretStatus: "valid",
    config: {
      ...publicOidcConfig,
      tokenAuthMethod: "client_secret_basic",
      configurationMode: "manual",
      endpoints: {
        authorization: "https://idp.example.com/authorize",
        token: "https://idp.example.com/token",
        userinfo: null,
        jwks: "https://idp.example.com/jwks",
      },
      ...config,
    },
    ...overrides,
  });
}

function mount(provider: Provider) {
  queryClient = createQueryClient(notifyError);
  // The detail page holds this query open with useSuspenseQuery; an observer
  // here does the same, so the form's invalidation refetches the provider
  // the way the real page would.
  queryClient.setQueryData(
    identityProviderQueryOptions(provider.slug).queryKey,
    provider,
  );
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <Suspense fallback={null}>
          <ProviderObserver slug={provider.slug} />
        </Suspense>
        <ProviderConnectionSection provider={provider} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

/** Holds the provider query active, as the detail page's route does. */
function ProviderObserver({ slug }: { slug: string }) {
  useSuspenseQuery(identityProviderQueryOptions(slug));
  return null;
}

const radio = (name: string) => screen.getByRole("radio", { name });
const save = () => screen.getByRole("button", { name: "Save" });

/** The body of the one configuration write, parsed. */
function savedConfig() {
  const sent = api.sent("PUT", putPath);
  expect(sent).toHaveLength(1);
  const body = sent[0]?.body as { config: Record<string, unknown> };
  return body.config;
}

describe("the connection form's client authentication", () => {
  it("offers each method as a described option, the saved one chosen", () => {
    mount(publicProvider());

    expect(radio("Public client")).toBeChecked();
    expect(
      screen.getByText("Sends the client secret in the Authorization header."),
    ).toBeVisible();
    expect(radio("client_secret_basic")).not.toBeChecked();
    expect(radio("client_secret_post")).not.toBeChecked();
    expect(radio("Use what discovery reports")).not.toHaveAttribute(
      "aria-disabled",
      "true",
    );
  });

  it("makes discovery unavailable with endpoints entered by hand, and says why in plain text", () => {
    mount(manualProvider());

    const discovery = radio("Use what discovery reports");
    expect(discovery).toHaveAttribute("aria-disabled", "true");
    const reason = screen.getByText(
      "With endpoints entered by hand, choose a specific method.",
    );
    expect(reason).toBeVisible();
    // The reason is outside any dimmed card and is the group's description.
    expect(reason.closest("[data-slot=radio-group]")).toBeNull();
    expect(
      screen.getByRole("radiogroup", { name: "Client authentication" }),
    ).toHaveAccessibleDescription(
      "With endpoints entered by hand, choose a specific method.",
    );
  });

  it("makes discovery available again when the mode goes back to discovery", async () => {
    mount(manualProvider());
    const user = userEvent.setup();

    await user.click(radio("Discover automatically"));
    expect(radio("Use what discovery reports")).not.toHaveAttribute(
      "aria-disabled",
      "true",
    );
    expect(
      screen.queryByText(
        "With endpoints entered by hand, choose a specific method.",
      ),
    ).not.toBeInTheDocument();
  });

  it("refuses a saved discovery method under manual endpoints, once, without sending", async () => {
    const user = userEvent.setup();
    api = fakeApi({});
    mount(manualProvider({ tokenAuthMethod: "discovery" }));

    await user.type(screen.getByLabelText("Client ID"), "x");
    await user.click(save());

    expect(
      await screen.findAllByText(
        "With endpoints entered by hand, choose a specific method.",
      ),
    ).toHaveLength(1);
    expect(api.sent("PUT", putPath)).toHaveLength(0);
  });
});

describe("the connection form's client secret", () => {
  it("draws the secret field when a public client is switched to a method that sends one, and sends it before the save", async () => {
    const provider = publicProvider();
    const order: string[] = [];
    api = fakeApi({
      [`POST ${rotatePath}`]: () => {
        order.push("secret");
        return new Response(null, { status: 204 });
      },
      [`PUT ${putPath}`]: () => {
        order.push("configuration");
        return Response.json(provider);
      },
    });
    mount(provider);
    const user = userEvent.setup();

    expect(screen.queryByLabelText("Client secret")).not.toBeInTheDocument();

    await user.click(radio("client_secret_basic"));
    const secret = await screen.findByLabelText("Client secret");
    expect(secret).toHaveAttribute("type", "password");

    await user.type(secret, "the-new-secret");
    await user.click(save());

    await waitFor(() => expect(api.sent("PUT", putPath)).toHaveLength(1));
    // The secret goes first, while the stored configuration still names the
    // old method, and the save never repeats it: the update endpoint refuses
    // a secret in its body.
    expect(order).toEqual(["secret", "configuration"]);
    const rotated = api.sent("POST", rotatePath);
    expect(rotated).toHaveLength(1);
    expect(rotated[0]?.body).toEqual({
      slug,
      secret: "the-new-secret",
    });
    expect(JSON.stringify(api.sent("PUT", putPath)[0]?.body)).not.toContain(
      "the-new-secret",
    );
    // It is never echoed back into the page.
    expect(
      screen.queryByDisplayValue("the-new-secret"),
    ).not.toBeInTheDocument();
  });

  it("asks for the secret before saving rather than sending an empty one", async () => {
    api = fakeApi({});
    mount(publicProvider());
    const user = userEvent.setup();

    await user.click(radio("client_secret_post"));
    await user.click(save());

    expect(
      await screen.findByText("Enter the client secret."),
    ).toBeInTheDocument();
    expect(api.sent("POST", rotatePath)).toHaveLength(0);
    expect(api.sent("PUT", putPath)).toHaveLength(0);
  });

  it("shows a stored secret as a row that replaces it, and draws no secret field", async () => {
    const user = userEvent.setup();
    api = fakeApi({});
    mount(manualProvider());

    expect(screen.getByText("Stored")).toBeVisible();
    expect(screen.queryByLabelText("Client secret")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Replace" }));
    expect(
      await screen.findByRole("dialog", { name: "Replace the client secret" }),
    ).toBeVisible();
  });

  it("draws neither the row nor the field for a public client", () => {
    mount(publicProvider());

    expect(screen.queryByText("Stored")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Client secret")).not.toBeInTheDocument();
  });

  it("draws no secret field once the provider holds one, and sends no secret", async () => {
    const provider = publicProvider({
      secretConfigured: true,
      secretStatus: "valid",
    });
    api = fakeApi({
      [`PUT ${putPath}`]: () => Response.json(provider),
    });
    mount(provider);
    const user = userEvent.setup();

    await user.click(radio("client_secret_basic"));
    await user.click(save());
    await waitFor(() => expect(api.sent("PUT", putPath)).toHaveLength(1));
    expect(screen.queryByLabelText("Client secret")).not.toBeInTheDocument();
    expect(api.sent("POST", rotatePath)).toHaveLength(0);
  });

  it("re-reads the provider when the save fails after the secret landed", async () => {
    const provider = publicProvider();
    let saveAttempts = 0;
    api = fakeApi({
      [`POST ${rotatePath}`]: () => new Response(null, { status: 204 }),
      [`PUT ${putPath}`]: () => {
        saveAttempts += 1;
        return apiError("bad_request", 400);
      },
      [`GET ${putPath}`]: () =>
        // The re-read reports the secret the rotation stored, so the page
        // stops asking for one.
        Response.json({
          ...provider,
          secretConfigured: true,
          secretStatus: "valid",
        }),
    });
    mount(provider);
    const user = userEvent.setup();

    await user.click(radio("client_secret_basic"));
    await user.type(screen.getByLabelText("Client secret"), "the-new-secret");
    await user.click(save());

    await waitFor(() => expect(saveAttempts).toBe(1));
    await waitFor(() => expect(api.sent("GET", putPath)).toHaveLength(1));
  });
});

describe("the connection form's sign-in response", () => {
  it("shows a provider saved without the setting as the authorization code, and sends the choice", async () => {
    const provider = publicProvider();
    api = fakeApi({ [`PUT ${putPath}`]: () => Response.json(provider) });
    mount(provider);
    const user = userEvent.setup();

    expect(radio("Authorization code")).toBeChecked();
    expect(radio("ID token + access token")).not.toBeChecked();

    await user.click(radio("ID token only"));
    await user.click(save());

    await waitFor(() => expect(api.sent("PUT", putPath)).toHaveLength(1));
    expect(savedConfig().responseType).toBe("id_token");
  });

  it("keeps the Keycloak and Microsoft note beside the choices", () => {
    mount(publicProvider());
    expect(
      screen.getByText(/In Keycloak this is Implicit Flow Enabled/),
    ).toBeVisible();
  });

  it("refuses an implicit response without the openid scope or a JWKS address", async () => {
    api = fakeApi({});
    mount(manualProvider({ scopes: ["profile"] }));
    const user = userEvent.setup();

    await user.click(radio("ID token only"));
    await user.click(save());
    expect(
      await screen.findByText(/needs the openid scope/),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Add scope" }));
    await user.type(
      await screen.findByRole("textbox", { name: "Add scope" }),
      "openid{Enter}",
    );
    await user.keyboard("{Escape}");
    await user.click(radio("None — skip the ID token"));
    await user.click(save());
    expect(
      await screen.findByText(/needs a JWKS endpoint to check the token/),
    ).toBeInTheDocument();
    expect(api.sent("PUT", putPath)).toHaveLength(0);
  });
});

describe("the connection form's scopes", () => {
  it("saves the scopes as typed without openid, and an empty list as []", async () => {
    const provider = publicProvider();
    api = fakeApi({ [`PUT ${putPath}`]: () => Response.json(provider) });
    mount(provider);
    const user = userEvent.setup();

    for (const name of ["openid", "profile"]) {
      await user.click(screen.getByRole("button", { name: `Remove ${name}` }));
    }
    await user.click(save());

    await waitFor(() => expect(api.sent("PUT", putPath)).toHaveLength(1));
    expect(savedConfig().scopes).toEqual([]);
  });

  it("states what the provider must support", () => {
    mount(publicProvider());
    expect(
      screen.getByText("The provider must support every scope listed here."),
    ).toBeVisible();
  });
});

describe("the connection form's endpoints", () => {
  it("draws discovery with its overrides as removable rows, and sends only what is shown", async () => {
    const provider = publicProvider({
      config: {
        ...publicOidcConfig,
        endpoints: {
          authorization: "https://idp.example.com/authorize",
          token: "https://idp.example.com/token",
          userinfo: null,
          jwks: null,
        },
      },
    });
    api = fakeApi({ [`PUT ${putPath}`]: () => Response.json(provider) });
    mount(provider);
    const user = userEvent.setup();

    expect(radio("Discover automatically")).toBeChecked();
    expect(
      screen.getByText("Addresses entered here replace the discovered ones."),
    ).toBeVisible();
    expect(screen.getByLabelText("Authorization endpoint")).toHaveValue(
      "https://idp.example.com/authorize",
    );
    expect(screen.getByLabelText("Token endpoint")).toHaveValue(
      "https://idp.example.com/token",
    );
    expect(
      screen.queryByLabelText("UserInfo endpoint"),
    ).not.toBeInTheDocument();

    await user.click(
      screen.getByRole("button", {
        name: "Remove the Token endpoint override",
      }),
    );
    expect(screen.queryByLabelText("Token endpoint")).not.toBeInTheDocument();
    await user.click(save());

    await waitFor(() => expect(api.sent("PUT", putPath)).toHaveLength(1));
    expect(savedConfig().endpoints).toEqual({
      authorization: "https://idp.example.com/authorize",
      token: null,
      userinfo: null,
      jwks: null,
    });
  });

  it("adds an override from the menu, and offers only the endpoints not yet overridden", async () => {
    mount(publicProvider());
    const user = userEvent.setup();

    await user.click(
      screen.getByRole("button", { name: "Override an endpoint" }),
    );
    const menu = await screen.findByRole("menu");
    expect(within(menu).getAllByRole("menuitem")).toHaveLength(4);
    await user.click(
      within(menu).getByRole("menuitem", { name: "UserInfo endpoint" }),
    );

    expect(await screen.findByLabelText("UserInfo endpoint")).toBeVisible();
    await user.click(
      screen.getByRole("button", { name: "Override an endpoint" }),
    );
    expect(
      within(await screen.findByRole("menu")).getAllByRole("menuitem"),
    ).toHaveLength(3);
  });

  it("refuses an empty override row, naming the row", async () => {
    api = fakeApi({});
    mount(publicProvider());
    const user = userEvent.setup();

    await user.click(
      screen.getByRole("button", { name: "Override an endpoint" }),
    );
    await user.click(
      within(await screen.findByRole("menu")).getByRole("menuitem", {
        name: "Token endpoint",
      }),
    );
    await user.click(save());

    expect(
      await screen.findByText("Enter the address, or remove this override."),
    ).toBeVisible();
    expect(api.sent("PUT", putPath)).toHaveLength(0);
  });

  it("requires the authorization and token endpoints by hand, and a JWKS choice", async () => {
    api = fakeApi({});
    mount(
      manualProvider({
        endpoints: {
          authorization: null,
          token: null,
          userinfo: null,
          jwks: null,
        },
      }),
    );
    const user = userEvent.setup();

    await user.type(screen.getByLabelText("Client ID"), "x");
    await user.click(save());

    expect(
      await screen.findByText("Enter the authorization endpoint."),
    ).toBeVisible();
    expect(screen.getByText("Enter the token endpoint.")).toBeVisible();
    expect(
      screen.getByText(
        "Enter the JWKS endpoint, or choose None to skip the ID token.",
      ),
    ).toBeVisible();
    expect(api.sent("PUT", putPath)).toHaveLength(0);
  });

  it("offers JWKS as an address or an explicit none, never a word to type", async () => {
    const provider = manualProvider();
    api = fakeApi({ [`PUT ${putPath}`]: () => Response.json(provider) });
    mount(provider);
    const user = userEvent.setup();

    expect(radio("Check the ID token with a JWKS address")).toBeChecked();
    expect(screen.getByRole("textbox", { name: "JWKS endpoint" })).toHaveValue(
      "https://idp.example.com/jwks",
    );

    await user.click(radio("None — skip the ID token"));
    expect(radio("None — skip the ID token")).toBeChecked();
    expect(
      screen.queryByRole("textbox", { name: "JWKS endpoint" }),
    ).not.toBeInTheDocument();

    // Changing their mind brings back what was typed.
    await user.click(radio("Check the ID token with a JWKS address"));
    expect(screen.getByRole("textbox", { name: "JWKS endpoint" })).toHaveValue(
      "https://idp.example.com/jwks",
    );
  });

  it("makes UserInfo required once JWKS is none, and sends none as its own value", async () => {
    const provider = manualProvider();
    api = fakeApi({ [`PUT ${putPath}`]: () => Response.json(provider) });
    mount(provider);
    const user = userEvent.setup();

    await user.click(radio("None — skip the ID token"));
    await user.click(save());
    expect(
      await screen.findByText(
        "Enter the UserInfo endpoint. It is used to identify people when JWKS is set to None.",
      ),
    ).toBeVisible();
    expect(api.sent("PUT", putPath)).toHaveLength(0);

    await user.type(
      screen.getByRole("textbox", { name: "UserInfo endpoint" }),
      "https://idp.example.com/userinfo",
    );
    await user.click(save());

    await waitFor(() => expect(api.sent("PUT", putPath)).toHaveLength(1));
    expect(savedConfig().endpoints).toEqual({
      authorization: "https://idp.example.com/authorize",
      token: "https://idp.example.com/token",
      userinfo: "https://idp.example.com/userinfo",
      jwks: "none",
    });
  });

  it("reads a saved none as the explicit choice", () => {
    mount(
      manualProvider({
        endpoints: {
          authorization: "https://idp.example.com/authorize",
          token: "https://idp.example.com/token",
          userinfo: "https://idp.example.com/userinfo",
          jwks: "none",
        },
      }),
    );
    expect(radio("None — skip the ID token")).toBeChecked();
  });
});

describe("the connection form's avatar authentication", () => {
  it("saves the switch on, and keeps it off for a provider saved without the setting", async () => {
    const provider = publicProvider();
    api = fakeApi({ [`PUT ${putPath}`]: () => Response.json(provider) });
    mount(provider);
    const user = userEvent.setup();

    const switchEl = screen.getByRole("switch", {
      name: "Send the access token when fetching the avatar",
    });
    expect(switchEl).not.toBeChecked();
    expect(
      screen.getByText(/Turn this on only if you trust the provider/),
    ).toBeVisible();

    await user.click(switchEl);
    await user.click(save());

    await waitFor(() => expect(api.sent("PUT", putPath)).toHaveLength(1));
    expect(savedConfig().authenticateAvatarFetch).toBe(true);
  });
});

describe("the callback addresses", () => {
  it("lists the three addresses to register, each copyable", async () => {
    mount(publicProvider());
    const user = userEvent.setup();
    const writeText = vi.spyOn(navigator.clipboard, "writeText");
    const origin = window.location.origin;

    const group = screen.getByRole("group", { name: "Callback addresses" });
    expect(
      within(group).getByText("Register these with the provider."),
    ).toBeVisible();
    const expected: [string, string][] = [
      [
        "Copy the sign-in address",
        `${origin}/api/prohibitorum/auth/federation/${slug}/callback`,
      ],
      [
        "Copy the account linking address",
        `${origin}/api/prohibitorum/me/identities/link/${slug}/callback`,
      ],
      [
        "Copy the connection test address",
        `${origin}/api/prohibitorum/auth/federation/${slug}/test/callback`,
      ],
    ];
    for (const [button, address] of expected) {
      expect(
        within(group).getByText(address, { normalizer: (text) => text }),
      ).toBeVisible();
      await user.click(within(group).getByRole("button", { name: button }));
      expect(writeText).toHaveBeenLastCalledWith(address);
    }
  });
});

describe("saving and discarding", () => {
  it("offers Save and Discard only while something differs", async () => {
    mount(publicProvider());
    const user = userEvent.setup();

    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
    await user.clear(screen.getByLabelText("Client ID"));
    await user.type(screen.getByLabelText("Client ID"), "other");
    expect(save()).toBeVisible();

    await user.click(screen.getByRole("button", { name: "Discard" }));
    expect(screen.getByLabelText("Client ID")).toHaveValue("client");
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("sends the whole stored configuration with the edit merged in", async () => {
    const provider = publicProvider();
    api = fakeApi({ [`PUT ${putPath}`]: () => Response.json(provider) });
    mount(provider);
    const user = userEvent.setup();

    await user.clear(screen.getByLabelText("Client ID"));
    await user.type(screen.getByLabelText("Client ID"), "other");
    await user.click(save());

    await waitFor(() => expect(api.sent("PUT", putPath)).toHaveLength(1));
    const config = savedConfig();
    expect(config.clientId).toBe("other");
    // The fields this section does not edit go out as stored.
    expect(config.usernameClaim).toBe("preferred_username");
    expect(config.pkceMethod).toBe("S256");
    expect(config.allowPrivateNetwork).toBe(false);
  });
});

describe("the connection form's validation", () => {
  it("names the issuer and the client ID when they are empty", async () => {
    api = fakeApi({});
    mount(publicProvider());
    const user = userEvent.setup();

    await user.clear(screen.getByLabelText("Issuer URL"));
    await user.clear(screen.getByLabelText("Client ID"));
    await user.click(save());

    expect(await screen.findByText("Enter the issuer URL.")).toBeVisible();
    expect(screen.getByText("Enter the client ID.")).toBeVisible();
    expect(api.sent("PUT", putPath)).toHaveLength(0);
  });

  it("refuses an issuer the server would, with its own reason", async () => {
    api = fakeApi({});
    mount(publicProvider());
    const user = userEvent.setup();

    await user.clear(screen.getByLabelText("Issuer URL"));
    await user.type(
      screen.getByLabelText("Issuer URL"),
      "http://idp.example.com",
    );
    await user.click(save());

    expect(await screen.findByText("Use an https address.")).toBeVisible();
    expect(api.sent("PUT", putPath)).toHaveLength(0);
  });

  it("refuses text typed into the scope box and never added", async () => {
    api = fakeApi({});
    mount(publicProvider());
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: "Add scope" }));
    await user.type(
      await screen.findByRole("textbox", { name: "Add scope" }),
      "email",
    );
    await user.keyboard("{Escape}");
    await user.click(save());

    expect(
      await screen.findByText("Press Enter to add it, or clear the box."),
    ).toBeVisible();
    expect(api.sent("PUT", putPath)).toHaveLength(0);
  });
});

describe("a refused write", () => {
  it("reports a refused save once, on the section", async () => {
    api = fakeApi({
      [`PUT ${putPath}`]: () => apiError("internal_error", 500),
    });
    mount(publicProvider());
    const user = userEvent.setup();

    await user.clear(screen.getByLabelText("Client ID"));
    await user.type(screen.getByLabelText("Client ID"), "other");
    await user.click(save());

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("puts the server's bare refusal on the issuer, once", async () => {
    api = fakeApi({
      [`PUT ${putPath}`]: () => apiError("bad_request", 400),
    });
    mount(publicProvider());
    const user = userEvent.setup();

    await user.clear(screen.getByLabelText("Client ID"));
    await user.type(screen.getByLabelText("Client ID"), "other");
    await user.click(save());

    await waitFor(() =>
      expect(screen.getByLabelText("Issuer URL")).toBeInvalid(),
    );
    expect(screen.queryAllByRole("alert")).toHaveLength(0);
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("reports a refused secret once, and stops before the configuration", async () => {
    api = fakeApi({
      [`POST ${rotatePath}`]: () => apiError("internal_error", 500),
    });
    mount(publicProvider());
    const user = userEvent.setup();

    await user.click(radio("client_secret_basic"));
    await user.type(screen.getByLabelText("Client secret"), "the-new-secret");
    await user.click(save());

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
    expect(api.sent("PUT", putPath)).toHaveLength(0);
  });
});
