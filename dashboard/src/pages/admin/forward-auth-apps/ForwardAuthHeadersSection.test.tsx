import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { ForwardAuthHeadersSection } from "@/pages/admin/forward-auth-apps/ForwardAuthHeadersSection";
import {
  type ForwardAuthApp,
  forwardAuthAppFixture,
} from "@/test/forward-auth-app";

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
  app: ForwardAuthApp,
  extra?: (request: Request) => Response | Promise<Response> | undefined,
) {
  fetchBoundary.mockImplementation(async (request) => {
    const canned = extra?.(request);
    if (canned) return canned;
    if (request.method === "PUT") {
      return Response.json({ ...app, remoteUserSource: "verified_email" });
    }
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <ForwardAuthHeadersSection app={app} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

async function pickSource(user: UserEvent, name: string) {
  await user.click(
    await screen.findByRole("combobox", { name: "Remote-User" }),
  );
  await user.click(
    await screen.findByRole("option", { name: new RegExp(`^${name}`) }),
  );
}

describe("the headers table", () => {
  it("lists the four headers with what each carries", async () => {
    mount(forwardAuthAppFixture());

    const table = await screen.findByRole("table", {
      name: "Headers sent to the service",
    });
    const rows = within(table).getAllByRole("row");
    // The heading row, then one row per header.
    expect(rows).toHaveLength(5);
    expect(
      rows
        .slice(1)
        .map((row) => within(row).getAllByRole("cell")[0]?.textContent),
    ).toEqual(["Remote-User", "Remote-Name", "Remote-Email", "Remote-Groups"]);
    expect(within(table).getByText("Display name")).toBeVisible();
    expect(within(table).getByText("Primary email")).toBeVisible();
    expect(
      within(table).getByText("User groups this application exposes"),
    ).toBeVisible();
  });

  it("offers the choice only on Remote-User, at the saved source", async () => {
    mount(forwardAuthAppFixture({ remoteUserSource: "verified_email" }));

    expect(
      await screen.findByRole("combobox", { name: "Remote-User" }),
    ).toHaveTextContent("Verified email");
    expect(screen.getAllByRole("combobox")).toHaveLength(1);
  });

  it("reads a record whose source is unknown as the username default", async () => {
    mount(forwardAuthAppFixture({ remoteUserSource: "" }));
    expect(
      await screen.findByRole("combobox", { name: "Remote-User" }),
    ).toHaveTextContent("Username");
  });

  it("holds no footer while nothing is changed", async () => {
    mount(forwardAuthAppFixture());
    await screen.findByRole("combobox", { name: "Remote-User" });
    expect(screen.queryByRole("button", { name: /save/i })).toBeNull();
  });
});

describe("changing Remote-User", () => {
  it("says the service sees a new account and names the action", async () => {
    const user = userEvent.setup();
    mount(forwardAuthAppFixture());

    await pickSource(user, "Verified email");

    expect(
      await screen.findByText(
        "Changing the user identifier makes the service treat everyone it already knows as a new account.",
      ),
    ).toBeVisible();
    expect(
      screen.getByRole("button", {
        name: "Change the user identifier and save",
      }),
    ).toBeVisible();
  });

  it("sends the change only once it is confirmed", async () => {
    const user = userEvent.setup();
    mount(forwardAuthAppFixture());

    await pickSource(user, "Verified email");
    await user.click(
      screen.getByRole("button", {
        name: "Change the user identifier and save",
      }),
    );
    const dialog = await screen.findByRole("alertdialog", {
      name: "Change the user identifier?",
    });
    expect(dialog).toHaveTextContent(
      "Grafana will receive the account's Verified email instead of its Username.",
    );
    expect(puts()).toHaveLength(0);
    await user.click(
      within(dialog).getByRole("button", { name: "Change and save" }),
    );

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(new URL(puts()[0]?.url ?? "").pathname).toBe(
      "/api/prohibitorum/forward-auth-apps/grafana/identity-projection",
    );
    expect(await puts()[0]?.json()).toEqual({
      remoteUserSource: "verified_email",
    });
  });

  it("sends nothing and keeps the choice when the confirmation is cancelled", async () => {
    const user = userEvent.setup();
    mount(forwardAuthAppFixture());

    await pickSource(user, "Account ID");
    await user.click(
      screen.getByRole("button", {
        name: "Change the user identifier and save",
      }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));

    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(puts()).toHaveLength(0);
    expect(
      screen.getByRole("combobox", { name: "Remote-User" }),
    ).toHaveTextContent("Account ID");
  });

  it("puts the saved source back on Discard", async () => {
    const user = userEvent.setup();
    mount(forwardAuthAppFixture());

    await pickSource(user, "Account ID");
    await user.click(screen.getByRole("button", { name: "Discard" }));

    expect(
      screen.getByRole("combobox", { name: "Remote-User" }),
    ).toHaveTextContent("Username");
    expect(screen.queryByRole("button", { name: /save/i })).toBeNull();
  });

  it("keeps the confirmation open while the write is in flight", async () => {
    const user = userEvent.setup();
    let release: (response: Response) => void = () => {};
    mount(forwardAuthAppFixture(), (request) =>
      request.method === "PUT"
        ? new Promise<Response>((resolve) => {
            release = resolve;
          })
        : undefined,
    );

    await pickSource(user, "Account ID");
    await user.click(
      screen.getByRole("button", {
        name: "Change the user identifier and save",
      }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Change and save" }),
    );
    await waitFor(() => expect(puts()).toHaveLength(1));
    await user.keyboard("{Escape}");

    expect(screen.getByRole("alertdialog")).toBeVisible();
    release(Response.json(forwardAuthAppFixture({ remoteUserSource: "sub" })));
  });

  it("reports a refused save once, on the section", async () => {
    const user = userEvent.setup();
    mount(forwardAuthAppFixture(), (request) =>
      request.method === "PUT"
        ? Response.json({ code: "internal_error" }, { status: 500 })
        : undefined,
    );

    await pickSource(user, "Account ID");
    await user.click(
      screen.getByRole("button", {
        name: "Change the user identifier and save",
      }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Change and save" }),
    );

    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});
