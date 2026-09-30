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
import { configureSudo, resetSudo, type SudoRequest } from "@/api/sudo";
import { createQueryClient } from "@/app/query-client";
import { searchSerialization } from "@/app/search-params";
import { defaultLoginAppearance } from "@/components/custom/login-appearance/appearance";
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
    loginAppearance: defaultLoginAppearance,
    loginImages: [],
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
    ...searchSerialization,
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

/** Requests to one endpoint, by the end of its path. */
function requests(suffix: string) {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter((request) => new URL(request.url).pathname.endsWith(suffix));
}

function freshSudo() {
  configureSudo({
    queryClient,
    set: () => {},
    setFresh: () => {},
    getFresh: () => true,
  });
}

const bothSet = {
  passwordSet: true,
  totpEnrolled: true,
  passkeyCount: 1,
  recoveryCodesRemaining: 8,
};
const neither = {
  passwordSet: false,
  totpEnrolled: false,
  passkeyCount: 1,
  recoveryCodesRemaining: 0,
};

/** The list's row whose title is `name`. */
function row(name: string) {
  const list = screen.getByRole("list", {
    name: "Password and authenticator",
  });
  const item = within(list)
    .getAllByRole("listitem")
    .find((candidate) => within(candidate).queryByText(name) !== null);
  if (!item) throw new Error(`No row named ${name}`);
  return item;
}

describe("password and authenticator rows", () => {
  it("lists every factor with its action when both exist, and never calls the combined endpoint", async () => {
    mount(bothSet);

    await screen.findByText("Password");
    expect(within(row("Password")).getByText("Set")).toBeInTheDocument();
    expect(
      within(row("Password")).getByRole("button", { name: "Change" }),
    ).toBeInTheDocument();
    expect(within(row("Authenticator")).getByText("Set")).toBeInTheDocument();
    expect(
      within(row("Authenticator")).getByRole("button", { name: "Replace" }),
    ).toBeInTheDocument();
    expect(
      within(row("Recovery codes")).getByText("8 codes left"),
    ).toBeInTheDocument();
    expect(
      within(row("Recovery codes")).getByRole("button", {
        name: "Regenerate",
      }),
    ).toBeInTheDocument();
    expect(
      within(row("Turn off password and authenticator")).getByRole("button", {
        name: "Turn off",
      }),
    ).toBeEnabled();
    expect(
      screen.queryByRole("button", {
        name: "Set up password and authenticator",
      }),
    ).not.toBeInTheDocument();

    // The forms wait in dialogs until the user asks for one.
    expect(screen.queryByLabelText("New password")).not.toBeInTheDocument();

    // Reading the state is not a write.
    const writes = fetchBoundary.mock.calls.filter(
      ([request]) => request.method !== "GET",
    );
    expect(writes).toEqual([]);
    expect(requests("/me/password-totp/verify")).toHaveLength(0);
  });

  it("offers only the combined setup when neither exists", async () => {
    mount(neither);

    expect(
      await screen.findByRole("button", {
        name: "Set up password and authenticator",
      }),
    ).toBeInTheDocument();
    const list = screen.getByRole("list", {
      name: "Password and authenticator",
    });
    expect(within(list).getAllByRole("listitem")).toHaveLength(2);
    expect(within(row("Password")).getByText("Not set")).toBeInTheDocument();
    expect(
      within(row("Authenticator")).getByText("Not set"),
    ).toBeInTheDocument();
    // Nothing to change, regenerate or turn off.
    expect(within(list).queryAllByRole("button")).toHaveLength(0);
    expect(screen.queryByText("Recovery codes")).not.toBeInTheDocument();
    expect(
      screen.queryByText("Turn off password and authenticator"),
    ).not.toBeInTheDocument();
  });

  it("keeps a lone password legible and repairable, and warns that the setup replaces it", async () => {
    mount({ ...neither, passwordSet: true });
    const user = userEvent.setup();

    await screen.findByText("Password");
    expect(within(row("Password")).getByText("Set")).toBeInTheDocument();
    expect(
      within(row("Authenticator")).getByText("Not set"),
    ).toBeInTheDocument();
    expect(screen.queryByText("Recovery codes")).not.toBeInTheDocument();
    expect(
      screen.getByText("Turn off password and authenticator"),
    ).toBeInTheDocument();

    await user.click(
      screen.getByRole("button", { name: "Set up password and authenticator" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Set a password",
    });
    expect(
      within(dialog).getByText(
        "Once this is done, your current password, authenticator and recovery codes are all replaced.",
      ),
    ).toBeInTheDocument();
  });

  it("marks recovery codes that are running low or used up", async () => {
    mount({ ...bothSet, recoveryCodesRemaining: 2 });
    await screen.findByText("Recovery codes");
    expect(within(row("Recovery codes")).getByText("2 codes left"));
    expect(
      within(row("Recovery codes")).getByText("Running low"),
    ).toBeInTheDocument();
  });

  it("marks recovery codes that are all used", async () => {
    mount({ ...bothSet, recoveryCodesRemaining: 0 });
    await screen.findByText("Recovery codes");
    expect(within(row("Recovery codes")).getByText("All used"));
    expect(screen.queryByText("Running low")).not.toBeInTheDocument();
  });

  it("disables turning off the last way in and says why on the button", async () => {
    mount({ ...bothSet, passkeyCount: 0 });
    const user = userEvent.setup();

    // The tooltip's trigger wraps the disabled button, and takes its name.
    const [trigger, turnOff] = await screen.findAllByRole("button", {
      name: "Turn off",
    });
    expect(turnOff).toBeDisabled();
    await user.hover(trigger as HTMLElement);
    expect(await screen.findByRole("tooltip")).toHaveTextContent(
      "This is your only way to sign in. Add a passkey first.",
    );
  });
});

describe("setting up both", () => {
  async function openSetup(user: UserEvent) {
    await user.click(
      await screen.findByRole("button", {
        name: "Set up password and authenticator",
      }),
    );
    return screen.findByRole("dialog", { name: "Set a password" });
  }

  it("stays on the password step until both passwords are valid", async () => {
    mount(neither);
    const user = userEvent.setup();
    const dialog = await openSetup(user);

    // No warning about replacing: there is nothing to replace.
    expect(within(dialog).queryByText(/replaced/)).not.toBeInTheDocument();

    await user.type(within(dialog).getByLabelText("New password"), "short");
    await user.click(within(dialog).getByRole("button", { name: "Next" }));
    expect(
      await within(dialog).findByText(
        "Use a password of at least 8 characters.",
      ),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByRole("heading", { name: "Set a password" }),
    ).toBeInTheDocument();

    await user.clear(within(dialog).getByLabelText("New password"));
    await user.type(
      within(dialog).getByLabelText("New password"),
      "a-long-enough-password",
    );
    await user.type(
      within(dialog).getByLabelText("Repeat new password"),
      "a-different-password",
    );
    await user.click(within(dialog).getByRole("button", { name: "Next" }));
    expect(
      await within(dialog).findByText("The two passwords do not match."),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByRole("heading", { name: "Set a password" }),
    ).toBeInTheDocument();
    // The code is the next step's, so it has not been asked for.
    expect(
      within(dialog).queryByText(
        "Enter the code your authenticator shows, using only 0–9.",
      ),
    ).not.toBeInTheDocument();
    expect(requests("/me/password-totp/verify")).toHaveLength(0);
  });

  it("sends the password, secret and code together through the identity check, then shows the first codes in the dialog", async () => {
    const codes = ["ABCD-EFGH-IJKL-MN23", "QRST-UVWX-YZ23-4567"];
    const factorsNow = { ...neither };
    mount(factorsNow, (path) => {
      if (path.endsWith("/me/password-totp/verify")) {
        factorsNow.passwordSet = true;
        factorsNow.totpEnrolled = true;
        factorsNow.recoveryCodesRemaining = codes.length;
        return json({ recovery_codes: codes });
      }
      if (path.endsWith("/me/sudo/methods")) {
        return json({ methods: ["password_totp"], fresh: false });
      }
      return undefined;
    });
    // The window is closed, so the write waits on the identity check; the
    // test stands in for the dialog and replays it once "verified".
    const asked: SudoRequest[] = [];
    configureSudo({
      queryClient,
      set: (request) => {
        if (request) asked.push(request);
      },
      setFresh: () => {},
      getFresh: () => false,
    });
    const user = userEvent.setup();
    const dialog = await openSetup(user);

    await user.type(
      within(dialog).getByLabelText("New password"),
      "a-long-enough-password",
    );
    await user.type(
      within(dialog).getByLabelText("Repeat new password"),
      "a-long-enough-password",
    );
    await user.click(within(dialog).getByRole("button", { name: "Next" }));
    expect(
      await within(dialog).findByRole("heading", {
        name: "Set up an authenticator",
      }),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByRole("textbox", { name: "Setup key" }),
    ).toBeInTheDocument();
    // The control that moved on is gone, so the step starts at its code.
    await waitFor(() =>
      expect(
        within(dialog).getByLabelText("Code from your authenticator"),
      ).toHaveFocus(),
    );

    await user.type(
      within(dialog).getByLabelText("Code from your authenticator"),
      "123456",
    );
    await user.click(within(dialog).getByRole("button", { name: "Turn on" }));

    await waitFor(() => expect(asked).toHaveLength(1));
    expect(asked[0]?.reason).toMatchObject({
      id: "sudo.reason.set-password-totp",
    });
    expect(requests("/me/password-totp/verify")).toHaveLength(0);
    const request = asked[0];
    if (!request) throw new Error("no sudo request");
    request.perform().then(request.resolve, request.reject);

    const reveal = await screen.findByRole("dialog", {
      name: "Save your recovery codes",
    });
    const verify = requests("/me/password-totp/verify");
    expect(verify).toHaveLength(1);
    const body = JSON.parse(await (verify[0] as Request).clone().text());
    expect(body).toMatchObject({
      password: "a-long-enough-password",
      code: "123456",
    });
    // The secret is generated in the browser and must be a valid Base32 secret.
    expect(body.secret_base32).toMatch(/^[A-Z2-7]{32}$/);
    // Never the authenticator-only route, which would leave the password unset.
    expect(requests("/me/totp/verify")).toHaveLength(0);

    // The first codes: nothing old is said to be replaced.
    expect(
      within(reveal).getByRole("textbox", { name: "New recovery codes" }),
    ).toHaveValue(codes.join("\n"));
    expect(within(reveal).queryByText(/old recovery codes/)).toBeNull();

    // Escape does not close it; only saving and continuing does.
    await user.keyboard("{Escape}");
    expect(
      screen.getByRole("dialog", { name: "Save your recovery codes" }),
    ).toBeInTheDocument();
    const leave = within(reveal).getByRole("button", { name: "Continue" });
    expect(leave).toBeDisabled();
    await user.click(
      within(reveal).getByRole("checkbox", {
        name: "I have saved my recovery codes",
      }),
    );
    await user.click(leave);

    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    // Both exist now: the setup button is gone and focus is on the first
    // action the rows offer.
    const change = await screen.findByRole("button", { name: "Change" });
    await waitFor(() => expect(change).toHaveFocus());
    expect(
      screen.queryByRole("button", {
        name: "Set up password and authenticator",
      }),
    ).not.toBeInTheDocument();
  });

  it("goes back to the password step when the server refuses the password", async () => {
    mount(neither, (path) =>
      path.endsWith("/me/password-totp/verify")
        ? Response.json(
            {
              code: "validation_failed",
              requestId: "panel-test",
              details: { location: "password" },
            },
            { status: 400 },
          )
        : undefined,
    );
    freshSudo();
    const user = userEvent.setup();
    const dialog = await openSetup(user);

    await user.type(
      within(dialog).getByLabelText("New password"),
      "a-long-enough-password",
    );
    await user.type(
      within(dialog).getByLabelText("Repeat new password"),
      "a-long-enough-password",
    );
    await user.click(within(dialog).getByRole("button", { name: "Next" }));
    await user.type(
      await within(dialog).findByLabelText("Code from your authenticator"),
      "123456",
    );
    await user.click(within(dialog).getByRole("button", { name: "Turn on" }));

    expect(
      await within(dialog).findByRole("heading", { name: "Set a password" }),
    ).toBeInTheDocument();
    expect(within(dialog).getByLabelText("New password")).toHaveAttribute(
      "aria-invalid",
      "true",
    );
  });
});

describe("changing one factor", () => {
  it("reveals regenerated recovery codes in a dialog rather than in place of the panel", async () => {
    const codes = ["ABCD-EFGH-IJKL-MN23", "QRST-UVWX-YZ23-4567"];
    mount(bothSet, (path) =>
      path.endsWith("/me/recovery-codes/regenerate")
        ? json({ recovery_codes: codes })
        : undefined,
    );
    freshSudo();
    const user: UserEvent = userEvent.setup();

    await user.click(await screen.findByRole("button", { name: "Regenerate" }));
    // The old codes stop working, so nothing is sent until that is confirmed.
    const confirm = await screen.findByRole("alertdialog", {
      name: "Replace your recovery codes?",
    });
    expect(requests("/me/recovery-codes/regenerate")).toHaveLength(0);
    await user.click(
      within(confirm).getByRole("button", { name: "Replace codes" }),
    );

    const dialog = await screen.findByRole("dialog", {
      name: "Save your new recovery codes",
    });
    expect(requests("/me/recovery-codes/regenerate")).toHaveLength(1);
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

    await user.click(await screen.findByRole("button", { name: "Regenerate" }));
    const confirm = await screen.findByRole("alertdialog", {
      name: "Replace your recovery codes?",
    });
    await user.click(within(confirm).getByRole("button", { name: "Cancel" }));

    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(requests("/me/recovery-codes/regenerate")).toHaveLength(0);
  });

  it("warns in the replace dialog that the current authenticator stops working", async () => {
    mount(bothSet);
    const user = userEvent.setup();

    await user.click(await screen.findByRole("button", { name: "Replace" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Replace authenticator",
    });
    expect(
      within(dialog).getByText(
        "Your current authenticator and every existing recovery code stop working as soon as this succeeds.",
      ),
    ).toBeInTheDocument();
  });

  it("closes the dialog and announces the change once the password is set", async () => {
    mount(bothSet);
    freshSudo();
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
  });

  it("reports a mismatch locally and sends nothing", async () => {
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
    expect(requests("/me/password/set")).toHaveLength(0);
  });
});

describe("factor cache", () => {
  it("re-reads factors from the server rather than trusting a stale cache", async () => {
    mount(bothSet);
    await screen.findByText("Password");
    expect(
      queryClient.getQueryData(factorsQueryOptions().queryKey),
    ).toMatchObject({ passwordSet: true });

    await waitFor(() =>
      expect(requests("/me/factors").length).toBeGreaterThan(0),
    );
  });
});
