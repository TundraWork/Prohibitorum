import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { clientIpQueryOptions } from "@/api/queries";
import type { ClientIpSettings } from "@/api/raw-admin-paths";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { NetworkPanel } from "@/pages/admin/settings/NetworkPanel";

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

const clientIpPath = "/api/prohibitorum/admin/settings/client-ip";
const consequence =
  "After saving, audit logs, sessions and rate limits use the client address read this way for all new requests. An incorrect setting can put all requests under one shared rate limit.";
const forwarded: ClientIpSettings = {
  strategy: "forwarded",
  header: "",
  trustedProxies: ["10.0.0.0/8", "192.168.0.0/16"],
};

function puts() {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter(
      (request) =>
        request.method === "PUT" &&
        new URL(request.url).pathname === clientIpPath,
    );
}

function reads() {
  return fetchBoundary.mock.calls.filter(
    ([request]) =>
      request.method === "GET" &&
      new URL(request.url).pathname === clientIpPath,
  );
}

function mount(
  settings: ClientIpSettings = forwarded,
  answer?: (request: Request) => Response | Promise<Response> | undefined,
) {
  queryClient.setQueryData(clientIpQueryOptions().queryKey, settings);
  fetchBoundary.mockImplementation(async (request) => {
    const canned = answer?.(request);
    if (canned) return canned;
    if (request.method === "GET") return Response.json(settings);
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <NetworkPanel />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

const source = (name: string) => screen.getByRole("radio", { name });
const proxies = () => screen.getByRole("textbox", { name: "Trusted proxies" });
const proxies2 = () => screen.getByRole("textbox");
const header = () => screen.getByRole("textbox", { name: "Header name" });

describe("the client IP section", () => {
  it("offers the three sources with what each means, and no footer until something changes", () => {
    mount();

    expect(
      screen.getByRole("heading", { level: 2, name: "Client IP" }),
    ).toBeVisible();
    expect(
      screen.getByRole("radiogroup", { name: "Read the address from" }),
    ).toBeVisible();
    expect(source("The connection")).not.toBeChecked();
    expect(source("X-Forwarded-For")).toBeChecked();
    expect(source("Another header")).not.toBeChecked();
    expect(source("The connection")).toHaveAccessibleDescription(
      "Use the source address of the connection. For deployments with no proxy in front of the server.",
    );
    expect(proxies()).toHaveValue("10.0.0.0/8\n192.168.0.0/16");
    expect(screen.queryByRole("textbox", { name: "Header name" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("shows only the fields the chosen source reads", async () => {
    const user = userEvent.setup();
    mount({ strategy: "direct", header: "", trustedProxies: [] });

    expect(
      screen.queryByRole("textbox", { name: "Trusted proxies" }),
    ).toBeNull();
    expect(screen.queryByRole("textbox", { name: "Header name" })).toBeNull();

    await user.click(source("Another header"));
    expect(header()).toHaveAttribute("placeholder", "CF-Connecting-IP");
    expect(proxies()).toBeVisible();

    await user.click(source("The connection"));
    expect(
      screen.queryByRole("textbox", { name: "Trusted proxies" }),
    ).toBeNull();
  });

  it("states the consequence and names the action while the source changes, and asks before sending", async () => {
    const user = userEvent.setup();
    mount();

    await user.click(source("The connection"));

    expect(screen.getByText(consequence)).toBeVisible();
    expect(
      screen.getByRole("button", {
        name: "Change the client address and save",
      }),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();

    await user.click(
      screen.getByRole("button", {
        name: "Change the client address and save",
      }),
    );
    const dialog = await screen.findByRole("alertdialog", {
      name: "Change the client address?",
    });
    expect(puts()).toHaveLength(0);
    // The dialog asks for the decision; the consequence stays in the footer.
    expect(within(dialog).queryByText(consequence)).toBeNull();

    await user.click(
      within(dialog).getByRole("button", { name: "Change address" }),
    );
    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await puts()[0]?.json()).toEqual({
      strategy: "direct",
      header: "",
      // The list is kept while it is hidden, as the server stores it.
      trustedProxies: ["10.0.0.0/8", "192.168.0.0/16"],
    });
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    await waitFor(() => expect(reads().length).toBeGreaterThan(0));
  });

  it("treats a change to the trusted proxies the same way", async () => {
    const user = userEvent.setup();
    mount();

    await user.type(proxies(), "\n172.16.0.0/12");

    expect(screen.getByText(consequence)).toBeVisible();
    await user.click(
      screen.getByRole("button", {
        name: "Change the client address and save",
      }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Change address" }),
    );
    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await puts()[0]?.json()).toMatchObject({
      trustedProxies: ["10.0.0.0/8", "192.168.0.0/16", "172.16.0.0/12"],
    });
  });

  it("sends the header name under Another header", async () => {
    const user = userEvent.setup();
    mount();

    await user.click(source("Another header"));
    await user.type(header(), "CF-Connecting-IP");
    await user.click(
      screen.getByRole("button", {
        name: "Change the client address and save",
      }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Change address" }),
    );
    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await puts()[0]?.json()).toEqual({
      strategy: "header",
      header: "CF-Connecting-IP",
      trustedProxies: ["10.0.0.0/8", "192.168.0.0/16"],
    });
  });

  it("leaves the form as it was and sends nothing when the confirmation is cancelled", async () => {
    const user = userEvent.setup();
    mount();

    await user.click(source("The connection"));
    await user.click(
      screen.getByRole("button", {
        name: "Change the client address and save",
      }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));

    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    expect(source("The connection")).toBeChecked();
    expect(puts()).toHaveLength(0);
  });

  it("ignores dismissal while the confirmed write is in flight", async () => {
    const user = userEvent.setup();
    let release: (response: Response) => void = () => {};
    mount(forwarded, (request) =>
      request.method === "PUT"
        ? new Promise<Response>((resolve) => {
            release = resolve;
          })
        : undefined,
    );

    await user.click(source("The connection"));
    await user.click(
      screen.getByRole("button", {
        name: "Change the client address and save",
      }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Change address" }),
    );
    await waitFor(() => expect(puts()).toHaveLength(1));

    await user.keyboard("{Escape}");
    expect(screen.getByRole("alertdialog")).toBeVisible();

    release(new Response(null, { status: 204 }));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  });

  it("saves a change that moves nothing with a plain Save and no confirmation", async () => {
    const user = userEvent.setup();
    mount();

    await user.type(proxies(), "\n\n");
    expect(screen.queryByText(consequence)).toBeNull();
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });

  it("puts the form back with Discard, sending nothing", async () => {
    const user = userEvent.setup();
    mount();

    await user.click(source("The connection"));
    await user.click(screen.getByRole("button", { name: "Discard" }));

    expect(source("X-Forwarded-For")).toBeChecked();
    expect(puts()).toHaveLength(0);
  });

  it("names the rule a header name breaks, and sends nothing", async () => {
    const user = userEvent.setup();
    mount();

    await user.click(source("Another header"));
    await user.click(
      screen.getByRole("button", {
        name: "Change the client address and save",
      }),
    );
    expect(
      await screen.findByText(
        "Enter the name of the header that carries the client address.",
      ),
    ).toBeVisible();

    await user.type(header(), "X Real IP");
    await user.click(
      screen.getByRole("button", {
        name: "Change the client address and save",
      }),
    );
    expect(
      await screen.findByText(
        "Use only letters, digits, hyphens and underscores.",
      ),
    ).toBeVisible();
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(puts()).toHaveLength(0);
  });

  it("names the line a trusted proxy breaks, and sends nothing", async () => {
    const user = userEvent.setup();
    mount();

    await user.type(proxies(), "\nnot-a-range");
    await user.click(
      screen.getByRole("button", {
        name: "Change the client address and save",
      }),
    );

    expect(
      await screen.findByText(
        "Line 3 is not an IPv4 or IPv6 range, such as 10.0.0.0/8 or 2001:db8::/32.",
      ),
    ).toBeVisible();
    expect(puts()).toHaveLength(0);
  });

  it("asks for at least one proxy and for no more than 64", async () => {
    const user = userEvent.setup();
    mount();

    await user.clear(proxies());
    await user.click(
      screen.getByRole("button", {
        name: "Change the client address and save",
      }),
    );
    expect(
      await screen.findByText(
        "Enter at least one trusted proxy. This method requires one.",
      ),
    ).toBeVisible();

    await user.click(proxies());
    await user.paste(
      Array.from({ length: 65 }, (_, i) => `10.0.${i}.0/24`).join("\n"),
    );
    await user.click(
      screen.getByRole("button", {
        name: "Change the client address and save",
      }),
    );
    expect(await screen.findByText("Enter at most 64 ranges.")).toBeVisible();
    expect(puts()).toHaveLength(0);
  });

  it("refuses to save a hidden list the server would reject, and says why in the footer", async () => {
    const user = userEvent.setup();
    mount();

    await user.type(proxies(), "\nnot-a-range");
    await user.click(source("The connection"));

    const blocked =
      "The trusted proxies list contains an invalid entry. Select X-Forwarded-For to correct it.";
    expect(screen.getByText(blocked)).toBeVisible();
    expect(
      screen.getByRole("button", {
        name: "Change the client address and save",
      }),
    ).toBeDisabled();
    expect(screen.getByRole("button", { name: "Discard" })).toBeEnabled();
    expect(puts()).toHaveLength(0);

    await user.click(source("X-Forwarded-For"));
    expect(screen.queryByText(blocked)).toBeNull();
  });

  it("says the hidden list problem in Chinese when the interface is", async () => {
    const user = userEvent.setup();
    i18n.activate("zh");
    mount();

    await user.type(proxies2(), "\nnot-a-range");
    await user.click(screen.getAllByRole("radio")[0] as HTMLElement);

    expect(screen.getByText(/^可信代理列表包含无效条目/)).toBeVisible();
  });

  it("reports a refused save once, on the form and not as a toast", async () => {
    const user = userEvent.setup();
    mount(forwarded, (request) =>
      request.method === "PUT"
        ? Response.json(
            { code: "bad_request", requestId: "client-ip-test" },
            { status: 400 },
          )
        : undefined,
    );

    await user.type(proxies(), "\n172.16.0.0/12");
    await user.click(
      screen.getByRole("button", {
        name: "Change the client address and save",
      }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Change address" }),
    );

    await waitFor(() => expect(puts()).toHaveLength(1));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    expect(await screen.findByRole("alert")).toBeVisible();
    expect(notifyError).not.toHaveBeenCalled();
  });
});
