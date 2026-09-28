import { I18nProvider } from "@lingui/react";
import { QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/api/errors";
import { publicConfigQueryOptions } from "@/api/queries";
import { createQueryClient } from "@/app/query-client";
import { searchSerialization } from "@/app/search-params";
import { i18n } from "@/i18n";
import {
  PasswordTotpSetup,
  type PasswordTotpSubmission,
} from "@/pages/public/enroll/PasswordTotpSetup";
import { testConfig } from "@/test/app";

vi.mock("qrcode", () => ({
  default: { toCanvas: vi.fn().mockResolvedValue(undefined) },
}));

const queryClient = createQueryClient(() => undefined);

beforeEach(() => {
  i18n.activate("en");
  queryClient.setQueryData(publicConfigQueryOptions().queryKey, testConfig);
});

afterEach(() => {
  queryClient.clear();
});

function mount({
  submit = vi.fn(async () => ["AAAA-BBBB-CCCC-DDDD"]),
  onSubmitError,
}: {
  submit?: (submission: PasswordTotpSubmission) => Promise<string[]>;
  onSubmitError?: (error: unknown) => boolean;
} = {}) {
  const onBack = vi.fn();
  const onDone = vi.fn(async () => undefined);
  const root = createRootRoute({ component: Outlet });
  const page = createRoute({
    getParentRoute: () => root,
    path: "/",
    component: () => (
      <PasswordTotpSetup
        accountName="alice"
        onBack={onBack}
        submit={submit}
        onSubmitError={onSubmitError}
        onDone={onDone}
      />
    ),
  });
  const router = createRouter({
    ...searchSerialization,
    routeTree: root.addChildren([page]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { submit, onBack, onDone };
}

function codeInput() {
  return screen.getByRole("textbox", { name: "Authenticator code" });
}

function setupKey() {
  return (
    screen.getByRole("textbox", { name: "Setup key" }) as HTMLInputElement
  ).value;
}

async function toAuthenticator(user: UserEvent) {
  await screen.findByRole("heading", { name: "Set a password" });
  await user.type(screen.getByLabelText("Password"), "correct horse");
  await user.type(screen.getByLabelText("Confirm password"), "correct horse");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  await screen.findByRole("heading", { name: "Set up an authenticator" });
}

function refused(code: string, details?: Record<string, unknown>) {
  return new ApiError({
    kind: "http",
    status: 400,
    code,
    requestId: "r1",
    ...(details ? { details } : {}),
  });
}

describe("PasswordTotpSetup", () => {
  it("goes from the password to the authenticator to the codes, focusing each step's first field", async () => {
    const user = userEvent.setup();
    const { submit, onDone } = mount();
    expect(
      await screen.findByRole("heading", { name: "Set a password" }),
    ).toHaveFocus();
    await toAuthenticator(user);
    await waitFor(() => expect(codeInput()).toHaveFocus());
    expect(screen.getByLabelText("Password")).not.toBeVisible();
    const secret = setupKey();
    await user.type(codeInput(), "123456");
    await user.click(screen.getByRole("button", { name: "Verify and finish" }));
    expect(
      await screen.findByRole("heading", { name: "Save your recovery codes" }),
    ).toBeVisible();
    expect(submit).toHaveBeenCalledExactlyOnceWith({
      password: "correct horse",
      secretBase32: secret,
      code: "123456",
    });
    // The factors are set now: there is no way back.
    expect(
      screen.queryByRole("button", { name: "Back" }),
    ).not.toBeInTheDocument();
    const next = screen.getByRole("button", { name: "Continue" });
    expect(next).toBeDisabled();
    await user.click(
      screen.getByRole("checkbox", { name: "I have saved my recovery codes" }),
    );
    await user.click(next);
    await waitFor(() => expect(onDone).toHaveBeenCalledOnce());
  });

  it("goes back from the first step to the caller and from the second to the first, keeping the secret", async () => {
    const user = userEvent.setup();
    const { onBack } = mount();
    await toAuthenticator(user);
    const secret = setupKey();
    await user.click(screen.getByRole("button", { name: "Back" }));
    await screen.findByRole("heading", { name: "Set a password" });
    await waitFor(() =>
      expect(screen.getByLabelText("Password")).toHaveFocus(),
    );
    expect(screen.getByLabelText("Password")).toHaveValue("correct horse");
    expect(onBack).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByRole("heading", { name: "Set up an authenticator" });
    expect(setupKey()).toBe(secret);
    await user.click(screen.getByRole("button", { name: "Back" }));
    await screen.findByRole("heading", { name: "Set a password" });
    await user.click(screen.getByRole("button", { name: "Back" }));
    expect(onBack).toHaveBeenCalledOnce();
  });

  it("checks the password rule and the repeat before moving on", async () => {
    const user = userEvent.setup();
    mount();
    await screen.findByRole("heading", { name: "Set a password" });
    await user.type(screen.getByLabelText("Password"), "short");
    await user.type(screen.getByLabelText("Confirm password"), "other");
    await user.click(screen.getByRole("button", { name: "Continue" }));
    expect(
      await screen.findByText("Use a password of at least 8 characters."),
    ).toBeVisible();
    expect(screen.getByText("The two passwords do not match.")).toBeVisible();
    expect(
      screen.getByRole("heading", { name: "Set a password" }),
    ).toBeVisible();
  });

  it("marks a wrong code on its field and clears it, keeping the secret", async () => {
    const user = userEvent.setup();
    const submit = vi.fn(async () => {
      throw refused("bad_credentials");
    });
    mount({ submit });
    await toAuthenticator(user);
    const secret = setupKey();
    await user.type(codeInput(), "123456");
    await user.click(screen.getByRole("button", { name: "Verify and finish" }));
    await waitFor(() =>
      expect(codeInput()).toHaveAttribute("aria-invalid", "true"),
    );
    expect(codeInput()).toHaveValue("");
    expect(setupKey()).toBe(secret);
    expect(
      screen.getByRole("heading", { name: "Set up an authenticator" }),
    ).toBeVisible();
  });

  it("returns to the password when the server turns it down", async () => {
    const user = userEvent.setup();
    const submit = vi.fn(async () => {
      throw refused("validation_failed", { location: "password" });
    });
    mount({ submit });
    await toAuthenticator(user);
    await user.type(codeInput(), "123456");
    await user.click(screen.getByRole("button", { name: "Verify and finish" }));
    await screen.findByRole("heading", { name: "Set a password" });
    await waitFor(() =>
      expect(screen.getByLabelText("Password")).toHaveAttribute(
        "aria-invalid",
        "true",
      ),
    );
  });

  it("leaves a failure the caller claims to the caller", async () => {
    const user = userEvent.setup();
    const error = refused("bad_credentials");
    const submit = vi.fn(async () => {
      throw error;
    });
    const onSubmitError = vi.fn(() => true);
    mount({ submit, onSubmitError });
    await toAuthenticator(user);
    await user.type(codeInput(), "123456");
    await user.click(screen.getByRole("button", { name: "Verify and finish" }));
    await waitFor(() =>
      expect(onSubmitError).toHaveBeenCalledExactlyOnceWith(error),
    );
    expect(codeInput()).not.toHaveAttribute("aria-invalid", "true");
    expect(codeInput()).toHaveValue("123456");
  });
});
