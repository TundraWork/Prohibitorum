import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/api/errors";
import { configureSudo, resetSudo, type SudoRequest } from "@/api/sudo";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { SigningKeysPanel } from "@/pages/admin/settings/SigningKeysPanel";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifyError = vi.fn<(error: unknown, scope?: string) => void>();
const openSudo = vi.fn<(request: SudoRequest | null) => void>();
let queryClient: QueryClient;
let sudoFresh = true;

const kids = {
  active: "active-key-00000000000000000000000000000000",
  pending: "pending-key-0000000000000000000000000000000",
  retiring: "retiring-key-000000000000000000000000000000",
  retiredNew: "retired-key-new-00000000000000000000000000",
  retiredOld: "retired-key-old-00000000000000000000000000",
};

function viewOf(
  kid: string,
  status: string,
  extra: Record<string, string> = {},
) {
  return {
    kid,
    status,
    algorithm: "RS256",
    use: "sig",
    publicJwk: { kty: "RSA", kid, e: "AQAB" },
    ...extra,
  };
}

// The server lists newest first; the page groups them by where they are in
// their life, whatever the order.
const keys = [
  viewOf(kids.pending, "pending"),
  viewOf(kids.active, "active", { activatedAt: "2020-01-01T00:00:00Z" }),
  viewOf(kids.retiring, "decommissioning", {
    retireAfter: "2099-01-01T12:00:00Z",
  }),
  viewOf(kids.retiredNew, "retired", { retireAfter: "2021-01-01T00:00:00Z" }),
  viewOf(kids.retiredOld, "retired", { retireAfter: "2019-01-01T00:00:00Z" }),
];

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifyError.mockReset();
  openSudo.mockReset();
  sudoFresh = true;
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(
    notifyError,
    vi.fn<(message: MessageDescriptor) => void>(),
  );
  configureSudo({
    queryClient,
    set: openSudo,
    setFresh: () => {},
    getFresh: () => sudoFresh,
  });
  fetchBoundary.mockImplementation(async (request) => {
    const path = new URL(request.url).pathname;
    if (path.endsWith("/signing-keys")) {
      return Response.json({ items: keys, nextCursor: "" });
    }
    if (path.endsWith("/me/sudo/methods")) {
      return Response.json({ methods: ["password_totp"], fresh: sudoFresh });
    }
    if (path.endsWith("/activate")) {
      return Response.json(
        { code: "credential_not_found", requestId: "r1" },
        { status: 404 },
      );
    }
    return Response.json(viewOf("new-key", "pending"), { status: 200 });
  });
});

afterEach(() => {
  resetSudo();
  queryClient.clear();
  vi.unstubAllGlobals();
});

function mount() {
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <SigningKeysPanel />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

/** The list item that carries a key id, found by the id's full text. */
function rowOf(kid: string): HTMLElement {
  const row = screen.getByText(kid).closest("li");
  if (!(row instanceof HTMLElement)) throw new Error(`no row for ${kid}`);
  return row;
}

function requests(suffix: string) {
  return fetchBoundary.mock.calls.filter(([request]) =>
    new URL(request.url).pathname.endsWith(suffix),
  );
}

async function loaded() {
  await screen.findByText(kids.active);
}

describe("signing keys", () => {
  it("explains the lifecycle under the heading", async () => {
    mount();
    await loaded();

    expect(
      screen.getByRole("heading", { level: 2, name: "Signing keys" }),
    ).toBeVisible();
    expect(
      screen.getByText(
        "A new key is published as soon as it is generated and starts signing when you activate it. The key it replaces keeps verifying through a grace period, then is retired.",
      ),
    ).toBeVisible();
  });

  it("lists the signing key first, then pending, then retiring, each saying where it stands", async () => {
    mount();
    await loaded();

    const rows = within(
      screen.getByRole("list", { name: "Signing keys" }),
    ).getAllByRole("listitem");
    expect(rows.map((row) => row.textContent)).toEqual([
      expect.stringContaining(kids.active),
      expect.stringContaining(kids.pending),
      expect.stringContaining(kids.retiring),
    ]);

    const active = rowOf(kids.active);
    expect(within(active).getByText("RS256")).toBeVisible();
    expect(within(active).getByText(/Signing since/)).toBeVisible();
    expect(
      within(rowOf(kids.pending)).getByText("Published, not yet signing"),
    ).toBeVisible();
    expect(
      within(rowOf(kids.retiring)).getByText(
        "Still verifying until Jan 1, 2099",
      ),
    ).toBeVisible();
  });

  it("offers the public key on every key, and activate and retire only on a pending one", async () => {
    mount();
    await loaded();
    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "Show 2 retired keys" }));

    for (const kid of Object.values(kids)) {
      const row = rowOf(kid);
      expect(
        within(row).getByRole("button", { name: "View public key" }),
      ).toBeInTheDocument();
      // Retiring a key that is already retiring would restart its grace
      // period, so it is not offered there either.
      for (const action of ["Activate", "Retire"]) {
        const control = within(row).queryByRole("button", { name: action });
        if (kid === kids.pending) expect(control).toBeInTheDocument();
        else expect(control).not.toBeInTheDocument();
      }
    }
  });

  it("keeps retired keys behind a button that counts them", async () => {
    const user = userEvent.setup();
    mount();
    await loaded();

    expect(screen.queryByText(kids.retiredNew)).not.toBeInTheDocument();
    const toggle = screen.getByRole("button", { name: "Show 2 retired keys" });
    expect(toggle).toHaveAttribute("aria-expanded", "false");

    await user.click(toggle);
    expect(screen.getByText(kids.retiredNew)).toBeVisible();
    expect(screen.getByText(kids.retiredOld)).toBeVisible();
    expect(
      rowOf(kids.retiredNew).querySelector("[data-slot=item-details]"),
    ).toHaveTextContent(/Retired .+ ago/);
    expect(
      screen.getByRole("button", { name: "Hide retired keys" }),
    ).toHaveAttribute("aria-expanded", "true");

    await user.click(screen.getByRole("button", { name: "Hide retired keys" }));
    expect(screen.queryByText(kids.retiredNew)).not.toBeInTheDocument();
  });

  it("offers no retired toggle when none are retired", async () => {
    fetchBoundary.mockImplementation(async (request) =>
      new URL(request.url).pathname.endsWith("/signing-keys")
        ? Response.json({ items: keys.slice(0, 3), nextCursor: "" })
        : new Response(null, { status: 204 }),
    );
    mount();
    await loaded();

    expect(screen.queryByRole("button", { name: /retired key/ })).toBeNull();
  });

  it("copies a key id from the button beside it", async () => {
    const user = userEvent.setup();
    mount();
    await loaded();
    const writeText = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockResolvedValue(undefined);

    await user.click(
      within(rowOf(kids.active)).getByRole("button", { name: "Copy key ID" }),
    );

    expect(writeText).toHaveBeenCalledWith(kids.active);
  });

  it("confirms before activating, and on a refusal says so in the key's own terms and refreshes the list", async () => {
    const user = userEvent.setup();
    mount();
    await loaded();
    const reads = requests("/signing-keys").length;

    await user.click(
      within(rowOf(kids.pending)).getByRole("button", { name: "Activate" }),
    );
    const dialog = await screen.findByRole("alertdialog", {
      name: "Sign with this key?",
    });
    expect(dialog).toHaveAccessibleDescription(
      `${kids.pending} New tokens and SAML assertions are signed with this key. The current key keeps verifying what it signed until it is retired.`,
    );
    expect(requests("/activate")).toHaveLength(0);

    await user.click(within(dialog).getByRole("button", { name: "Activate" }));

    await waitFor(() => expect(notifyError).toHaveBeenCalledTimes(1));
    const [error, scope] = notifyError.mock.calls[0] ?? [];
    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).code).toBe("credential_not_found");
    expect(scope).toBe("signing-key");
    const [activate] = requests("/activate");
    expect(activate?.[0].method).toBe("POST");
    expect(await activate?.[0].clone().json()).toEqual({});
    await waitFor(() =>
      expect(requests("/signing-keys").length).toBeGreaterThan(reads),
    );
    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
  });

  it("confirms before retiring, then retires and refreshes the list", async () => {
    const user = userEvent.setup();
    mount();
    await loaded();
    const reads = requests("/signing-keys").length;

    await user.click(
      within(rowOf(kids.pending)).getByRole("button", { name: "Retire" }),
    );
    const dialog = await screen.findByRole("alertdialog", {
      name: "Retire this key?",
    });
    expect(dialog).toHaveTextContent(kids.pending);
    expect(dialog).toHaveTextContent(
      "After the grace period the key is removed from the JWKS and the SAML metadata.",
    );
    expect(requests("/retire")).toHaveLength(0);
    await user.click(within(dialog).getByRole("button", { name: "Retire" }));

    await waitFor(() => expect(requests("/retire")).toHaveLength(1));
    await waitFor(() =>
      expect(requests("/signing-keys").length).toBeGreaterThan(reads),
    );
    expect(notifyError).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  });

  it("ignores dismissal of a confirmation while its write is in flight", async () => {
    const user = userEvent.setup();
    let release: (response: Response) => void = () => {};
    const base = fetchBoundary.getMockImplementation();
    fetchBoundary.mockImplementation(async (request) =>
      new URL(request.url).pathname.endsWith("/retire")
        ? new Promise<Response>((resolve) => {
            release = resolve;
          })
        : (base?.(request) ?? new Response(null, { status: 204 })),
    );
    mount();
    await loaded();

    await user.click(
      within(rowOf(kids.pending)).getByRole("button", { name: "Retire" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Retire" }));
    await waitFor(() => expect(requests("/retire")).toHaveLength(1));

    await user.keyboard("{Escape}");
    expect(screen.getByRole("alertdialog")).toBeVisible();

    release(Response.json(viewOf(kids.pending, "decommissioning")));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  });

  it("generates a key and refreshes the list", async () => {
    const user = userEvent.setup();
    mount();
    await loaded();
    const reads = requests("/signing-keys").length;

    await user.click(screen.getByRole("button", { name: "Generate key" }));

    await waitFor(() => expect(requests("/generate")).toHaveLength(1));
    expect(requests("/generate")[0]?.[0].method).toBe("POST");
    await waitFor(() =>
      expect(requests("/signing-keys").length).toBeGreaterThan(reads),
    );
  });

  it("asks for a fresh identity check before generating, activating or retiring", async () => {
    sudoFresh = false;
    const user = userEvent.setup();
    mount();
    await loaded();

    await user.click(screen.getByRole("button", { name: "Generate key" }));
    await waitFor(() => expect(openSudo).toHaveBeenCalledTimes(1));
    expect(openSudo.mock.calls[0]?.[0]?.reason?.id).toBe(
      "sudo.reason.generate-signing-key",
    );

    for (const [action, reason] of [
      ["Activate", "sudo.reason.activate-signing-key"],
      ["Retire", "sudo.reason.retire-signing-key"],
    ] as const) {
      await user.click(
        within(rowOf(kids.pending)).getByRole("button", { name: action }),
      );
      const dialog = await screen.findByRole("alertdialog");
      await user.click(within(dialog).getByRole("button", { name: action }));
      await waitFor(() =>
        expect(openSudo.mock.calls.at(-1)?.[0]?.reason?.id).toBe(reason),
      );
      // Backing out of the check closes the confirmation and changes nothing.
      openSudo.mock.calls.at(-1)?.[0]?.reject(new Error("backed out"));
      await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    }
    expect(requests("/generate")).toHaveLength(0);
    expect(requests("/activate")).toHaveLength(0);
    expect(requests("/retire")).toHaveLength(0);
  });

  it("shows a key's public JWK in a dialog, and copies it", async () => {
    const user = userEvent.setup();
    mount();
    await loaded();
    const writeText = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockResolvedValue(undefined);

    await user.click(
      within(rowOf(kids.active)).getByRole("button", {
        name: "View public key",
      }),
    );
    const dialog = await screen.findByRole("dialog", { name: "Public key" });
    expect(dialog).toHaveTextContent(`This is ${kids.active} as the instance`);
    const jwk = JSON.stringify(
      { kty: "RSA", kid: kids.active, e: "AQAB" },
      null,
      2,
    );
    expect(within(dialog).getByRole("textbox")).toHaveValue(jwk);

    await user.click(within(dialog).getByRole("button", { name: "Copy" }));
    expect(writeText).toHaveBeenCalledWith(jwk);
    expect(
      await within(dialog).findByRole("button", { name: "Copied" }),
    ).toBeVisible();

    await user.click(within(dialog).getByRole("button", { name: "Close" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
  });

  it("says once, in the dialog, that the clipboard refused", async () => {
    const user = userEvent.setup();
    mount();
    await loaded();
    vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(
      new DOMException("Denied", "NotAllowedError"),
    );

    await user.click(
      within(rowOf(kids.active)).getByRole("button", {
        name: "View public key",
      }),
    );
    const dialog = await screen.findByRole("dialog", { name: "Public key" });
    await user.click(within(dialog).getByRole("button", { name: "Copy" }));

    expect(await within(dialog).findByRole("alert")).toBeVisible();
  });

  it("says the keys could not be loaded, with a way to ask again", async () => {
    const user = userEvent.setup();
    fetchBoundary.mockImplementation(async (request) =>
      new URL(request.url).pathname.endsWith("/signing-keys")
        ? Response.json(
            { code: "internal_error", requestId: "keys" },
            { status: 500 },
          )
        : new Response(null, { status: 204 }),
    );
    mount();

    expect(await screen.findByRole("alert")).toBeVisible();
    // The section draws the failure, so no toast repeats it.
    expect(notifyError).not.toHaveBeenCalled();
    fetchBoundary.mockImplementation(async () =>
      Response.json({ items: keys, nextCursor: "" }),
    );
    await user.click(screen.getByRole("button", { name: "Try again" }));
    await loaded();
  });

  it("has nothing to list when there are no keys", async () => {
    fetchBoundary.mockImplementation(async () =>
      Response.json({ items: [], nextCursor: "" }),
    );
    mount();

    expect(await screen.findByText("No signing keys")).toBeVisible();
  });

  describe("when later pages exist", () => {
    function paged(second: () => Response) {
      fetchBoundary.mockImplementation(async (request) => {
        const url = new URL(request.url);
        if (!url.pathname.endsWith("/signing-keys")) {
          return new Response(null, { status: 204 });
        }
        return url.searchParams.has("cursor")
          ? second()
          : Response.json({ items: keys.slice(0, 2), nextCursor: "more" });
      });
    }

    it("does not count retired keys, and offers more only once they are shown", async () => {
      const user = userEvent.setup();
      paged(() => Response.json({ items: keys.slice(2), nextCursor: "" }));
      mount();
      await loaded();

      expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
      await user.click(
        screen.getByRole("button", { name: "Show retired keys" }),
      );
      await user.click(screen.getByRole("button", { name: "Load more" }));

      expect(await screen.findByText(kids.retiredNew)).toBeVisible();
      expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
      expect(
        screen.getByRole("button", { name: "Hide retired keys" }),
      ).toBeVisible();
    });

    it("says a later page failed while keeping the keys already shown", async () => {
      const user = userEvent.setup();
      paged(() =>
        Response.json(
          { code: "internal_error", requestId: "page2" },
          { status: 500 },
        ),
      );
      mount();
      await loaded();

      await user.click(
        screen.getByRole("button", { name: "Show retired keys" }),
      );
      await user.click(screen.getByRole("button", { name: "Load more" }));

      expect(await screen.findByRole("alert")).toBeVisible();
      expect(
        screen.getByText("The rest of the keys could not be loaded."),
      ).toBeVisible();
      expect(screen.getByText(kids.active)).toBeVisible();
      expect(screen.getByText(kids.pending)).toBeVisible();
      expect(notifyError).not.toHaveBeenCalled();
    });
  });
});
