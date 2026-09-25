import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import { factorsQueryOptions, sessionQueryOptions } from "@/api/queries";
import { configureSudo, resetSudo } from "@/api/sudo";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { PasswordTotpPanel } from "@/pages/security/PasswordTotpPanel";

vi.mock("qrcode", () => ({
  default: { toCanvas: vi.fn().mockResolvedValue(undefined) },
}));

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifySuccess = vi.fn<(message: MessageDescriptor) => void>();
let queryClient: QueryClient;

function json(body: unknown) {
  return Response.json(body);
}

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifySuccess.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(() => undefined, notifySuccess);
  queryClient.setQueryData<components["schemas"]["SessionView"]>(
    sessionQueryOptions().queryKey,
    { id: 1, username: "alice", displayName: "Alice", role: "user" },
  );
  queryClient.setQueryData(["public", "config"], {
    instanceName: "Prohibitorum",
    hasCustomIcon: false,
    iconUrl: "",
    iconEtag: "",
    maintenanceMode: false,
    maintenanceMessage: "",
    hasCustomBackground: false,
    backgroundUrl: "",
    backgroundEtag: "",
    totp: { issuer: "Prohibitorum", algorithm: "SHA1", digits: 6, period: 30 },
  });
});

afterEach(() => {
  resetSudo();
  queryClient.clear();
  vi.unstubAllGlobals();
});

function mount(
  factors: {
    passwordSet: boolean;
    totpEnrolled: boolean;
    passkeyCount: number;
    recoveryCodesRemaining: number;
  },
  extra?: (path: string) => Response | undefined,
) {
  fetchBoundary.mockImplementation(async (request) => {
    const path = new URL(request.url).pathname;
    const canned = extra?.(path);
    if (canned) return canned;
    if (path.endsWith("/me/factors")) return json(factors);
    if (path.endsWith("/me/sudo/methods")) {
      return json({ methods: ["password_totp"], fresh: true });
    }
    return new Response(null, { status: 204 });
  });
  // The reveal guards against leaving with unsaved codes, and that guard is the
  // router's, so the panel needs one mounted even when nothing navigates.
  const root = createRootRoute({ component: Outlet });
  const security = createRoute({
    getParentRoute: () => root,
    path: "/",
    component: PasswordTotpPanel,
  });
  const router = createRouter({
    routeTree: root.addChildren([security]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

/** Requests that asked for new recovery codes. */
function regenerateRequests() {
  return fetchBoundary.mock.calls.filter(([request]) =>
    new URL(request.url).pathname.endsWith("/me/recovery-codes/regenerate"),
  );
}

const bothSet = {
  passwordSet: true,
  totpEnrolled: true,
  passkeyCount: 1,
  recoveryCodesRemaining: 4,
};

describe("password and authenticator factors", () => {
  it("lists every action at once when both factors exist, and never the combined endpoint", async () => {
    mount(bothSet);

    // Each action is a row that is on screen from the start: nothing to open.
    expect(await screen.findByText("Change password")).toBeInTheDocument();
    expect(screen.getByText("Replace authenticator")).toBeInTheDocument();
    expect(screen.getByText("Recovery codes")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Change" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Replace" })).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Generate new recovery codes" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("heading", {
        name: "Set up a password and authenticator",
      }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("heading", {
        name: "Set up an authenticator with a new password",
      }),
    ).not.toBeInTheDocument();

    // The forms wait in dialogs until the user asks for one.
    expect(screen.queryByLabelText("New password")).not.toBeInTheDocument();

    // Reading the state is not a write.
    const writes = fetchBoundary.mock.calls.filter(
      ([request]) => request.method !== "GET",
    );
    expect(writes).toEqual([]);
  });

  it("reveals regenerated recovery codes in a dialog rather than in place of the panel", async () => {
    const codes = ["ABCD-EFGH-IJKL-MN23", "QRST-UVWX-YZ23-4567"];
    mount(bothSet, (path) =>
      path.endsWith("/me/recovery-codes/regenerate")
        ? json({ recovery_codes: codes })
        : undefined,
    );
    configureSudo({
      queryClient,
      set: () => {},
      setFresh: () => {},
      getFresh: () => true,
    });
    const user: UserEvent = userEvent.setup();

    await screen.findByText("Change password");
    await user.click(
      screen.getByRole("button", { name: "Generate new recovery codes" }),
    );
    // The old codes stop working, so nothing is sent until that is confirmed.
    const confirm = await screen.findByRole("alertdialog", {
      name: "Replace your recovery codes?",
    });
    expect(regenerateRequests()).toHaveLength(0);
    await user.click(
      within(confirm).getByRole("button", { name: "Replace codes" }),
    );

    const dialog = await screen.findByRole("dialog", {
      name: "Save your new recovery codes",
    });
    expect(regenerateRequests()).toHaveLength(1);
    expect(
      within(dialog).getByRole("textbox", { name: "New recovery codes" }),
    ).toHaveValue(codes.join("\n"));
    // The console is still there behind the codes, not replaced by them. The
    // dialog hides the rest of the page from assistive technology while it is
    // open, so the panel is only reachable through hidden queries.
    expect(
      screen.getByRole("button", { name: "Change", hidden: true }),
    ).toBeInTheDocument();

    // Saving is what unlocks the only way out of the dialog.
    const leave = within(dialog).getByRole("button", { name: "Continue" });
    expect(leave).toBeDisabled();
    await user.click(
      within(dialog).getByRole("checkbox", {
        name: "I have saved my recovery codes",
      }),
    );
    await user.click(leave);
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    expect(screen.getByRole("button", { name: "Change" })).toBeInTheDocument();
  });

  it("keeps the existing recovery codes when the replacement is cancelled", async () => {
    mount(bothSet);
    const user: UserEvent = userEvent.setup();

    await screen.findByText("Change password");
    await user.click(
      screen.getByRole("button", { name: "Generate new recovery codes" }),
    );
    const confirm = await screen.findByRole("alertdialog", {
      name: "Replace your recovery codes?",
    });
    await user.click(within(confirm).getByRole("button", { name: "Cancel" }));

    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(regenerateRequests()).toHaveLength(0);
  });

  it("shows the combined setup in place when a factor is missing", async () => {
    mount({
      passwordSet: false,
      totpEnrolled: false,
      passkeyCount: 0,
      recoveryCodesRemaining: 0,
    });

    // One form, not two: the backend only establishes both together. It is
    // the only thing left to do, so its fields are on screen on arrival.
    expect(
      await screen.findByRole("heading", {
        name: "Set up a password and authenticator",
      }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Choose a password")).toBeInTheDocument();
    expect(screen.queryByText("Change password")).not.toBeInTheDocument();
    expect(screen.queryByText("Replace authenticator")).not.toBeInTheDocument();

    // No passkeys, so turning the pair off is the last way in and is refused
    // before the attempt rather than after.
    expect(screen.getByRole("button", { name: "Turn off" })).toBeDisabled();
  });

  it("names the missing half when only the authenticator is absent", async () => {
    mount({
      passwordSet: true,
      totpEnrolled: false,
      passkeyCount: 2,
      recoveryCodesRemaining: 0,
    });

    expect(
      await screen.findByRole("heading", {
        name: "Set up an authenticator with a new password",
      }),
    ).toBeInTheDocument();
  });

  it("sends password and code together, and prefers the combined endpoint over the authenticator-only one", async () => {
    mount({
      passwordSet: false,
      totpEnrolled: false,
      passkeyCount: 0,
      recoveryCodesRemaining: 0,
    });
    const user: UserEvent = userEvent.setup();

    await user.type(
      await screen.findByLabelText("Choose a password"),
      "a-long-enough-password",
    );
    await user.type(screen.getByLabelText("Current code"), "123456");
    await user.click(screen.getByRole("button", { name: "Turn on both" }));

    await waitFor(() => {
      expect(
        fetchBoundary.mock.calls.some(([request]) =>
          new URL(request.url).pathname.endsWith("/me/password-totp/verify"),
        ),
      ).toBe(true);
    });

    const verify = fetchBoundary.mock.calls.find(([request]) =>
      new URL(request.url).pathname.endsWith("/me/password-totp/verify"),
    );
    const [request] = verify ?? [];
    expect(request).toBeDefined();
    const body = JSON.parse(await (request as Request).clone().text());
    expect(body).toMatchObject({
      password: "a-long-enough-password",
      code: "123456",
    });
    // The secret is generated in the browser and must be a valid Base32 secret.
    expect(body.secret_base32).toMatch(/^[A-Z2-7]{32}$/);

    // Never the authenticator-only route, which would leave the password unset.
    expect(
      fetchBoundary.mock.calls.some(([request]) =>
        new URL(request.url).pathname.endsWith("/me/totp/verify"),
      ),
    ).toBe(false);
  });

  it("closes the dialog and announces the change once the password is set", async () => {
    mount(bothSet);
    configureSudo({
      queryClient,
      set: () => {},
      setFresh: () => {},
      getFresh: () => true,
    });
    const user: UserEvent = userEvent.setup();

    await user.click(await screen.findByRole("button", { name: "Change" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Change password",
    });
    await user.type(
      within(dialog).getByLabelText("New password"),
      "a-long-enough-password",
    );
    await user.type(
      within(dialog).getByLabelText("Repeat new password"),
      "a-long-enough-password",
    );
    await user.click(
      within(dialog).getByRole("button", { name: "Change password" }),
    );

    await waitFor(() =>
      expect(notifySuccess).toHaveBeenCalledWith(
        expect.objectContaining({ id: "success.password.changed" }),
      ),
    );
    await waitFor(() =>
      expect(
        screen.queryByRole("dialog", { name: "Change password" }),
      ).not.toBeInTheDocument(),
    );
    // The row keeps describing the action instead of carrying a note.
    expect(
      screen.getByText("Choose a new password for signing in."),
    ).toBeInTheDocument();
  });

  it("keeps a swallowed failure out of the request and reports a mismatch locally", async () => {
    mount(bothSet);
    const user: UserEvent = userEvent.setup();

    await user.click(await screen.findByRole("button", { name: "Change" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Change password",
    });

    await user.type(
      within(dialog).getByLabelText("New password"),
      "a-long-enough-password",
    );
    await user.type(
      within(dialog).getByLabelText("Repeat new password"),
      "different",
    );
    await user.click(
      within(dialog).getByRole("button", { name: "Change password" }),
    );

    expect(
      await screen.findByText("The two passwords do not match."),
    ).toBeInTheDocument();
    expect(
      fetchBoundary.mock.calls.some(([request]) =>
        new URL(request.url).pathname.endsWith("/me/password/set"),
      ),
    ).toBe(false);
  });
});

describe("factor cache", () => {
  it("re-reads factors from the server rather than trusting a stale cache", async () => {
    mount(bothSet);
    await screen.findByText("Change password");
    expect(
      queryClient.getQueryData(factorsQueryOptions().queryKey),
    ).toMatchObject({ passwordSet: true });

    await waitFor(() =>
      expect(
        fetchBoundary.mock.calls.some(([request]) =>
          new URL(request.url).pathname.endsWith("/me/factors"),
        ),
      ).toBe(true),
    );
  });
});
