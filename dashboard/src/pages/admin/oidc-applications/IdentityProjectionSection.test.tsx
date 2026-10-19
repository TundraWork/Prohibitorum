import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { IdentityProjectionSection } from "@/pages/admin/oidc-applications/IdentityProjectionSection";
import { type OidcApp, oidcAppFixture } from "@/test/oidc-app";

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
  app: OidcApp,
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
        <IdentityProjectionSection app={app} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

async function pickSource(user: UserEvent, name: string) {
  await user.click(
    await screen.findByRole("combobox", { name: "Subject source" }),
  );
  // An option's name carries its explanation, so it is matched by how it starts.
  await user.click(
    await screen.findByRole("option", { name: new RegExp(`^${name}`) }),
  );
}

describe("the identity section", () => {
  it("holds no footer while nothing is edited", async () => {
    mount(oidcAppFixture());
    await screen.findByRole("combobox", { name: "Subject source" });
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("says a subject change signs everyone out and names the action", async () => {
    const user = userEvent.setup();
    mount(oidcAppFixture());

    await pickSource(user, "Username");

    expect(
      await screen.findByText(
        "Changing the subject source signs everyone out of every client.",
      ),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Change subject and save" }),
    ).toBeVisible();
  });

  it("sends the change only once it is confirmed, with the aliases as stored", async () => {
    const user = userEvent.setup();
    mount(oidcAppFixture({ claimAliases: { nickname: "preferred_username" } }));

    await pickSource(user, "Username");
    await user.click(
      screen.getByRole("button", { name: "Change subject and save" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    expect(puts()).toHaveLength(0);
    await user.click(
      within(dialog).getByRole("button", { name: "Change and save" }),
    );

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await puts()[0]?.json()).toEqual({
      subjectSource: "username",
      claimAliases: { nickname: "preferred_username" },
    });
  });

  it("saves an alias edit without a confirmation", async () => {
    const user = userEvent.setup();
    mount(oidcAppFixture());

    await user.click(await screen.findByRole("button", { name: "Add alias" }));
    await user.type(
      screen.getByRole("textbox", { name: "Claim name" }),
      "team",
    );
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(await puts()[0]?.json()).toEqual({
      subjectSource: "sub",
      claimAliases: { team: "name" },
    });
  });

  it("marks a claim name the ID token already defines", async () => {
    const user = userEvent.setup();
    mount(oidcAppFixture());

    await user.click(await screen.findByRole("button", { name: "Add alias" }));
    await user.type(screen.getByRole("textbox", { name: "Claim name" }), "sub");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(
      await screen.findByText("The ID token already defines this claim."),
    ).toBeVisible();
    expect(screen.getByRole("textbox", { name: "Claim name" })).toBeInvalid();
    expect(puts()).toHaveLength(0);
  });

  it("reports a refused save once, on the section", async () => {
    const user = userEvent.setup();
    mount(oidcAppFixture(), (request) =>
      request.method === "PUT"
        ? Response.json({ code: "internal_error" }, { status: 500 })
        : undefined,
    );

    await user.click(await screen.findByRole("button", { name: "Add alias" }));
    await user.type(
      screen.getByRole("textbox", { name: "Claim name" }),
      "team",
    );
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});
