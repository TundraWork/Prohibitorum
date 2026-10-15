import { I18nProvider } from "@lingui/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider as AriaI18nProvider } from "react-aria";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { ProviderConnectionSection } from "@/pages/admin/identity-providers/ProviderConnectionSection";
import { type FakeApi, fakeApi } from "@/test/app";

type Provider = components["schemas"]["IdentityProviderView"];
type QueryClient = ReturnType<typeof createQueryClient>;

let queryClient: QueryClient;
let api: FakeApi;

beforeEach(() => {
  i18n.activate("en");
});

/** An OIDC provider saved as a public client, enabled and ready. */
function publicProvider(): Provider {
  return {
    slug: "public-idp",
    displayName: "Public IdP",
    protocol: "oidc",
    mode: "invite_only",
    disabled: false,
    ready: true,
    secretConfigured: false,
    secretStatus: "unconfigured",
    secretValidatedAt: null,
    createdAt: "2026-01-01T00:00:00Z",
    config: {
      issuerUrl: "https://idp.example.com",
      clientId: "client",
      scopes: ["openid", "profile"],
      allowedDomains: [],
      requireVerifiedEmail: true,
      allowPrivateNetwork: false,
      usernameClaim: "preferred_username",
      displayNameClaim: "name",
      emailClaim: "email",
      pictureClaim: "picture",
      subjectClaim: "sub",
      configurationMode: "discovery",
      endpoints: {
        authorization: null,
        token: null,
        userinfo: null,
        jwks: null,
      },
      tokenAuthMethod: "none",
      pkceMethod: "S256",
    },
    supportsOperator: false,
    searchFields: [],
    linkedAccountCount: 0,
  };
}

function mount(provider: Provider) {
  queryClient = createQueryClient(vi.fn());
  render(
    <I18nProvider i18n={i18n}>
      <AriaI18nProvider>
        <QueryClientProvider client={queryClient}>
          <ProviderConnectionSection provider={provider} />
        </QueryClientProvider>
      </AriaI18nProvider>
    </I18nProvider>,
  );
}

/** Opens the client authentication select and picks an option by name. */
async function chooseMethod(
  user: ReturnType<typeof userEvent.setup>,
  name: string,
) {
  await user.click(
    screen.getByRole("button", { name: /Client authentication/ }),
  );
  const listbox = await screen.findByRole("listbox");
  await user.click(within(listbox).getByText(name));
}

describe("the connection form's client secret", () => {
  it("draws the secret field when a public client is switched to a method that sends one, and sends it before the save", async () => {
    const provider = publicProvider();
    api = fakeApi({
      "POST /api/prohibitorum/identity-providers/rotate-secret": () =>
        new Response(null, { status: 204 }),
      "PUT /api/prohibitorum/identity-providers/public-idp": () =>
        Response.json(provider),
    });
    mount(provider);
    const user = userEvent.setup();

    expect(
      screen.queryByRole("textbox", { name: /Client secret/ }),
    ).not.toBeInTheDocument();

    await chooseMethod(user, "client_secret_basic");
    const secret = await screen.findByLabelText("Client secret");
    expect(secret).toBeInTheDocument();

    await user.type(secret, "the-new-secret");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(
        api.sent("PUT", "/api/prohibitorum/identity-providers/public-idp"),
      ).toHaveLength(1),
    );
    // The secret goes first, while the stored configuration still names the
    // old method, and the save never repeats it: the update endpoint refuses
    // a secret in its body.
    const rotated = api.sent(
      "POST",
      "/api/prohibitorum/identity-providers/rotate-secret",
    );
    expect(rotated).toHaveLength(1);
    expect(rotated[0]?.body).toEqual({
      slug: "public-idp",
      secret: "the-new-secret",
    });
    expect(
      api.sent("PUT", "/api/prohibitorum/identity-providers/public-idp")[0]
        ?.body,
    ).not.toHaveProperty("secret");
  });

  it("asks for the secret before saving rather than sending an empty one", async () => {
    const provider = publicProvider();
    api = fakeApi({});
    mount(provider);
    const user = userEvent.setup();

    await chooseMethod(user, "client_secret_post");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(
      await screen.findByText("Enter the client secret."),
    ).toBeInTheDocument();
    expect(
      api.sent("POST", "/api/prohibitorum/identity-providers/rotate-secret"),
    ).toHaveLength(0);
    expect(
      api.sent("PUT", "/api/prohibitorum/identity-providers/public-idp"),
    ).toHaveLength(0);
  });

  it("draws no secret field once the provider holds one", async () => {
    const provider = {
      ...publicProvider(),
      secretConfigured: true,
      secretStatus: "configured",
    };
    api = fakeApi({
      "PUT /api/prohibitorum/identity-providers/public-idp": () =>
        Response.json(provider),
    });
    mount(provider);
    const user = userEvent.setup();

    await chooseMethod(user, "client_secret_basic");
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(
        api.sent("PUT", "/api/prohibitorum/identity-providers/public-idp"),
      ).toHaveLength(1),
    );
    expect(
      screen.queryByRole("textbox", { name: /Client secret/ }),
    ).not.toBeInTheDocument();
    expect(
      api.sent("POST", "/api/prohibitorum/identity-providers/rotate-secret"),
    ).toHaveLength(0);
  });
});
