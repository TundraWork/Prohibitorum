import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { ProviderCredentialsSection } from "@/pages/admin/identity-providers/ProviderCredentialsSection";
import { type Provider, providerFixture } from "@/test/identity-provider";

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

function rotations() {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter(
      (request) =>
        request.method === "POST" &&
        new URL(request.url).pathname.endsWith("/rotate-secret"),
    );
}

function mount(
  provider: Provider,
  extra?: (request: Request) => Response | Promise<Response> | undefined,
) {
  fetchBoundary.mockImplementation(async (request) => {
    const canned = extra?.(request);
    if (canned) return canned;
    return new Response(null, { status: 204 });
  });
  return render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <ProviderCredentialsSection provider={provider} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

const steam = (overrides: Partial<Provider> = {}) =>
  providerFixture({
    protocol: "steam",
    secretConfigured: false,
    secretStatus: "unconfigured",
    ...overrides,
  });

describe("the Steam Web API key", () => {
  it("is its own section, and says the key is not set", () => {
    mount(steam());

    expect(
      screen.getByRole("heading", { level: 2, name: "Web API key" }),
    ).toBeVisible();
    expect(screen.getByText("Not set")).toBeVisible();
    expect(screen.getByRole("button", { name: "Set" })).toBeVisible();
  });

  it("says the key is stored and offers to replace it", () => {
    mount(steam({ secretConfigured: true, secretStatus: "valid" }));

    expect(screen.getByText("Stored")).toBeVisible();
    expect(screen.getByRole("button", { name: "Replace" })).toBeVisible();
  });

  it("sets the key from a write-only dialog and closes it", async () => {
    const user = userEvent.setup();
    mount(steam());

    await user.click(screen.getByRole("button", { name: "Set" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Set the Web API key",
    });
    expect(
      within(dialog).getByText("Stored encrypted and never shown again."),
    ).toBeVisible();
    const field = within(dialog).getByLabelText("Web API key");
    expect(field).toHaveAttribute("type", "password");
    await user.type(field, "ABC123");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => expect(rotations()).toHaveLength(1));
    expect(await rotations()[0]?.json()).toEqual({
      slug: "corp",
      secret: "ABC123",
    });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("names the dialog for replacing when a key is stored", async () => {
    const user = userEvent.setup();
    mount(steam({ secretConfigured: true, secretStatus: "valid" }));

    await user.click(screen.getByRole("button", { name: "Replace" }));
    expect(
      await screen.findByRole("dialog", { name: "Replace the Web API key" }),
    ).toBeVisible();
  });

  it("refuses an empty key without sending", async () => {
    const user = userEvent.setup();
    mount(steam());

    await user.click(screen.getByRole("button", { name: "Set" }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    expect(await within(dialog).findByText("Enter the secret.")).toBeVisible();
    expect(rotations()).toHaveLength(0);
  });

  it("sends nothing when cancelled, and starts empty the next time", async () => {
    const user = userEvent.setup();
    mount(steam());

    await user.click(screen.getByRole("button", { name: "Set" }));
    let dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText("Web API key"), "ABC123");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(rotations()).toHaveLength(0);

    await user.click(screen.getByRole("button", { name: "Set" }));
    dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByLabelText("Web API key")).toHaveValue("");
  });

  it("ignores dismissal while the key is being saved", async () => {
    const user = userEvent.setup();
    let release: (response: Response) => void = () => {};
    mount(
      steam(),
      () =>
        new Promise<Response>((resolve) => {
          release = resolve;
        }),
    );

    await user.click(screen.getByRole("button", { name: "Set" }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText("Web API key"), "ABC123");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(rotations()).toHaveLength(1));
    await user.keyboard("{Escape}");

    expect(screen.getByRole("dialog")).toBeVisible();
    release(new Response(null, { status: 204 }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("reports a refused key once, inside the dialog", async () => {
    const user = userEvent.setup();
    mount(steam(), () =>
      Response.json({ code: "internal_error" }, { status: 500 }),
    );

    await user.click(screen.getByRole("button", { name: "Set" }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText("Web API key"), "ABC123");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    expect(await within(dialog).findAllByRole("alert")).toHaveLength(1);
    expect(screen.getByRole("dialog")).toBeVisible();
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("an OIDC provider", () => {
  it("has no section here: its client secret is set in the connection", () => {
    const { container } = mount(providerFixture({ secretConfigured: true }));
    expect(container).toBeEmptyDOMElement();
  });
});

describe("a VRChat provider", () => {
  it("has no secret to set here; its operator session is its own section", () => {
    const { container } = mount(providerFixture({ protocol: "vrchat" }));
    expect(container).toBeEmptyDOMElement();
  });
});
