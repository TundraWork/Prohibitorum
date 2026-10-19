import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { SamlIdentitySection } from "@/pages/admin/saml-applications/SamlIdentitySection";
import { type SamlApp, samlAppFixture } from "@/test/saml-app";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifyError.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(
    notifyError,
    vi.fn<(message: MessageDescriptor) => void>(),
  );
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

function puts() {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter((request) => request.method === "PUT");
}

function mount(
  app: SamlApp,
  extra?: (request: Request) => Response | undefined,
) {
  fetchBoundary.mockImplementation(async (request) => {
    const canned = extra?.(request);
    if (canned) return canned;
    if (request.method === "PUT") return Response.json(app);
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <SamlIdentitySection app={app} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

async function pickNameId(user: UserEvent, name: string) {
  await user.click(
    await screen.findByRole("combobox", { name: "NameID format" }),
  );
  await user.click(
    await screen.findByRole("option", { name: new RegExp(`^${name}`) }),
  );
}

async function pickSource(user: UserEvent, row: number, name: string) {
  const sources = await screen.findAllByRole("combobox", { name: "Source" });
  const trigger = sources[row];
  if (trigger === undefined) throw new Error(`no source in row ${row}`);
  await user.click(trigger);
  await user.click(
    await screen.findByRole("option", { name: new RegExp(`^${name}`) }),
  );
}

async function addAttribute(user: UserEvent) {
  await user.click(
    await screen.findByRole("button", { name: "Add an attribute" }),
  );
}

describe("the NameID format", () => {
  it("holds no footer while nothing is edited", async () => {
    mount(samlAppFixture());
    expect(
      await screen.findByRole("combobox", { name: "NameID format" }),
    ).toHaveTextContent("persistent");
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("says every service provider sees a new account and names the action", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture());

    await pickNameId(user, "transient");

    expect(
      await screen.findByText(
        "Changing the NameID format makes every service provider see a new account.",
      ),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Change NameID and save" }),
    ).toBeVisible();
  });

  it("sends the change only once it is confirmed, with the attribute map as stored", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture());

    await pickNameId(user, "transient");
    await user.click(
      screen.getByRole("button", { name: "Change NameID and save" }),
    );
    const dialog = await screen.findByRole("alertdialog", {
      name: "Change the NameID format?",
    });
    expect(puts()).toHaveLength(0);
    await user.click(
      within(dialog).getByRole("button", { name: "Change and save" }),
    );

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await puts()[0]?.json()).toMatchObject({
      nameIdFormat: "urn:oasis:names:tc:SAML:2.0:nameid-format:transient",
      attributeMap: samlAppFixture().attributeMap,
    });
  });

  it("sends nothing and keeps the choice when the confirmation is cancelled", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture());

    await pickNameId(user, "transient");
    await user.click(
      screen.getByRole("button", { name: "Change NameID and save" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));

    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(puts()).toHaveLength(0);
    expect(
      screen.getByRole("combobox", { name: "NameID format" }),
    ).toHaveTextContent("transient");
  });

  it("keeps a stored format the list does not cover, whole", async () => {
    mount(
      samlAppFixture({
        nameIdFormat: "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress",
      }),
    );
    expect(
      await screen.findByRole("combobox", { name: "NameID format" }),
    ).toHaveTextContent("emailAddress");
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("says a record with no format uses the instance default", async () => {
    mount(samlAppFixture({ nameIdFormat: "" }));
    expect(
      await screen.findByRole("combobox", { name: "NameID format" }),
    ).toHaveTextContent("Use the instance default");
  });
});

describe("the attribute map", () => {
  it("draws no example text that could be read as a value", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture({ attributeMap: [] }));

    await addAttribute(user);

    expect(
      screen.getByRole("textbox", { name: "Attribute name" }),
    ).not.toHaveAttribute("placeholder");
    expect(
      screen.getByRole("textbox", { name: "Friendly name" }),
    ).not.toHaveAttribute("placeholder");
  });

  it("reads a standard format as its short name at rest and the whole URN while editing", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture());

    const format = await screen.findByRole("combobox", {
      name: "Name format",
    });
    expect(format).toHaveValue("basic");
    await user.click(format);
    expect(format).toHaveValue(
      "urn:oasis:names:tc:SAML:2.0:attrname-format:basic",
    );
  });

  it("reads a custom format by its last segment at rest, with the whole URN described under it", async () => {
    const user = userEvent.setup();
    mount(
      samlAppFixture({
        attributeMap: [
          {
            name: "uid",
            name_format: "urn:example:names:custom",
            source: "username",
            multi: false,
          },
        ],
      }),
    );

    const format = await screen.findByRole("combobox", {
      name: "Name format",
    });
    expect(format).toHaveValue("custom");
    expect(format).toHaveAccessibleDescription("urn:example:names:custom");
    await user.click(format);
    expect(format).toHaveValue("urn:example:names:custom");
    expect(document.querySelector('[data-slot="name-format-full"]')).toBeNull();
  });

  it("offers every suggestion for a custom value too", async () => {
    const user = userEvent.setup();
    mount(
      samlAppFixture({
        attributeMap: [
          {
            name: "uid",
            name_format: "urn:example:names:custom",
            source: "username",
            multi: false,
          },
        ],
      }),
    );

    await user.click(
      await screen.findByRole("combobox", { name: "Name format" }),
    );
    await user.keyboard("{ArrowDown}");
    expect(await screen.findAllByRole("option")).toHaveLength(3);
  });

  it("sends a standard format as the whole URN, not the short name", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture());

    await user.type(
      await screen.findByRole("textbox", { name: "Attribute name" }),
      "2",
    );
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    const sent = await puts()[0]?.json();
    expect(sent.attributeMap[0].name_format).toBe(
      "urn:oasis:names:tc:SAML:2.0:attrname-format:basic",
    );
  });

  it("takes a name format typed in, and sends it whole", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture());

    const format = await screen.findByRole("combobox", {
      name: "Name format",
    });
    await user.clear(format);
    await user.type(format, "urn:example:attrname:custom");
    // Typing opens the suggestions, which hide the rest of the page from the
    // accessibility tree until they close.
    await user.keyboard("{Escape}");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    const sent = await puts()[0]?.json();
    expect(sent.attributeMap[0].name_format).toBe(
      "urn:example:attrname:custom",
    );
  });

  it("offers the standard formats as suggestions, short name over the URN", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture());

    const format = await screen.findByRole("combobox", {
      name: "Name format",
    });
    await user.click(format);
    await user.keyboard("{ArrowDown}");
    expect(await screen.findAllByRole("option")).toHaveLength(3);
    await user.click(await screen.findByRole("option", { name: /^uri/ }));

    expect(screen.getByRole("combobox", { name: "Name format" })).toHaveValue(
      "urn:oasis:names:tc:SAML:2.0:attrname-format:uri",
    );
  });

  it("saves a new attribute without a confirmation, in the shape the server stores", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture({ attributeMap: [] }));

    await addAttribute(user);
    await user.type(
      screen.getByRole("textbox", { name: "Attribute name" }),
      "uid",
    );
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(screen.queryByRole("alertdialog")).toBeNull();
    // Exactly these keys: a blank friendly name is left out, not sent empty.
    const sent = await puts()[0]?.json();
    expect(sent.attributeMap).toEqual([
      {
        name: "uid",
        name_format: "urn:oasis:names:tc:SAML:2.0:attrname-format:basic",
        source: "username",
        multi: false,
      },
    ]);
  });

  it("composes an account attribute source from its key", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture({ attributeMap: [] }));

    await addAttribute(user);
    await user.type(
      screen.getByRole("textbox", { name: "Attribute name" }),
      "team",
    );
    await pickSource(user, 0, "Account attribute");
    await user.type(
      screen.getByRole("textbox", { name: "Attribute key" }),
      "team",
    );
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await puts()[0]?.json()).toMatchObject({
      attributeMap: [{ name: "team", source: "attributes.team" }],
    });
  });

  it("removes an attribute", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture());

    await user.click(
      await screen.findByRole("button", { name: "Remove row 1" }),
    );
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await puts()[0]?.json()).toMatchObject({ attributeMap: [] });
  });

  it("asks for a name and marks the box", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture({ attributeMap: [] }));

    await addAttribute(user);
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(
      await screen.findByText("Give this attribute a name."),
    ).toBeVisible();
    expect(
      screen.getByRole("textbox", { name: "Attribute name" }),
    ).toBeInvalid();
    expect(puts()).toHaveLength(0);
  });

  it("refuses two attributes under one name", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture());

    await addAttribute(user);
    const names = screen.getAllByRole("textbox", { name: "Attribute name" });
    await user.type(names[1] as HTMLElement, "mail");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(
      await screen.findByText(
        "Another attribute already sends under this name.",
      ),
    ).toBeVisible();
    expect(puts()).toHaveLength(0);
  });

  it("asks for the key of an account attribute and marks the key box", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture({ attributeMap: [] }));

    await addAttribute(user);
    await user.type(
      screen.getByRole("textbox", { name: "Attribute name" }),
      "team",
    );
    await pickSource(user, 0, "Account attribute");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(
      await screen.findByText("Name the account attribute this reads from."),
    ).toBeVisible();
    expect(
      screen.getByRole("textbox", { name: "Attribute key" }),
    ).toBeInvalid();
    expect(puts()).toHaveLength(0);
  });

  it("reports a refused save once, on the section", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture({ attributeMap: [] }), (request) =>
      request.method === "PUT"
        ? Response.json({ code: "internal_error" }, { status: 500 })
        : undefined,
    );

    await addAttribute(user);
    await user.type(
      screen.getByRole("textbox", { name: "Attribute name" }),
      "uid",
    );
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});
