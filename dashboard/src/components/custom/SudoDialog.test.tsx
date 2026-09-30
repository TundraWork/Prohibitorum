import { msg } from "@lingui/core/macro";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { createStore, Provider } from "jotai";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/api/errors";
import type { SudoMethod } from "@/api/raw-paths";
import { isSudoCancelled, resetSudo, runWithSudo } from "@/api/sudo";
import { createQueryClient } from "@/app/query-client";
import { defaultLoginAppearance } from "@/components/custom/login-appearance/appearance";
import { SudoDialog } from "@/components/custom/SudoDialog";
import { i18n } from "@/i18n";

const startAuthentication = vi.fn();
const browserSupportsWebAuthn = vi.fn(() => true);
vi.mock("@simplewebauthn/browser", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@simplewebauthn/browser")>()),
  startAuthentication: (...args: unknown[]) => startAuthentication(...args),
  browserSupportsWebAuthn: () => browserSupportsWebAuthn(),
}));

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
let queryClient: QueryClient;
/** Every request made, with the JSON body it carried. */
let sent: { method: string; path: string; body: unknown }[];

const reason = msg({
  id: "sudo.reason.add-passkey",
  message: "Confirm it is you to add a passkey to this account.",
});

function publicConfig(digits: number) {
  return {
    instanceName: "Prohibitorum",
    hasCustomIcon: false,
    iconUrl: "",
    iconEtag: "",
    maintenanceMode: false,
    maintenanceMessage: "",
    loginAppearance: defaultLoginAppearance,
    loginImages: [],
    totp: { issuer: "Prohibitorum", algorithm: "SHA1", digits, period: 30 },
  };
}

function failure(status: number, code: string) {
  return Response.json({ code, requestId: "req-1" }, { status });
}

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  startAuthentication.mockReset();
  browserSupportsWebAuthn.mockReset();
  browserSupportsWebAuthn.mockReturnValue(true);
  sent = [];
  vi.stubGlobal("fetch", fetchBoundary);
  vi.stubGlobal("isSecureContext", true);
  queryClient = createQueryClient(() => undefined);
});

afterEach(() => {
  resetSudo();
  queryClient.clear();
  vi.unstubAllGlobals();
});

/**
 * Mounts the dialog over a server offering `methods`, with the window closed.
 * `answer` intercepts a path before the defaults; returning `undefined` falls
 * through to them.
 */
function mount(
  methods: SudoMethod[],
  {
    digits = 6,
    answer,
  }: {
    digits?: number;
    answer?: (path: string) => Promise<Response> | Response | undefined;
  } = {},
): UserEvent {
  queryClient.setQueryData(["public", "config"], publicConfig(digits));
  fetchBoundary.mockImplementation(async (request) => {
    const path = new URL(request.url).pathname;
    const text = request.method === "GET" ? "" : await request.clone().text();
    sent.push({
      method: request.method,
      path,
      body: text ? JSON.parse(text) : null,
    });
    const canned = answer?.(path);
    if (canned) return canned;
    if (path.endsWith("/me/sudo/methods")) {
      return Response.json({ methods, fresh: false });
    }
    if (path.endsWith("/me/sudo/begin") && request.method === "POST") {
      const body = JSON.parse(text) as { method: SudoMethod };
      return body.method === "webauthn"
        ? Response.json({ challenge: "abc", rpId: "localhost" })
        : new Response(null, { status: 204 });
    }
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <Provider store={createStore()}>
          <SudoDialog />
        </Provider>
      </QueryClientProvider>
    </I18nProvider>,
  );
  return userEvent.setup();
}

/** Parks `perform` in the dialog and waits for the dialog to open. */
async function intercept<T>(perform: () => Promise<T>) {
  const outcome = runWithSudo(perform, reason);
  // Observed from the start, so a rejection is never reported as unhandled.
  const settled = outcome.then(
    (value) => ({ ok: true as const, value }),
    (error: unknown) => ({ ok: false as const, error }),
  );
  const dialog = await screen.findByRole("dialog");
  return { dialog, settled };
}

function requestsTo(suffix: string) {
  return sent.filter((request) => request.path.endsWith(suffix));
}

async function fillPasswordForm(user: UserEvent, dialog: HTMLElement) {
  await user.type(within(dialog).getByLabelText("Current password"), "secret");
  await user.type(
    within(dialog).getByLabelText("Authenticator code"),
    "123456",
  );
}

describe("sudo dialog", () => {
  it("shows both methods side by side, closed by the close button alone", async () => {
    mount(["webauthn", "password_totp"]);
    const { dialog } = await intercept(async () => "done");

    expect(
      within(dialog).getByRole("heading", { name: "Confirm it is you" }),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByText(
        "Confirm it is you to add a passkey to this account.",
      ),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByRole("button", { name: "Verify with a passkey" }),
    ).toBeInTheDocument();
    expect(within(dialog).getByText("or")).toBeInTheDocument();
    expect(within(dialog).getByLabelText("Current password")).toBeVisible();
    expect(within(dialog).getByLabelText("Authenticator code")).toBeVisible();
    expect(
      within(dialog).getByRole("button", { name: "Verify and continue" }),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByRole("button", { name: "Close" }),
    ).toBeInTheDocument();
    expect(
      within(dialog).queryByRole("button", { name: "Cancel" }),
    ).not.toBeInTheDocument();
    // The rules around "or" are decoration; only the word is read out.
    expect(within(dialog).queryAllByRole("separator")).toHaveLength(0);
    // The passkey is the quicker way through, so it leads: the primary style
    // and the focus the dialog opens on.
    expect(
      within(dialog).getByRole("button", { name: "Verify with a passkey" }),
    ).toHaveFocus();
    expect(
      within(dialog).getByRole("button", { name: "Verify with a passkey" }),
    ).toHaveClass("button--primary");
    expect(
      within(dialog).getByRole("button", { name: "Verify and continue" }),
    ).toHaveClass("button--secondary");
  });

  it("shows only the form when the account has no passkey", async () => {
    mount(["password_totp"]);
    const { dialog } = await intercept(async () => "done");

    expect(within(dialog).getByLabelText("Current password")).toHaveFocus();
    expect(
      within(dialog).getByRole("button", { name: "Verify and continue" }),
    ).toHaveClass("button--primary");
    expect(
      within(dialog).queryByRole("button", { name: "Verify with a passkey" }),
    ).not.toBeInTheDocument();
    expect(within(dialog).queryByText("or")).not.toBeInTheDocument();
  });

  it("shows only the passkey button when the account has no password", async () => {
    mount(["webauthn"]);
    const { dialog } = await intercept(async () => "done");

    expect(
      within(dialog).getByRole("button", { name: "Verify with a passkey" }),
    ).toHaveClass("button--primary");
    expect(
      within(dialog).queryByLabelText("Current password"),
    ).not.toBeInTheDocument();
    expect(
      within(dialog).queryByRole("button", { name: "Verify and continue" }),
    ).not.toBeInTheDocument();
    expect(within(dialog).queryByText("or")).not.toBeInTheDocument();
  });

  it("says so when the account has no way to verify", async () => {
    mount([]);
    const { dialog } = await intercept(async () => "done");

    expect(
      within(dialog).getByText(
        /This account has no way to confirm your identity from here/,
      ),
    ).toBeInTheDocument();
    expect(within(dialog).queryAllByRole("button")).toHaveLength(1);
  });

  it("cancels from the close button without running the operation", async () => {
    const user = mount(["password_totp"]);
    const perform = vi.fn(async () => "done");
    const { dialog, settled } = await intercept(perform);

    await user.click(within(dialog).getByRole("button", { name: "Close" }));

    const result = await settled;
    expect(result.ok).toBe(false);
    expect(!result.ok && isSudoCancelled(result.error)).toBe(true);
    expect(perform).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
  });

  it("cancels on Escape without running the operation", async () => {
    const user = mount(["password_totp"]);
    const perform = vi.fn(async () => "done");
    const { settled } = await intercept(perform);

    await user.keyboard("{Escape}");

    const result = await settled;
    expect(!result.ok && isSudoCancelled(result.error)).toBe(true);
    expect(perform).not.toHaveBeenCalled();
  });

  it("verifies with the password and code, then replays the operation once", async () => {
    const user = mount(["webauthn", "password_totp"]);
    const perform = vi.fn(async () => "replayed");
    const { dialog, settled } = await intercept(perform);

    await fillPasswordForm(user, dialog);
    await user.click(
      within(dialog).getByRole("button", { name: "Verify and continue" }),
    );

    expect(await settled).toEqual({ ok: true, value: "replayed" });
    expect(perform).toHaveBeenCalledTimes(1);
    expect(requestsTo("/me/sudo/begin").map((call) => call.body)).toEqual([
      { method: "password_totp" },
    ]);
    expect(requestsTo("/me/sudo/complete").map((call) => call.body)).toEqual([
      { current_password: "secret", totp_code: "123456" },
    ]);
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
  });

  it("stays open with the error when the password or code is wrong", async () => {
    const user = mount(["password_totp"], {
      answer: (path) =>
        path.endsWith("/me/sudo/complete")
          ? failure(401, "bad_credentials")
          : undefined,
    });
    const perform = vi.fn(async () => "replayed");
    const { dialog } = await intercept(perform);

    await fillPasswordForm(user, dialog);
    await user.click(
      within(dialog).getByRole("button", { name: "Verify and continue" }),
    );

    expect(
      await within(dialog).findByText("The credentials could not be verified."),
    ).toBeInTheDocument();
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(perform).not.toHaveBeenCalled();
  });

  it("hands a failed replay back to the caller and closes", async () => {
    const user = mount(["password_totp"]);
    const refused = new ApiError({
      kind: "http",
      status: 409,
      code: "conflict",
    });
    const perform = vi.fn(async () => {
      throw refused;
    });
    const { dialog, settled } = await intercept(perform);

    await fillPasswordForm(user, dialog);
    await user.click(
      within(dialog).getByRole("button", { name: "Verify and continue" }),
    );

    expect(await settled).toEqual({ ok: false, error: refused });
    expect(perform).toHaveBeenCalledTimes(1);
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
  });

  it("cannot be closed while the verification is in flight", async () => {
    let answerComplete: (response: Response) => void = () => {};
    const user = mount(["webauthn", "password_totp"], {
      answer: (path) =>
        path.endsWith("/me/sudo/complete")
          ? new Promise<Response>((resolve) => {
              answerComplete = resolve;
            })
          : undefined,
    });
    const perform = vi.fn(async () => "replayed");
    const { dialog, settled } = await intercept(perform);

    await fillPasswordForm(user, dialog);
    await user.click(
      within(dialog).getByRole("button", { name: "Verify and continue" }),
    );
    await waitFor(() =>
      expect(requestsTo("/me/sudo/complete")).toHaveLength(1),
    );

    expect(
      within(dialog).getByRole("button", { name: "Close" }),
    ).toBeDisabled();
    expect(
      within(dialog).getByRole("button", { name: "Verify with a passkey" }),
    ).toBeDisabled();
    await user.keyboard("{Escape}");
    expect(screen.getByRole("dialog")).toBeInTheDocument();

    answerComplete(new Response(null, { status: 204 }));
    expect(await settled).toEqual({ ok: true, value: "replayed" });
  });

  it("locks the form while the passkey runs and clears its failure on submit", async () => {
    let rejectCeremony: (error: unknown) => void = () => {};
    startAuthentication.mockImplementation(
      () =>
        new Promise((_, reject) => {
          rejectCeremony = reject;
        }),
    );
    const user = mount(["webauthn", "password_totp"]);
    const { dialog } = await intercept(async () => "replayed");

    await user.click(
      within(dialog).getByRole("button", { name: "Verify with a passkey" }),
    );
    await waitFor(() => expect(startAuthentication).toHaveBeenCalledTimes(1));

    expect(within(dialog).getByLabelText("Current password")).toBeDisabled();
    expect(
      within(dialog).getByRole("button", { name: "Verify and continue" }),
    ).toBeDisabled();
    expect(
      within(dialog).getByRole("button", { name: "Close" }),
    ).toBeDisabled();

    const reads = requestsTo("/me/sudo/methods").length;
    rejectCeremony(
      new ApiError({ kind: "http", status: 401, code: "bad_credentials" }),
    );
    const alert = await within(dialog).findByRole("alert");
    expect(alert).toHaveTextContent("The credentials could not be verified.");
    // A failed ceremony has to begin again, so the method list is re-read.
    await waitFor(() =>
      expect(requestsTo("/me/sudo/methods")).toHaveLength(reads + 1),
    );

    await fillPasswordForm(user, dialog);
    await user.click(
      within(dialog).getByRole("button", { name: "Verify and continue" }),
    );
    await waitFor(() =>
      expect(
        screen.queryByText("The credentials could not be verified."),
      ).not.toBeInTheDocument(),
    );
  });

  it("stays quiet when the passkey prompt is dismissed", async () => {
    startAuthentication.mockRejectedValue(
      new DOMException("The operation was aborted.", "AbortError"),
    );
    const user = mount(["webauthn", "password_totp"]);
    const { dialog } = await intercept(async () => "replayed");

    await user.click(
      within(dialog).getByRole("button", { name: "Verify with a passkey" }),
    );
    await waitFor(() =>
      expect(
        within(dialog).getByRole("button", { name: "Verify with a passkey" }),
      ).toBeEnabled(),
    );
    expect(within(dialog).queryByRole("alert")).not.toBeInTheDocument();
  });

  it("verifies with a passkey, then replays the operation once", async () => {
    startAuthentication.mockResolvedValue({
      id: "credential-id",
      rawId: "credential-id",
      type: "public-key",
      response: { clientDataJSON: "a", authenticatorData: "b", signature: "c" },
      clientExtensionResults: {},
    });
    const user = mount(["webauthn"]);
    const perform = vi.fn(async () => "replayed");
    const { dialog, settled } = await intercept(perform);

    await user.click(
      within(dialog).getByRole("button", { name: "Verify with a passkey" }),
    );

    expect(await settled).toEqual({ ok: true, value: "replayed" });
    expect(perform).toHaveBeenCalledTimes(1);
    expect(requestsTo("/me/sudo/begin").map((call) => call.body)).toEqual([
      { method: "webauthn" },
    ]);
  });

  it("disables the passkey button where the browser cannot use one", async () => {
    browserSupportsWebAuthn.mockReturnValue(false);
    mount(["webauthn", "password_totp"]);
    const { dialog } = await intercept(async () => "done");

    // The tooltip's trigger wraps the button and takes the hover and focus the
    // disabled button cannot, so the name is found on both.
    const passkey = within(dialog)
      .getAllByRole("button", { name: "Verify with a passkey" })
      .find((element) => element.tagName === "BUTTON");
    expect(passkey).toBeDisabled();
    // The password is then the way through, so it leads instead.
    expect(passkey).toHaveClass("button--secondary");
    expect(
      within(dialog).getByRole("button", { name: "Verify and continue" }),
    ).toHaveClass("button--primary");
    expect(within(dialog).getByLabelText("Current password")).toHaveFocus();
  });

  it("draws as many code slots as the instance's codes have digits", async () => {
    mount(["password_totp"], { digits: 8 });
    const { dialog } = await intercept(async () => "done");

    expect(
      dialog.querySelectorAll('[data-slot="input-otp-slot"]'),
    ).toHaveLength(8);
  });

  it("checks each field when it loses focus", async () => {
    const user = mount(["password_totp"]);
    const { dialog } = await intercept(async () => "done");

    await user.click(within(dialog).getByLabelText("Current password"));
    await user.tab();
    expect(
      await within(dialog).findByText(
        "Enter your current password, up to 1024 UTF-8 bytes.",
      ),
    ).toBeInTheDocument();

    await user.type(within(dialog).getByLabelText("Authenticator code"), "123");
    await user.tab();
    expect(
      await within(dialog).findByText(
        "Enter the code your authenticator shows, using only 0–9.",
      ),
    ).toBeInTheDocument();
  });
});
