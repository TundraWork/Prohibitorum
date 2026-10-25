import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { client } from "@/api/client";
import { publicConfigQueryOptions } from "@/api/queries";
import type { PublicConfig } from "@/api/raw-paths";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { GeneralPanel } from "@/pages/admin/settings/GeneralPanel";
import { publicConfigFixture } from "@/test/public-config";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifyError = vi.fn<(error: unknown) => void>();
const notifySuccess = vi.fn<(message: MessageDescriptor) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifyError.mockReset();
  notifySuccess.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(notifyError, notifySuccess);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

// jsdom's File cannot be the body of the test environment's Request, so the
// upload goes out as the file's text; the rest of the write runs as it is.
const send = client.PUT;
vi.spyOn(client, "PUT").mockImplementation((async (
  path: string,
  init: { body?: unknown },
) => {
  if (init.body instanceof File) {
    return send(
      path as never,
      { ...init, body: await init.body.text() } as never,
    );
  }
  return send(path as never, init as never);
}) as never);

const settingsPath = "/api/prohibitorum/admin/settings";
const iconPath = `${settingsPath}/icon`;

function requests(method: string, path: string) {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter(
      (request) =>
        request.method === method && new URL(request.url).pathname === path,
    );
}

function mount(
  config: PublicConfig = publicConfigFixture(),
  answer?: (request: Request) => Response | undefined,
) {
  queryClient.setQueryData(publicConfigQueryOptions().queryKey, config);
  fetchBoundary.mockImplementation(async (request) => {
    const canned = answer?.(request);
    if (canned) return canned;
    if (request.method === "GET") return Response.json(config);
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <GeneralPanel />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

const nameField = () => screen.getByRole("textbox", { name: "Name" });
const fileInput = () => {
  const input = document.querySelector<HTMLInputElement>('input[type="file"]');
  if (input === null) throw new Error("no file input");
  return input;
};

describe("the general section", () => {
  it("has a General heading, the name with its hint, and no footer until something changes", () => {
    mount(publicConfigFixture({ instanceName: "Acme ID" }));

    expect(
      screen.getByRole("heading", { level: 2, name: "General" }),
    ).toBeVisible();
    expect(nameField()).toHaveValue("Acme ID");
    expect(
      screen.getByText(
        "Leave empty to use the name from the deployment configuration.",
      ),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Discard" })).toBeNull();
  });

  it("saves a changed name from the footer, once", async () => {
    const user = userEvent.setup();
    mount();

    await user.clear(nameField());
    await user.type(nameField(), "Acme ID");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(requests("PUT", settingsPath)).toHaveLength(1));
    expect(await requests("PUT", settingsPath)[0]?.json()).toEqual({
      instanceName: "Acme ID",
    });
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Save" })).toBeNull(),
    );
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("puts the name back with Discard, sending nothing", async () => {
    const user = userEvent.setup();
    mount(publicConfigFixture({ instanceName: "Acme ID" }));

    await user.type(nameField(), " Two");
    await user.click(screen.getByRole("button", { name: "Discard" }));

    expect(nameField()).toHaveValue("Acme ID");
    expect(requests("PUT", settingsPath)).toHaveLength(0);
  });

  it("refuses a name over 64 characters with the limit, and sends nothing", async () => {
    const user = userEvent.setup();
    mount();

    await user.clear(nameField());
    await user.click(nameField());
    await user.paste("x".repeat(65));
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(
      await screen.findByText("Use 64 characters or fewer."),
    ).toBeVisible();
    expect(requests("PUT", settingsPath)).toHaveLength(0);
  });

  it("reports a refused name once, on the form and not as a toast", async () => {
    const user = userEvent.setup();
    mount(publicConfigFixture(), (request) =>
      request.method === "PUT"
        ? Response.json(
            { code: "bad_request", requestId: "general-test" },
            { status: 400 },
          )
        : undefined,
    );

    await user.type(nameField(), "x");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(requests("PUT", settingsPath)).toHaveLength(1));
    expect(await screen.findByText("The request is not valid.")).toBeVisible();
    // The refusal is the name field's own error, not a form-level one.
    expect(nameField()).toBeInvalid();
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("uploads a chosen icon at once, with no Save", async () => {
    const user = userEvent.setup();
    mount();

    expect(
      screen.getByRole("button", { name: "Upload an icon" }),
    ).toBeVisible();
    await user.upload(
      fileInput(),
      new File(["png"], "logo.png", { type: "image/png" }),
    );

    await waitFor(() => expect(requests("PUT", iconPath)).toHaveLength(1));
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("removes a custom icon at once", async () => {
    const user = userEvent.setup();
    mount(publicConfigFixture({ hasCustomIcon: true }));

    await user.click(screen.getByRole("button", { name: "Remove icon" }));

    await waitFor(() => expect(requests("DELETE", iconPath)).toHaveLength(1));
  });

  it("reports a failed icon removal once, beside the control and not as a toast", async () => {
    const user = userEvent.setup();
    mount(publicConfigFixture({ hasCustomIcon: true }), (request) =>
      request.method === "DELETE"
        ? Response.json(
            { code: "internal_error", requestId: "general-test" },
            { status: 500 },
          )
        : undefined,
    );

    await user.click(screen.getByRole("button", { name: "Remove icon" }));

    expect(await screen.findByRole("alert")).toBeVisible();
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("reports a refused icon upload once, with its message, and not as a toast", async () => {
    const user = userEvent.setup();
    mount(publicConfigFixture(), (request) =>
      request.method === "PUT" && new URL(request.url).pathname === iconPath
        ? Response.json(
            { code: "bad_request", requestId: "general-test" },
            { status: 400 },
          )
        : undefined,
    );

    await user.upload(
      fileInput(),
      new File(["png"], "logo.png", { type: "image/png" }),
    );

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("The request is not valid.");
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});
