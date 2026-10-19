import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { SamlMetadataSection } from "@/pages/admin/saml-applications/SamlMetadataSection";
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

function posts() {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter(
      (request) =>
        request.method === "POST" &&
        new URL(request.url).pathname.endsWith("/reingest-metadata"),
    );
}

function mount(
  app: SamlApp,
  extra?: (request: Request) => Response | Promise<Response> | undefined,
) {
  fetchBoundary.mockImplementation(async (request) => {
    const canned = extra?.(request);
    if (canned) return canned;
    if (request.method === "POST") return Response.json(app);
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <SamlMetadataSection app={app} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

async function pasteAndSubmit(user: UserEvent, document: string) {
  await user.click(
    await screen.findByRole("button", { name: "Re-import metadata" }),
  );
  const dialog = await screen.findByRole("dialog", {
    name: "Re-import metadata",
  });
  if (document !== "") {
    await user.click(
      within(dialog).getByRole("textbox", { name: "Metadata XML" }),
    );
    await user.paste(document);
  }
  await user.click(within(dialog).getByRole("button", { name: "Re-import" }));
  return dialog;
}

describe("what the metadata says", () => {
  it("lists the endpoints and the certificates read-only", async () => {
    mount(
      samlAppFixture({
        keys: [{ use: "signing", notAfter: "2031-03-15T12:00:00Z" }],
      }),
    );

    expect(
      await screen.findByRole("list", {
        name: "Endpoints and certificates",
      }),
    ).toBeVisible();
    expect(screen.getByText("Endpoint 0")).toBeVisible();
    expect(
      screen.getByText("https://wiki.example.test/saml/acs"),
    ).toBeVisible();
    expect(screen.getByText("Expires Mar 15, 2031")).toBeVisible();
    expect(screen.queryByRole("textbox")).toBeNull();
  });

  it("says plainly when there is nothing to list", async () => {
    mount(samlAppFixture({ acs: [], keys: [] }));
    expect(
      await screen.findByText("This application has no endpoints."),
    ).toBeVisible();
    expect(
      screen.getByText("No certificates are published for this application."),
    ).toBeVisible();
  });
});

describe("re-importing metadata", () => {
  it("asks for a document before anything else", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture());

    await pasteAndSubmit(user, "");

    expect(
      await screen.findByText(
        "Paste the service provider's metadata document.",
      ),
    ).toBeVisible();
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(posts()).toHaveLength(0);
  });

  it("says it replaces the endpoints and certificates, and sends only once confirmed", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture());

    await pasteAndSubmit(user, "<EntityDescriptor/>");
    const confirm = await screen.findByRole("alertdialog", {
      name: "Replace the endpoints and certificates?",
    });
    expect(
      within(confirm).getByText(/replaces every endpoint and certificate/),
    ).toBeVisible();
    expect(posts()).toHaveLength(0);
    await user.click(
      within(confirm).getByRole("button", { name: "Replace and re-import" }),
    );

    await waitFor(() => expect(posts()).toHaveLength(1));
    expect(await posts()[0]?.json()).toEqual({
      metadataXml: "<EntityDescriptor/>",
    });
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
  });

  it("sends nothing when the confirmation is cancelled and keeps the document", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture());

    const dialog = await pasteAndSubmit(user, "<EntityDescriptor/>");
    const confirm = await screen.findByRole("alertdialog");
    await user.click(within(confirm).getByRole("button", { name: "Cancel" }));

    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(posts()).toHaveLength(0);
    expect(
      within(dialog).getByRole("textbox", { name: "Metadata XML" }),
    ).toHaveValue("<EntityDescriptor/>");
  });

  it("keeps the confirmation open while the write is in flight", async () => {
    const user = userEvent.setup();
    let release: (response: Response) => void = () => {};
    mount(samlAppFixture(), (request) =>
      request.method === "POST"
        ? new Promise<Response>((resolve) => {
            release = resolve;
          })
        : undefined,
    );

    await pasteAndSubmit(user, "<EntityDescriptor/>");
    const confirm = await screen.findByRole("alertdialog");
    await user.click(
      within(confirm).getByRole("button", { name: "Replace and re-import" }),
    );
    await waitFor(() => expect(posts()).toHaveLength(1));
    await user.keyboard("{Escape}");

    expect(screen.getByRole("alertdialog")).toBeVisible();
    release(Response.json(samlAppFixture()));
  });

  it("reports a refused document once, on the form", async () => {
    const user = userEvent.setup();
    mount(samlAppFixture(), (request) =>
      request.method === "POST"
        ? Response.json({ code: "internal_error" }, { status: 500 })
        : undefined,
    );

    const dialog = await pasteAndSubmit(user, "<nonsense/>");
    const confirm = await screen.findByRole("alertdialog");
    await user.click(
      within(confirm).getByRole("button", { name: "Replace and re-import" }),
    );

    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(await within(dialog).findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});
