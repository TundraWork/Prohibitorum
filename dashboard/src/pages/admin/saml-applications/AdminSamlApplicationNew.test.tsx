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
import { samlAppFixture } from "@/test/saml-app";

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

function serve(create: () => Response | Promise<Response>): FakeApi {
  const created = samlAppFixture({ id: 9, displayName: "Wiki" });
  return fakeApi({
    ...publicApi({ ...testSession, role: "admin" }),
    [`POST ${base}/saml-applications`]: create,
    [`GET ${base}/saml-applications`]: () =>
      Response.json({ items: [created], nextCursor: "" }),
    [`GET ${base}/saml-applications/9`]: () => Response.json(created),
    [`GET ${base}/managed-applications/saml/9/access`]: () =>
      Response.json({
        app: {
          kind: "saml",
          appId: "9",
          displayName: "Wiki",
          accessRestricted: false,
        },
        accessRestricted: false,
        providers: [],
        groups: [],
      }),
    [`GET ${base}/groups`]: () => Response.json([]),
    [`GET ${base}/saml-applications/9/managers`]: () => Response.json([]),
    [`GET ${base}/oidc-applications`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${base}/forward-auth-apps`]: () =>
      Response.json({ items: [], nextCursor: "" }),
  });
}

async function mountForm() {
  const user = userEvent.setup();
  const router = renderApp("/admin/saml-applications/new", queryClient);
  await screen.findByRole("heading", {
    level: 1,
    name: "New SAML application",
  });
  return { user, router };
}

const created = () =>
  Response.json(samlAppFixture({ id: 9, displayName: "Wiki" }), {
    status: 201,
  });

async function typeName(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByRole("textbox", { name: "Name" }), "Wiki");
}

async function pasteDocument(
  user: ReturnType<typeof userEvent.setup>,
  document = "<EntityDescriptor/>",
) {
  await user.click(screen.getByRole("textbox", { name: "Metadata XML" }));
  await user.paste(document);
}

async function chooseManual(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("radio", { name: "Enter it by hand" }));
}

const submit = () => screen.getByRole("button", { name: "Create application" });

describe("the form's shape", () => {
  it("groups the fields into sections a reader can name", async () => {
    serve(created);
    await mountForm();

    expect(
      screen
        .getAllByRole("heading", { level: 2 })
        .map((heading) => heading.textContent),
    ).toEqual([
      "Application",
      "Registration",
      "Service provider",
      "Sign-in",
      "Access",
    ]);
  });

  it("offers importing metadata first, and chosen, with the other way described", async () => {
    serve(created);
    await mountForm();

    const group = screen.getByRole("radiogroup", {
      name: "How the application is registered",
    });
    const metadata = within(group).getByRole("radio", {
      name: "Import metadata",
    });
    expect(metadata).toBeChecked();
    expect(
      within(group).getByRole("radio", { name: "Enter it by hand" }),
    ).not.toBeChecked();
    expect(screen.getByRole("textbox", { name: "Metadata XML" })).toBeVisible();
    expect(screen.queryByRole("textbox", { name: /^Entity ID/ })).toBeNull();
  });

  it("asks for the Entity ID, the NameID format and the endpoints when entered by hand", async () => {
    serve(created);
    const { user } = await mountForm();

    await chooseManual(user);

    expect(screen.getByRole("textbox", { name: /^Entity ID/ })).toBeVisible();
    expect(
      screen.getByRole("combobox", { name: "NameID format" }),
    ).toHaveTextContent("Use the instance default");
    expect(screen.getByRole("textbox", { name: "Address" })).toBeVisible();
    expect(screen.queryByRole("textbox", { name: "Metadata XML" })).toBeNull();
  });

  it("states the restriction's consequence beside its switch", async () => {
    serve(created);
    await mountForm();

    expect(
      screen.getByRole("switch", { name: "Restrict access" }),
    ).toHaveAccessibleDescription(
      /Nobody can use the application until you select a user group/,
    );
  });
});

describe("validation", () => {
  it("asks for a name, then for a document, before sending anything", async () => {
    const api = serve(created);
    const { user } = await mountForm();

    await user.click(submit());
    expect(await screen.findByText("Enter a name.")).toBeVisible();

    await typeName(user);
    await user.click(submit());
    expect(
      await screen.findByText(
        "Paste the service provider's metadata document.",
      ),
    ).toBeVisible();
    expect(api.sent("POST", `${base}/saml-applications`)).toHaveLength(0);
  });

  it("asks for the Entity ID when entered by hand", async () => {
    const api = serve(created);
    const { user } = await mountForm();

    await chooseManual(user);
    await typeName(user);
    await user.type(
      screen.getByRole("textbox", { name: "Address" }),
      "https://wiki.example.test/acs",
    );
    await user.click(submit());

    expect(
      await screen.findByText("Enter the service provider's Entity ID."),
    ).toBeVisible();
    expect(api.sent("POST", `${base}/saml-applications`)).toHaveLength(0);
  });

  it("refuses an endpoint address that is not https", async () => {
    const api = serve(created);
    const { user } = await mountForm();

    await chooseManual(user);
    await typeName(user);
    await user.type(
      screen.getByRole("textbox", { name: /^Entity ID/ }),
      "https://wiki.example.test",
    );
    await user.type(
      screen.getByRole("textbox", { name: "Address" }),
      "http://wiki.example.test/acs",
    );
    await user.click(submit());

    expect(
      await screen.findByText("Use an absolute https address."),
    ).toBeVisible();
    expect(api.sent("POST", `${base}/saml-applications`)).toHaveLength(0);
  });

  it("refuses a document too large to send, on the document", async () => {
    const api = serve(created);
    const { user } = await mountForm();

    await typeName(user);
    await pasteDocument(user, "x".repeat(70 * 1024));
    await user.click(submit());

    expect(await screen.findByText(/too large to send/)).toBeVisible();
    expect(api.sent("POST", `${base}/saml-applications`)).toHaveLength(0);
  });
});

describe("creating", () => {
  it("imports a metadata document and goes to the application's page", async () => {
    const api = serve(created);
    const { user, router } = await mountForm();

    await typeName(user);
    await pasteDocument(user);
    await user.click(submit());

    await screen.findByRole("heading", { level: 1, name: "Wiki" });
    expect(router.state.location.pathname).toBe("/admin/saml-applications/9");
    expect(api.sent("POST", `${base}/saml-applications`)[0]?.body).toEqual({
      displayName: "Wiki",
      metadataXml: "<EntityDescriptor/>",
      requireSignedAuthnRequest: false,
      allowIdpInitiated: false,
      accessRestricted: false,
    });
  });

  it("enters an application by hand with its endpoint", async () => {
    const api = serve(created);
    const { user } = await mountForm();

    await chooseManual(user);
    await typeName(user);
    await user.type(
      screen.getByRole("textbox", { name: /^Entity ID/ }),
      "https://wiki.example.test",
    );
    await user.type(
      screen.getByRole("textbox", { name: "Address" }),
      "https://wiki.example.test/acs",
    );
    await user.click(screen.getByRole("switch", { name: "Restrict access" }));
    await user.click(submit());

    await screen.findByRole("heading", { level: 1, name: "Wiki" });
    expect(api.sent("POST", `${base}/saml-applications`)[0]?.body).toEqual({
      displayName: "Wiki",
      entityId: "https://wiki.example.test",
      nameIdFormat: "",
      acs: [
        {
          binding: "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST",
          location: "https://wiki.example.test/acs",
          index: 0,
          isDefault: true,
        },
      ],
      requireSignedAuthnRequest: false,
      allowIdpInitiated: false,
      accessRestricted: true,
    });
  });

  it("keeps what was typed for the other way while the first is chosen", async () => {
    serve(created);
    const { user } = await mountForm();

    await pasteDocument(user, "<kept/>");
    await chooseManual(user);
    await user.click(screen.getByRole("radio", { name: "Import metadata" }));

    expect(screen.getByRole("textbox", { name: "Metadata XML" })).toHaveValue(
      "<kept/>",
    );
  });

  it("puts a taken Entity ID on its field, and says it once", async () => {
    serve(() => apiError("saml_application_already_exists", 409));
    const { user } = await mountForm();

    await chooseManual(user);
    await typeName(user);
    await user.type(
      screen.getByRole("textbox", { name: /^Entity ID/ }),
      "https://wiki.example.test",
    );
    await user.type(
      screen.getByRole("textbox", { name: "Address" }),
      "https://wiki.example.test/acs",
    );
    await user.click(submit());

    const field = screen.getByRole("textbox", { name: /^Entity ID/ });
    await waitFor(() => expect(field).toHaveAttribute("aria-invalid", "true"));
    expect(field).toHaveAccessibleDescription(/already/i);
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("goes back to the list on Cancel without sending anything", async () => {
    const api = serve(created);
    const { user, router } = await mountForm();

    await user.click(screen.getByRole("link", { name: "Cancel" }));

    await screen.findByRole("heading", { level: 1, name: "SAML applications" });
    expect(router.state.location.pathname).toBe("/admin/saml-applications");
    expect(api.sent("POST", `${base}/saml-applications`)).toHaveLength(0);
  });
});

describe("the metadata way's refusals", () => {
  it("puts a taken Entity ID on the form once, not on a field the reader cannot see", async () => {
    serve(() => apiError("saml_application_already_exists", 409));
    const { user } = await mountForm();

    await typeName(user);
    await pasteDocument(user);
    await user.click(submit());

    const alerts = await screen.findAllByRole("alert");
    expect(alerts).toHaveLength(1);
    expect(alerts[0]).toHaveTextContent(
      "An application with that Entity ID already exists.",
    );
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("puts a document the server cannot read on the metadata box", async () => {
    serve(() => apiError("bad_request", 400));
    const { user } = await mountForm();

    await typeName(user);
    await pasteDocument(user, "<nonsense/>");
    await user.click(submit());

    const box = screen.getByRole("textbox", { name: "Metadata XML" });
    await waitFor(() => expect(box).toHaveAttribute("aria-invalid", "true"));
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("the list afterwards", () => {
  it("is invalidated by a creation, so it shows the new application", async () => {
    serve(created);
    const { user } = await mountForm();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    await typeName(user);
    await pasteDocument(user);
    await user.click(submit());
    await screen.findByRole("heading", { level: 1, name: "Wiki" });

    expect(invalidate).toHaveBeenCalledWith(
      expect.objectContaining({ queryKey: ["admin", "saml-applications"] }),
    );
  });
});
