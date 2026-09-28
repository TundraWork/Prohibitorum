import { browserSupportsWebAuthn } from "@simplewebauthn/browser";
import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import { loadDocument } from "@/app/load-document";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import {
  apiError,
  fakeApi,
  publicApi,
  renderApp,
  testSession,
} from "@/test/app";

vi.mock("@/app/load-document", () => ({ loadDocument: vi.fn() }));
vi.mock("qrcode", () => ({
  default: { toCanvas: vi.fn().mockResolvedValue(undefined) },
}));
vi.mock("@simplewebauthn/browser", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@simplewebauthn/browser")>()),
  browserSupportsWebAuthn: vi.fn(() => false),
  startRegistration: vi.fn(),
}));

type Preview = components["schemas"]["EnrollmentPreview"];

const token = "tok-1";
const address = `/enroll/${token}`;
const previewPath = `/api/prohibitorum/enrollments/${token}`;
const gitlab = { slug: "gitlab", displayName: "GitLab", protocol: "oidc" };
const github = { slug: "github", displayName: "GitHub", protocol: "oidc" };

function preview(overrides: Partial<Preview> = {}): Preview {
  return {
    intent: "invite",
    expiresAt: "2026-10-03T12:00:00Z",
    allowedMethods: ["passkey", "password_totp"],
    ...overrides,
  };
}

let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  queryClient = createQueryClient(() => undefined);
  vi.mocked(browserSupportsWebAuthn).mockReturnValue(false);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
  vi.mocked(loadDocument).mockReset();
});

function enrollApi(body: Preview = preview()) {
  return fakeApi({
    ...publicApi(null),
    [`GET ${previewPath}`]: () => Response.json(body),
  });
}

/** A browser with passkeys, so the passkey button can be pressed. */
function withPasskeys() {
  vi.stubGlobal("isSecureContext", true);
  vi.mocked(browserSupportsWebAuthn).mockReturnValue(true);
}

/**
 * The passkey button itself. Where passkeys are unavailable HeroUI's tooltip
 * trigger wraps it in an element with the button role and the same name.
 */
function passkeyButton() {
  const found = screen
    .getAllByRole("button", { name: "Create a passkey" })
    .find((element) => element.tagName === "BUTTON");
  if (!found) throw new Error("no passkey button");
  return found;
}

async function passwordTotp(user: UserEvent) {
  await user.click(
    screen.getByRole("button", {
      name: "Use a password and an authenticator instead",
    }),
  );
  await screen.findByRole("heading", { name: "Set a password" });
  await user.type(screen.getByLabelText("Password"), "correct horse");
  await user.type(screen.getByLabelText("Confirm password"), "correct horse");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  await screen.findByRole("heading", { name: "Set up an authenticator" });
  await user.type(
    screen.getByRole("textbox", { name: "Authenticator code" }),
    "123456",
  );
  await user.click(screen.getByRole("button", { name: "Verify and finish" }));
}

describe("enrollment", () => {
  it("invites with both names to choose, every method, and the providers under a separator", async () => {
    enrollApi(preview({ providers: [gitlab, github] }));
    renderApp(address, queryClient);
    expect(
      await screen.findByRole("heading", { name: "Create your account" }),
    ).toHaveFocus();
    expect(
      screen.getByText(
        "You've been invited to Test instance. This link works until Oct 3, 2026.",
      ),
    ).toBeVisible();
    expect(screen.getByRole("textbox", { name: "Username" })).toHaveValue("");
    expect(screen.getByRole("textbox", { name: "Display name" })).toBeVisible();
    expect(passkeyButton()).toBeVisible();
    expect(
      screen.getByRole("button", {
        name: "Use a password and an authenticator instead",
      }),
    ).toBeVisible();
    expect(screen.getByText("or")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Continue with GitLab" }),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Continue with GitHub" }),
    ).toBeVisible();
  });

  it("creates the first administrator with a passkey alone and no separator", async () => {
    enrollApi(preview({ intent: "bootstrap", allowedMethods: ["passkey"] }));
    renderApp(address, queryClient);
    expect(
      await screen.findByRole("heading", {
        name: "Create the administrator account",
      }),
    ).toBeVisible();
    expect(
      screen.getByText("This is the first account on Test instance."),
    ).toBeVisible();
    expect(screen.getByRole("textbox", { name: "Username" })).toBeVisible();
    expect(
      screen.queryByRole("button", {
        name: "Use a password and an authenticator instead",
      }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("or")).not.toBeInTheDocument();
  });

  it("fills in the display name a VRChat registration suggests", async () => {
    enrollApi(
      preview({
        intent: "federated_register",
        suggestedDisplayName: "Alice VR",
      }),
    );
    renderApp(address, queryClient);
    expect(
      await screen.findByText(
        "Your VRChat profile is verified. Choose your account name, then set up how you'll sign in.",
      ),
    ).toBeVisible();
    expect(screen.getByRole("textbox", { name: "Display name" })).toHaveValue(
      "Alice VR",
    );
  });

  it("names the account a reset is for and asks for no fields", async () => {
    enrollApi(
      preview({
        intent: "reset",
        target: { username: "alice", displayName: "Alice" },
      }),
    );
    renderApp(address, queryClient);
    expect(
      await screen.findByRole("heading", {
        name: "Set up a new sign-in for Alice",
      }),
    ).toBeVisible();
    expect(screen.getByText("@alice")).toBeVisible();
    expect(
      screen.queryByRole("textbox", { name: "Username" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("textbox", { name: "Display name" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("form", { name: "Set up a new sign-in for Alice" }),
    ).toBeVisible();
  });

  it("goes on to the password setup on Enter where passkeys are unavailable", async () => {
    enrollApi();
    const user = userEvent.setup();
    renderApp(address, queryClient);
    await user.type(
      await screen.findByRole("textbox", { name: "Username" }),
      "alice",
    );
    await user.type(
      screen.getByRole("textbox", { name: "Display name" }),
      "Alice{Enter}",
    );
    expect(
      await screen.findByRole("heading", { name: "Set a password" }),
    ).toBeVisible();
  });

  it("keeps the authenticator's secret when a taken username sends the reader back", async () => {
    const api = enrollApi();
    api.routes[`POST ${previewPath}/password-totp/verify`] = () =>
      apiError("username_taken", 409);
    const user = userEvent.setup();
    renderApp(address, queryClient);
    await user.type(
      await screen.findByRole("textbox", { name: "Username" }),
      "alice",
    );
    await user.type(
      screen.getByRole("textbox", { name: "Display name" }),
      "Alice",
    );
    await passwordTotp(user);
    const first = api.sent("POST", `${previewPath}/password-totp/verify`)[0]
      ?.body as { secret_base32: string };
    const username = await screen.findByRole("textbox", { name: "Username" });
    await user.clear(username);
    await user.type(username, "alice2");
    await passwordTotp(user);
    await waitFor(() =>
      expect(
        api.sent("POST", `${previewPath}/password-totp/verify`),
      ).toHaveLength(2),
    );
    const second = api.sent("POST", `${previewPath}/password-totp/verify`)[1]
      ?.body as { secret_base32: string; username: string };
    expect(second.secret_base32).toBe(first.secret_base32);
    expect(second.username).toBe("alice2");
  });

  it("shows a username the link fixes as read-only", async () => {
    enrollApi(preview({ username: "fixed" }));
    renderApp(address, queryClient);
    const username = await screen.findByRole("textbox", { name: "Username" });
    expect(username).toHaveValue("fixed");
    expect(username).toHaveAttribute("readonly");
  });

  it("offers only the provider an invitation is bound to", async () => {
    enrollApi(
      preview({
        providers: [gitlab, github],
        expectedUpstreamIdpSlug: "gitlab",
      }),
    );
    const user = userEvent.setup();
    renderApp(address, queryClient);
    expect(
      await screen.findByText(
        "This invitation needs the account to be created through GitLab.",
      ),
    ).toBeVisible();
    const buttons = within(screen.getByRole("main")).getAllByRole("button");
    expect(buttons).toHaveLength(1);
    expect(buttons[0]).toHaveAccessibleName("Continue with GitLab");
    expect(screen.queryByText("or")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("textbox", { name: "Username" }),
    ).not.toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: "Continue with GitLab" }),
    );
    expect(loadDocument).toHaveBeenCalledWith(
      `/api/prohibitorum/enrollments/${token}/start-federation?provider=gitlab`,
    );
  });

  it("disables the passkey button where passkeys are unavailable", async () => {
    enrollApi();
    renderApp(address, queryClient);
    await screen.findByRole("heading", { name: "Create your account" });
    expect(passkeyButton()).toBeDisabled();
  });

  it.each([
    ["username_taken", "Username", "That username is already taken."],
    [
      "invalid_username",
      "Username",
      "That username is not in a format this instance accepts.",
    ],
    [
      "username_immutable",
      "Username",
      "A username cannot be changed once the account exists.",
    ],
    [
      "invalid_display_name",
      "Display name",
      "Enter a display name of 1–128 characters, without control characters.",
    ],
  ])("marks %s on the %s field", async (code, label, message) => {
    withPasskeys();
    const api = enrollApi();
    api.routes[`POST ${previewPath}/register/begin`] = () =>
      apiError(code, 400);
    const user = userEvent.setup();
    renderApp(address, queryClient);
    await user.type(
      await screen.findByRole("textbox", { name: "Username" }),
      "alice",
    );
    await user.type(
      screen.getByRole("textbox", { name: "Display name" }),
      "Alice",
    );
    await user.click(passkeyButton());
    const field = screen.getByRole("textbox", { name: label });
    await waitFor(() => expect(field).toHaveAttribute("aria-invalid", "true"));
    expect(screen.getByText(message)).toBeVisible();
    expect(api.sent("POST", `${previewPath}/register/begin`)[0]?.body).toEqual({
      username: "alice",
      displayName: "Alice",
    });
  });

  it("sends the chosen names with the password and authenticator", async () => {
    const api = enrollApi();
    api.routes[`POST ${previewPath}/password-totp/verify`] = () =>
      Response.json({
        session: testSession,
        recoveryCodes: ["AAAA-BBBB-CCCC-DDDD"],
      });
    const user = userEvent.setup();
    renderApp(address, queryClient);
    await user.type(
      await screen.findByRole("textbox", { name: "Username" }),
      "alice",
    );
    await user.type(
      screen.getByRole("textbox", { name: "Display name" }),
      "Alice",
    );
    await passwordTotp(user);
    expect(
      await screen.findByRole("heading", { name: "Save your recovery codes" }),
    ).toBeVisible();
    const [sent] = api.sent("POST", `${previewPath}/password-totp/verify`);
    expect(sent?.body).toMatchObject({
      username: "alice",
      displayName: "Alice",
      password: "correct horse",
      code: "123456",
    });
    const body = sent?.body as { secret_base32?: string } | undefined;
    expect(body?.secret_base32).toMatch(/^[A-Z2-7]{32}$/);
  });

  it("sends no names for a reset", async () => {
    const api = enrollApi(
      preview({
        intent: "reset",
        target: { username: "alice", displayName: "Alice" },
      }),
    );
    api.routes[`POST ${previewPath}/password-totp/verify`] = () =>
      Response.json({
        session: testSession,
        recoveryCodes: ["AAAA-BBBB-CCCC-DDDD"],
      });
    const user = userEvent.setup();
    renderApp(address, queryClient);
    await screen.findByRole("heading", {
      name: "Set up a new sign-in for Alice",
    });
    await passwordTotp(user);
    await screen.findByRole("heading", { name: "Save your recovery codes" });
    const body = api.sent("POST", `${previewPath}/password-totp/verify`)[0]
      ?.body as Record<string, unknown>;
    expect(Object.keys(body).sort()).toEqual([
      "code",
      "password",
      "secret_base32",
    ]);
  });

  it("returns to the first step when the username is taken in the meantime", async () => {
    const api = enrollApi();
    api.routes[`POST ${previewPath}/password-totp/verify`] = () =>
      apiError("username_taken", 409);
    const user = userEvent.setup();
    renderApp(address, queryClient);
    await user.type(
      await screen.findByRole("textbox", { name: "Username" }),
      "alice",
    );
    await user.type(
      screen.getByRole("textbox", { name: "Display name" }),
      "Alice",
    );
    await passwordTotp(user);
    const username = await screen.findByRole("textbox", { name: "Username" });
    await waitFor(() =>
      expect(username).toHaveAttribute("aria-invalid", "true"),
    );
    await waitFor(() => expect(username).toHaveFocus());
    expect(username).toHaveValue("alice");
    expect(
      screen.getByRole("heading", { name: "Create your account" }),
    ).toBeVisible();
  });

  it("says a used or expired link is gone", async () => {
    fakeApi({
      ...publicApi(null),
      [`GET ${previewPath}`]: () => apiError("enrollment_expired", 410),
    });
    renderApp(address, queryClient);
    expect(
      await screen.findByRole("heading", { name: "Unable to load this page" }),
    ).toBeVisible();
    expect(
      screen.getByText(
        "This link was already used or has expired. Ask your administrator for a new one.",
      ),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Back to sign-in" }),
    ).toBeVisible();
  });
});
