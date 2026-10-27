import { I18nProvider } from "@lingui/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { ProviderOperatorSection } from "@/pages/admin/identity-providers/ProviderOperatorSection";
import { apiError, type FakeApi, fakeApi } from "@/test/app";

const slug = "vrchat";
const base = `/api/prohibitorum/identity-providers/${slug}/operator-session`;

const notifyError = vi.fn<(error: unknown, scope?: string) => void>();
let api: FakeApi;
let queryClient: ReturnType<typeof createQueryClient>;

beforeEach(() => {
  i18n.activate("en");
  notifyError.mockReset();
});

afterEach(() => {
  queryClient.clear();
});

type Provider = Parameters<typeof ProviderOperatorSection>[0]["provider"];

const provider = (overrides: Partial<Provider> = {}): Provider => ({
  slug,
  displayName: "VRChat",
  secretStatus: "unconfigured",
  secretValidatedAt: null,
  ...overrides,
});

function mount(
  record: Provider,
  routes: Parameters<typeof fakeApi>[0] = {},
): UserEvent {
  api = fakeApi(routes);
  queryClient = createQueryClient(notifyError);
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <ProviderOperatorSection provider={record} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return userEvent.setup();
}

const challenge = {
  status: "challenge",
  challenge: "c-1",
  methods: ["totp", "emailOtp"],
};

/** Opens the dialog and fills the first step. */
async function signInAs(user: UserEvent, password = "hunter2") {
  await user.click(await screen.findByRole("button", { name: /^Sign in/ }));
  const dialog = await screen.findByRole("dialog", {
    name: "Sign in to VRChat",
  });
  await user.type(within(dialog).getByLabelText("Username"), "operator");
  await user.type(within(dialog).getByLabelText("Password"), password);
  await user.click(within(dialog).getByRole("button", { name: "Sign in" }));
  return dialog;
}

describe("the session's state", () => {
  it("says there is none and offers only to sign in", () => {
    mount(provider());
    expect(screen.getByText("Not set up")).toBeVisible();
    expect(screen.queryByText(/^Checked/)).toBeNull();
    expect(screen.queryByRole("button", { name: "Check" })).toBeNull();
    expect(screen.getByRole("button", { name: "Sign in" })).toBeVisible();
  });

  it("says a checked session is valid, and when it was checked", () => {
    mount(
      provider({
        secretStatus: "valid",
        secretValidatedAt: new Date(Date.now() - 86_400_000).toISOString(),
      }),
    );
    expect(screen.getByText("Session valid")).toBeVisible();
    expect(screen.getByText(/^Checked/)).toHaveTextContent("Checked yesterday");
    expect(screen.getByRole("button", { name: "Check" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Sign in again" })).toBeVisible();
    // The row names what it is, and the badge sits with the actions.
    expect(screen.getByText("Operator account")).toBeVisible();
    const actions = screen.getByText("Session valid").parentElement;
    expect(actions).toContainElement(
      screen.getByRole("button", { name: "Check" }),
    );
  });

  it("makes Check the routine action while the session works", () => {
    mount(provider({ secretStatus: "valid" }));
    expect(
      screen.getByRole("button", { name: "Sign in again" }),
    ).toHaveAttribute("data-variant", "outline");
  });

  it.each([
    ["unconfigured", "Sign in"],
    ["invalid", "Sign in again"],
  ])(
    "makes signing in the primary action when the session is %s",
    (secretStatus, name) => {
      mount(provider({ secretStatus }));
      expect(screen.getByRole("button", { name })).toHaveAttribute(
        "data-variant",
        "default",
      );
    },
  );

  it("says an expired session expired", () => {
    mount(provider({ secretStatus: "invalid" }));
    expect(screen.getByText("Session expired")).toBeVisible();
    expect(screen.getByRole("button", { name: "Sign in again" })).toBeVisible();
  });
});

describe("checking the session", () => {
  const configured = provider({ secretStatus: "valid" });

  it("asks the server to validate it", async () => {
    const user = mount(configured, {
      [`POST ${base}/validate`]: () => Response.json({ status: "valid" }),
    });
    await user.click(screen.getByRole("button", { name: "Check" }));
    await waitFor(() =>
      expect(api.sent("POST", `${base}/validate`)).toHaveLength(1),
    );
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("holds Sign in again while the check runs", async () => {
    let release: (response: Response) => void = () => {};
    const user = mount(configured, {
      [`POST ${base}/validate`]: () =>
        new Promise<Response>((resolve) => {
          release = resolve;
        }),
    });
    const check = screen.getByRole("button", { name: "Check" });
    await user.click(check);
    await waitFor(() => expect(check).toHaveAttribute("aria-busy", "true"));
    expect(
      screen.getByRole("button", { name: "Sign in again" }),
    ).toBeDisabled();

    release(Response.json({ status: "valid" }));
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Sign in again" }),
      ).toBeEnabled(),
    );
  });

  it("tells a refused check once, under the row, and not as a toast", async () => {
    const user = mount(configured, {
      [`POST ${base}/validate`]: () => apiError("rate_limited", 429),
    });
    await user.click(screen.getByRole("button", { name: "Check" }));
    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("the sign-in dialog", () => {
  it("signs in with the username and password and closes", async () => {
    const user = mount(provider(), {
      [`POST ${base}/start`]: () => Response.json({ status: "valid" }),
    });
    await signInAs(user);
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(api.sent("POST", `${base}/start`)[0]?.body).toEqual({
      username: "operator",
      password: "hunter2",
    });
  });

  it("starts empty each time it opens", async () => {
    const user = mount(provider());
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    let dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText("Username"), "operator");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(api.sent("POST", `${base}/start`)).toHaveLength(0);

    await user.click(screen.getByRole("button", { name: "Sign in" }));
    dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByLabelText("Username")).toHaveValue("");
  });

  it("asks for a verification method and code when VRChat challenges", async () => {
    const user = mount(provider(), {
      [`POST ${base}/start`]: () => Response.json(challenge),
      [`POST ${base}/verify`]: () => Response.json({ status: "valid" }),
    });
    const dialog = await signInAs(user);

    const methods = await within(dialog).findByRole("radiogroup", {
      name: "Verification method",
    });
    expect(
      within(methods).getByRole("radio", { name: "Authenticator app" }),
    ).toBeChecked();
    expect(
      within(methods).getByRole("radio", { name: "Email code" }),
    ).toBeVisible();
    // The password is not kept on screen behind the second step.
    expect(within(dialog).queryByLabelText("Password")).toBeNull();

    await user.click(
      within(methods).getByRole("radio", { name: "Email code" }),
    );
    await user.keyboard("{Tab}123456");
    await user.click(within(dialog).getByRole("button", { name: "Verify" }));

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(api.sent("POST", `${base}/verify`)[0]?.body).toEqual({
      challenge: "c-1",
      method: "emailOtp",
      code: "123456",
    });
  });

  it("says it is waiting for VRChat, and ignores dismissal meanwhile", async () => {
    let release: (response: Response) => void = () => {};
    const user = mount(provider(), {
      [`POST ${base}/start`]: () =>
        new Promise<Response>((resolve) => {
          release = resolve;
        }),
    });
    const dialog = await signInAs(user);

    expect(
      await within(dialog).findByText("Waiting for VRChat…"),
    ).toBeVisible();
    // One spinner, on the submit; the line beside it is text only.
    const spinners = document.querySelectorAll('[data-slot="spinner"]');
    expect(spinners).toHaveLength(1);
    expect(
      within(dialog).getByRole("button", { name: "Submitting…" }),
    ).toContainElement(spinners[0] as HTMLElement);
    await user.keyboard("{Escape}");
    expect(screen.getByRole("dialog")).toBeVisible();
    expect(
      within(dialog).getByRole("button", { name: "Cancel" }),
    ).toBeDisabled();

    release(Response.json({ status: "valid" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("keeps the dialog on a refused password, once, with the password cleared", async () => {
    const user = mount(provider({ secretStatus: "valid" }), {
      [`POST ${base}/start`]: () =>
        apiError("vrchat_operator_credentials_invalid", 422),
    });
    const dialog = await signInAs(user);

    expect(
      await within(dialog).findAllByText(
        "VRChat did not accept the username or password.",
      ),
    ).toHaveLength(1);
    expect(screen.getByRole("dialog")).toBeVisible();
    expect(within(dialog).getByLabelText("Password")).toHaveValue("");
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("keeps the verification step on a wrong code, once", async () => {
    const user = mount(provider(), {
      [`POST ${base}/start`]: () => Response.json(challenge),
      [`POST ${base}/verify`]: () =>
        apiError("vrchat_operator_code_invalid", 422),
    });
    const dialog = await signInAs(user);
    await within(dialog).findByRole("radiogroup", {
      name: "Verification method",
    });
    await user.click(
      within(dialog).getByRole("radio", { name: "Authenticator app" }),
    );
    await user.keyboard("{Tab}000000");
    await user.click(within(dialog).getByRole("button", { name: "Verify" }));

    expect(
      await within(dialog).findAllByText("The verification code is not valid."),
    ).toHaveLength(1);
    expect(screen.getByRole("dialog")).toBeVisible();
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("closes from its own Cancel button", async () => {
    const user = mount(provider());
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("keeps a half-finished verification through a backdrop click and Escape", async () => {
    const user = mount(provider(), {
      [`POST ${base}/start`]: () => Response.json(challenge),
    });
    const dialog = await signInAs(user);
    await within(dialog).findByRole("radiogroup", {
      name: "Verification method",
    });

    const backdrop = document.querySelector('[data-slot="dialog-backdrop"]');
    if (backdrop === null) throw new Error("no dialog backdrop");
    await user.click(backdrop);
    expect(screen.getByRole("dialog")).toBeVisible();
    await user.click(within(dialog).getByRole("radio", { name: "Email code" }));
    await user.keyboard("{Escape}");
    expect(screen.getByRole("dialog")).toBeVisible();
    expect(within(dialog).queryByRole("button", { name: "Close" })).toBeNull();
    expect(api.sent("POST", `${base}/start`)).toHaveLength(1);
  });

  it("starts over from the first step after Cancel at the verification step", async () => {
    const user = mount(provider(), {
      [`POST ${base}/start`]: () => Response.json(challenge),
    });
    const dialog = await signInAs(user);
    await within(dialog).findByRole("radiogroup", {
      name: "Verification method",
    });
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    await user.click(screen.getByRole("button", { name: "Sign in" }));
    const reopened = await screen.findByRole("dialog");
    expect(within(reopened).getByLabelText("Username")).toBeVisible();
  });

  it("closes on a backdrop click or Escape at the first step", async () => {
    const user = mount(provider());
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    await screen.findByRole("dialog");
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });
});
