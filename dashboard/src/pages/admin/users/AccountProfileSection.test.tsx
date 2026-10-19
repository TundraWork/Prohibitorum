import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { configureSudo, resetSudo } from "@/api/sudo";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { AccountProfileSection } from "@/pages/admin/users/AccountProfileSection";
import { type Account, accountFixture } from "@/test/account";

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
  configureSudo({
    queryClient,
    set: () => {},
    setFresh: () => {},
    getFresh: () => true,
  });
});

afterEach(() => {
  resetSudo();
  queryClient.clear();
  vi.unstubAllGlobals();
});

function puts() {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter((request) => request.method === "PUT");
}

/** The JSON the one save sent. */
async function putBody<T = unknown>(): Promise<T> {
  const request = puts()[0];
  if (request === undefined) throw new Error("no save was sent");
  return (await request.json()) as T;
}

function mount(
  account: Account,
  extra?: (request: Request) => Response | Promise<Response> | undefined,
) {
  fetchBoundary.mockImplementation(async (request) => {
    const canned = extra?.(request);
    if (canned) return canned;
    if (request.method === "PUT") return Response.json(account);
    if (new URL(request.url).pathname.endsWith("/me/sudo/methods")) {
      return Response.json({ methods: ["password_totp"], fresh: true });
    }
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <AccountProfileSection account={account} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("the profile section", () => {
  it("shows the record, the subject and no Disabled switch", () => {
    mount(accountFixture());

    expect(
      screen.getByRole("heading", { level: 2, name: "Profile" }),
    ).toBeVisible();
    expect(screen.getByRole("textbox", { name: "Display name" })).toHaveValue(
      "Bob Stone",
    );
    expect(screen.getByRole("textbox", { name: "Email" })).toHaveValue(
      "bob@example.test",
    );
    expect(screen.getByRole("textbox", { name: "Attributes" })).toHaveValue(
      JSON.stringify({ department: "ops" }, null, 2),
    );
    expect(
      screen.getByDisplayValue("00000000-0000-4000-8000-000000000002"),
    ).toBeVisible();
    expect(screen.queryByRole("switch")).toBeNull();
    expect(screen.queryByRole("button", { name: /save/i })).toBeNull();
  });

  it("keeps monospace to the attributes box, not its label or hint", () => {
    mount(accountFixture());

    expect(
      screen
        .getByRole("textbox", { name: "Attributes" })
        .closest("[data-slot=textarea-control]"),
    ).toHaveClass("font-mono");
    expect(screen.getByText("Attributes")).not.toHaveClass("font-mono");
    expect(
      screen.getByText(/Saving sends whatever is in this box/),
    ).not.toHaveClass("font-mono");
  });

  describe("the address", () => {
    it("says whether the stored address is verified", () => {
      mount(accountFixture({ emailVerified: true }));
      expect(screen.getByText("Verified")).toBeVisible();
    });

    it("says when it is not", () => {
      mount(accountFixture({ emailVerified: false }));
      expect(screen.getByText("Not verified")).toBeVisible();
    });

    it("says nothing about an address that is not stored", () => {
      mount(accountFixture({ email: undefined, emailVerified: false }));
      expect(screen.queryByText("Not verified")).toBeNull();
      expect(screen.queryByText("Verified")).toBeNull();
    });

    it("stops claiming the typed address is verified", async () => {
      const user = userEvent.setup();
      mount(accountFixture());

      await user.type(screen.getByRole("textbox", { name: "Email" }), "x");

      expect(screen.queryByText("Verified")).toBeNull();
    });
  });

  describe("saving", () => {
    it("sends the full record, carrying the disabled flag and leaving the address alone", async () => {
      const user = userEvent.setup();
      mount(accountFixture({ disabled: true }));

      const name = screen.getByRole("textbox", { name: "Display name" });
      await user.clear(name);
      await user.type(name, "Robert");
      await user.click(screen.getByRole("button", { name: "Save changes" }));

      await waitFor(() => expect(puts()).toHaveLength(1));
      expect(new URL(puts()[0]?.url ?? "").pathname).toBe(
        "/api/prohibitorum/accounts/2",
      );
      // The address is absent: sending it would mark it unverified.
      expect(await putBody()).toEqual({
        displayName: "Robert",
        role: "user",
        disabled: true,
        attributes: { department: "ops" },
      });
    });

    it("sends a changed address, and an empty one to clear it", async () => {
      const user = userEvent.setup();
      mount(accountFixture());

      const email = screen.getByRole("textbox", { name: "Email" });
      await user.clear(email);
      await user.click(screen.getByRole("button", { name: "Save changes" }));

      await waitFor(() => expect(puts()).toHaveLength(1));
      expect((await putBody<{ email: string }>()).email).toBe("");
    });

    it("saves an empty attributes box as an empty object", async () => {
      const user = userEvent.setup();
      mount(accountFixture());

      await user.clear(screen.getByRole("textbox", { name: "Attributes" }));
      await user.click(screen.getByRole("button", { name: "Save changes" }));

      await waitFor(() => expect(puts()).toHaveLength(1));
      expect((await putBody<{ attributes: unknown }>()).attributes).toEqual({});
    });

    it("says a half-typed object is not valid JSON, and sends nothing", async () => {
      const user = userEvent.setup();
      mount(accountFixture());

      const box = screen.getByRole("textbox", { name: "Attributes" });
      await user.clear(box);
      await user.click(box);
      await user.paste('{"a":');
      await user.click(screen.getByRole("button", { name: "Save changes" }));

      expect(
        await screen.findByText(
          "This is not valid JSON. Check the braces, commas and quotes.",
        ),
      ).toBeVisible();
      expect(puts()).toHaveLength(0);
    });

    it("refuses a JSON value that is not an object", async () => {
      const user = userEvent.setup();
      mount(accountFixture());

      const box = screen.getByRole("textbox", { name: "Attributes" });
      await user.clear(box);
      await user.click(box);
      await user.paste("[1]");
      await user.tab();

      expect(
        await screen.findByText(
          "Attributes must be a JSON object, such as {}.",
        ),
      ).toBeVisible();
    });

    it("asks for a display name", async () => {
      const user = userEvent.setup();
      mount(accountFixture());

      await user.clear(screen.getByRole("textbox", { name: "Display name" }));
      await user.tab();

      expect(
        await screen.findByText("Give the account a display name."),
      ).toBeVisible();
    });

    it("puts the saved record back on Discard", async () => {
      const user = userEvent.setup();
      mount(accountFixture());

      const name = screen.getByRole("textbox", { name: "Display name" });
      await user.type(name, "!");
      await user.click(screen.getByRole("button", { name: "Discard" }));

      expect(name).toHaveValue("Bob Stone");
      expect(screen.queryByRole("button", { name: "Discard" })).toBeNull();
    });

    it("reports a refused save once, with no toast", async () => {
      const user = userEvent.setup();
      mount(accountFixture(), (request) =>
        request.method === "PUT"
          ? Response.json({ code: "internal_error" }, { status: 500 })
          : undefined,
      );

      await user.type(
        screen.getByRole("textbox", { name: "Display name" }),
        "!",
      );
      await user.click(screen.getByRole("button", { name: "Save changes" }));

      expect(await screen.findAllByRole("alert")).toHaveLength(1);
      expect(notifyError).not.toHaveBeenCalled();
    });
  });

  describe("the role", () => {
    it("offers User and Admin, at the stored role", () => {
      mount(accountFixture({ role: "admin" }));

      const group = screen.getByRole("radiogroup", { name: "Role" });
      expect(within(group).getByRole("radio", { name: "Admin" })).toBeChecked();
      expect(within(group).getAllByRole("radio")).toHaveLength(2);
    });

    it("shows a stored role outside the two as itself, with a note", () => {
      mount(accountFixture({ role: "member" }));

      const group = screen.getByRole("radiogroup", { name: "Role" });
      expect(
        within(group).getByRole("radio", { name: "member" }),
      ).toBeChecked();
      expect(
        screen.getByText(
          /has the role member, which the console does not offer/,
        ),
      ).toBeVisible();
    });

    it("tells the reader to change an unknown role before saving", () => {
      mount(accountFixture({ role: "member" }));
      expect(
        screen.getByText(/Change it to User or Admin before saving/),
      ).toBeVisible();
    });

    it("sends the role the reader picked in place of an unknown one", async () => {
      const user = userEvent.setup();
      mount(accountFixture({ role: "member" }));

      await user.click(screen.getByRole("radio", { name: "User" }));
      await user.click(
        screen.getByRole("button", { name: "Make user and save" }),
      );

      await waitFor(() => expect(puts()).toHaveLength(1));
      expect((await putBody<{ role: string }>()).role).toBe("user");
    });

    it("says what making an admin does, and names the action", async () => {
      const user = userEvent.setup();
      mount(accountFixture());

      await user.click(screen.getByRole("radio", { name: "Admin" }));

      expect(
        screen.getByText("Bob Stone gets access to the administration pages."),
      ).toBeVisible();
      expect(
        screen.getByRole("button", { name: "Make admin and save" }),
      ).toBeVisible();
    });

    it("says what making a user does, and names the action", async () => {
      const user = userEvent.setup();
      mount(accountFixture({ role: "admin" }));

      await user.click(screen.getByRole("radio", { name: "User" }));

      expect(
        screen.getByText("Bob Stone loses access to the administration pages."),
      ).toBeVisible();
      expect(
        screen.getByRole("button", { name: "Make user and save" }),
      ).toBeVisible();
    });

    it("drops the consequence when the role is put back", async () => {
      const user = userEvent.setup();
      mount(accountFixture());

      await user.click(screen.getByRole("radio", { name: "Admin" }));
      await user.click(screen.getByRole("radio", { name: "User" }));

      expect(screen.queryByText(/administration pages/)).toBeNull();
      expect(screen.queryByRole("button", { name: /save/i })).toBeNull();
    });

    it("holds Admin for a disabled account, with the reason in words", () => {
      mount(accountFixture({ disabled: true }));

      const admin = screen.getByRole("radio", { name: "Admin" });
      expect(admin).toHaveAttribute("aria-disabled", "true");
      expect(screen.getByRole("radio", { name: "User" })).not.toHaveAttribute(
        "aria-disabled",
        "true",
      );
      expect(admin).toHaveAccessibleDescription(
        "Enable the account before making it an admin.",
      );
      expect(
        screen.getByText("Enable the account before making it an admin."),
      ).toBeVisible();
    });

    it("keeps an admin who is disabled as an admin, with nothing held", () => {
      mount(accountFixture({ role: "admin", disabled: true }));
      expect(screen.getByRole("radio", { name: "Admin" })).not.toHaveAttribute(
        "aria-disabled",
        "true",
      );
      expect(
        screen.queryByText("Enable the account before making it an admin."),
      ).toBeNull();
    });

    it("puts the server's refusal to make a disabled account an admin on the role", async () => {
      const user = userEvent.setup();
      mount(accountFixture(), (request) =>
        request.method === "PUT"
          ? Response.json(
              { code: "admin_cannot_be_disabled", requestId: "r1" },
              { status: 400 },
            )
          : undefined,
      );

      await user.click(screen.getByRole("radio", { name: "Admin" }));
      await user.click(
        screen.getByRole("button", { name: "Make admin and save" }),
      );

      const group = screen.getByRole("radiogroup", { name: "Role" });
      const field = group.closest("[data-slot=field]") as HTMLElement;
      expect(
        await within(field).findByText(
          "Enable the account before making it an admin.",
        ),
      ).toBeVisible();
      expect(notifyError).not.toHaveBeenCalled();
    });

    it("sends the new role", async () => {
      const user = userEvent.setup();
      mount(accountFixture());

      await user.click(screen.getByRole("radio", { name: "Admin" }));
      await user.click(
        screen.getByRole("button", { name: "Make admin and save" }),
      );

      await waitFor(() => expect(puts()).toHaveLength(1));
      expect((await putBody<{ role: string }>()).role).toBe("admin");
    });

    it("asks for the identity check first when the window is closed", async () => {
      const user = userEvent.setup();
      const set = vi.fn();
      configureSudo({
        queryClient,
        set,
        setFresh: () => {},
        getFresh: () => false,
      });
      mount(accountFixture(), (request) =>
        new URL(request.url).pathname.endsWith("/me/sudo/methods")
          ? Response.json({ methods: ["password_totp"], fresh: false })
          : undefined,
      );

      await user.click(screen.getByRole("radio", { name: "Admin" }));
      await user.click(
        screen.getByRole("button", { name: "Make admin and save" }),
      );

      await waitFor(() => expect(set).toHaveBeenCalled());
      expect(puts()).toHaveLength(0);
    });

    it("does not ask for it when only the name changes", async () => {
      const user = userEvent.setup();
      const set = vi.fn();
      configureSudo({
        queryClient,
        set,
        setFresh: () => {},
        getFresh: () => false,
      });
      mount(accountFixture());

      await user.type(
        screen.getByRole("textbox", { name: "Display name" }),
        "!",
      );
      await user.click(screen.getByRole("button", { name: "Save changes" }));

      await waitFor(() => expect(puts()).toHaveLength(1));
      expect(set).not.toHaveBeenCalled();
    });
  });
});
