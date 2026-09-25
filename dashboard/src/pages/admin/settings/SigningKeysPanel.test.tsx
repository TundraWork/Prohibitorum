import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/api/errors";
import { configureSudo, resetSudo } from "@/api/sudo";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { SigningKeysPanel } from "@/pages/admin/settings/SigningKeysPanel";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifyError = vi.fn();
let queryClient: QueryClient;

const keys = [
  { kid: "pending-key-0000000000000000000000000000000", status: "pending" },
  { kid: "active-key-00000000000000000000000000000000", status: "active" },
  {
    kid: "retiring-key-000000000000000000000000000000",
    status: "decommissioning",
    retireAfter: "2099-01-01T00:00:00Z",
  },
  { kid: "retired-key-0000000000000000000000000000000", status: "retired" },
].map((key) => ({
  algorithm: "RS256",
  use: "sig",
  publicJwk: { kty: "RSA", kid: key.kid, e: "AQAB" },
  ...key,
}));

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifyError.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(notifyError);
  configureSudo({
    queryClient,
    set: () => {},
    setFresh: () => {},
    getFresh: () => true,
  });
  fetchBoundary.mockImplementation(async (request) => {
    const path = new URL(request.url).pathname;
    if (path.endsWith("/signing-keys")) {
      return Response.json({ items: keys, nextCursor: "" });
    }
    if (path.endsWith("/me/sudo/methods")) {
      return Response.json({ methods: ["password_totp"], fresh: true });
    }
    if (path.endsWith("/activate")) {
      return Response.json(
        { code: "credential_not_found", requestId: "r1" },
        { status: 404 },
      );
    }
    return new Response(null, { status: 204 });
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

/** The body row that carries a key id, found by the id's full text. */
function rowOf(kid: string): HTMLElement {
  const row = screen.getByText(kid).closest('[role="row"]');
  if (!(row instanceof HTMLElement)) throw new Error(`no row for ${kid}`);
  return row;
}

function requests(suffix: string) {
  return fetchBoundary.mock.calls.filter(([request]) =>
    new URL(request.url).pathname.endsWith(suffix),
  );
}

describe("signing keys", () => {
  it("offers the public key on every key, and activate and retire only on a pending one", async () => {
    mount();
    await screen.findByText(keys[0]?.kid ?? "");

    for (const key of keys) {
      const row = rowOf(key.kid);
      expect(
        within(row).getByRole("button", { name: "View public key" }),
      ).toBeInTheDocument();
      const pending = key.status === "pending";
      // Retiring a key that is already retiring would restart its grace
      // period, so it is not offered there either.
      for (const action of ["Activate", "Retire"]) {
        const control = within(row).queryByRole("button", { name: action });
        if (pending) expect(control).toBeInTheDocument();
        else expect(control).not.toBeInTheDocument();
      }
    }
    expect(
      within(rowOf(keys[1]?.kid ?? "")).getByText("Signing"),
    ).toBeVisible();
    expect(
      within(rowOf(keys[2]?.kid ?? "")).getByText("Retiring"),
    ).toBeVisible();
  });

  it("confirms before activating, and on a refusal says so in the key's own terms and refreshes the list", async () => {
    mount();
    const user = userEvent.setup();
    await screen.findByText(keys[0]?.kid ?? "");
    const reads = requests("/signing-keys").length;

    await user.click(
      within(rowOf(keys[0]?.kid ?? "")).getByRole("button", {
        name: "Activate",
      }),
    );
    const dialog = await screen.findByRole("alertdialog", {
      name: "Sign with this key?",
    });
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

  it("shows a key's public JWK in a dialog", async () => {
    mount();
    const user = userEvent.setup();
    await screen.findByText(keys[1]?.kid ?? "");

    await user.click(
      within(rowOf(keys[1]?.kid ?? "")).getByRole("button", {
        name: "View public key",
      }),
    );
    const dialog = await screen.findByRole("dialog", { name: "Public key" });
    expect(dialog.textContent).toContain('"kty": "RSA"');
    await user.click(within(dialog).getByRole("button", { name: "Close" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
  });
});
