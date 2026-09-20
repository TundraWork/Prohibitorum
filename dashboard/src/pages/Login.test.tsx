import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  type AnyRoute,
  createBrowserHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  type RouterHistory,
  RouterProvider,
} from "@tanstack/react-router";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import type { ReactElement } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, describeError } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import {
  authStatusQueryOptions,
  publicConfigQueryOptions,
  sessionQueryOptions,
} from "@/api/queries";
import type { PublicConfig } from "@/api/raw-paths";
import { createQueryClient } from "@/app/query-client";
import { RecoveryCodes } from "@/components/custom/RecoveryCodes";
import { i18n } from "@/i18n";
import { PasswordPage, RecoveryPage, TotpPage } from "@/pages/Login";

vi.mock("qrcode", () => ({
  default: { toCanvas: vi.fn().mockResolvedValue(undefined) },
}));

const config: PublicConfig = {
  instanceName: "Test instance",
  hasCustomIcon: false,
  iconUrl: "",
  iconEtag: "",
  maintenanceMode: false,
  maintenanceMessage: "",
  hasCustomBackground: false,
  backgroundUrl: "",
  backgroundEtag: "",
  totp: { issuer: "Test", algorithm: "SHA1", digits: 6, period: 30 },
};
let queryClient: QueryClient;
let history: RouterHistory;

beforeEach(() => {
  window.history.replaceState(null, "", "/login");
  history = createBrowserHistory();
  i18n.activate("en");
  queryClient = createQueryClient(() => {});
  queryClient.setQueryData(publicConfigQueryOptions().queryKey, config);
  queryClient.setQueryData(authStatusQueryOptions().queryKey, {
    bootstrapped: true,
  });
});
afterEach(() => {
  history.destroy();
  queryClient.clear();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function mountRouter(routeTree: AnyRoute) {
  const router = createRouter({ routeTree, history });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return router;
}

function mount() {
  const root = createRootRoute({ component: Outlet });
  const password = createRoute({
    getParentRoute: () => root,
    path: "/login",
    component: PasswordPage,
  });
  const totp = createRoute({
    getParentRoute: () => root,
    path: "/login/totp",
    component: TotpPage,
  });
  const recovery = createRoute({
    getParentRoute: () => root,
    path: "/login/recovery",
    component: RecoveryPage,
  });
  const elsewhere = createRoute({
    getParentRoute: () => root,
    path: "/",
    component: () => <h1>Destination</h1>,
  });
  return mountRouter(root.addChildren([password, totp, recovery, elsewhere]));
}

function mountStandalone(component: () => ReactElement) {
  const root = createRootRoute({ component: Outlet });
  const login = createRoute({
    getParentRoute: () => root,
    path: "/login",
    component,
  });
  return mountRouter(root.addChildren([login]));
}

async function password(user: UserEvent) {
  const username = await screen.findByRole("textbox", { name: "Username" });
  if (!(username as HTMLInputElement).value)
    await user.type(username, " alice ");
  await user.type(screen.getByLabelText("Password"), " password ");
  await user.click(
    screen.getByRole("button", { name: "Continue with password" }),
  );
  await screen.findByRole("textbox", { name: "Authenticator code" });
}

function rejectedCode() {
  return Response.json(
    { code: "bad_credentials", requestId: "login-test" },
    { status: 401 },
  );
}

it("replaces the shared login error when alternating password and passkey failures", async () => {
  vi.stubGlobal("isSecureContext", true);
  vi.stubGlobal("PublicKeyCredential", class {});
  const fetch = vi
    .fn<typeof globalThis.fetch>()
    .mockResolvedValueOnce(rejectedCode())
    .mockResolvedValueOnce(
      Response.json(
        { code: "server_error", requestId: "passkey-failure" },
        { status: 503 },
      ),
    )
    .mockResolvedValueOnce(
      Response.json(
        { code: "rate_limited", requestId: "password-retry" },
        { status: 429 },
      ),
    );
  vi.stubGlobal("fetch", fetch);
  const user = userEvent.setup();
  mount();
  await user.type(
    await screen.findByRole("textbox", { name: "Username" }),
    "alice",
  );
  await user.type(screen.getByLabelText("Password"), " password ");
  for (const [button, code] of [
    ["Continue with password", "bad_credentials"],
    ["Sign in with a passkey", "server_error"],
    ["Continue with password", "rate_limited"],
  ]) {
    await user.click(screen.getByRole("button", { name: button }));
    await waitFor(() => {
      const alerts = screen.getAllByRole("alert");
      expect(alerts).toHaveLength(1);
      expect(alerts[0]).toHaveTextContent(
        i18n._(describeError(new ApiError({ kind: "http", code }))),
      );
    });
    expect(screen.getByLabelText("Password")).toHaveValue(" password ");
  }
});

describe("single-use password verification", () => {
  it("keeps a token after local validation but requires another password after sending a failed code", async () => {
    const bodies: unknown[] = [];
    let passwordCount = 0;
    const fetch = vi
      .fn<typeof globalThis.fetch>()
      .mockImplementation(async (input) => {
        const request = input as Request;
        bodies.push(await request.json());
        if (request.url.endsWith("/password/begin")) {
          passwordCount++;
          return Response.json({
            partial_session_token: `partial-${passwordCount}`,
          });
        }
        return rejectedCode();
      });
    vi.stubGlobal("fetch", fetch);
    const user = userEvent.setup();
    mount();
    await password(user);
    const code = screen.getByRole("textbox", { name: "Authenticator code" });
    await user.type(code, "１２３４５６");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(code).toHaveAttribute("aria-invalid", "true");
    await user.clear(code);
    await user.type(code, "012345");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    const firstPassword = await screen.findByLabelText("Password");
    expect(firstPassword).toHaveValue("");
    expect(screen.getByRole("textbox", { name: "Username" })).toHaveValue(
      " alice ",
    );
    expect(
      screen.queryByRole("textbox", { name: "Authenticator code" }),
    ).not.toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: "Continue with password" }),
    );
    expect(fetch).toHaveBeenCalledTimes(2);
    await password(user);
    await user.type(
      screen.getByRole("textbox", { name: "Authenticator code" }),
      "012345",
    );
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    await screen.findByLabelText("Password");
    expect(bodies).toEqual([
      { username: " alice ", password: " password " },
      { partial_session_token: "partial-1", code: "012345" },
      { username: " alice ", password: " password " },
      { partial_session_token: "partial-2", code: "012345" },
    ]);
  });

  it("sends only one second-step request while repeated submissions are pending", async () => {
    let settle!: (response: Response) => void;
    const pending = new Promise<Response>((resolve) => {
      settle = resolve;
    });
    const fetch = vi
      .fn<typeof globalThis.fetch>()
      .mockResolvedValueOnce(
        Response.json({ partial_session_token: "one-use" }),
      )
      .mockReturnValueOnce(pending);
    vi.stubGlobal("fetch", fetch);
    const user = userEvent.setup();
    mount();
    await password(user);
    const code = screen.getByRole("textbox", { name: "Authenticator code" });
    await user.type(code, "012345");
    const form = screen.getByRole("form", {
      name: "Verify your authenticator",
    });
    fireEvent.submit(form);
    fireEvent.submit(form);
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
    expect(code).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Use a recovery code" }),
    ).toBeDisabled();
    await act(async () => {
      settle(rejectedCode());
    });
    await screen.findByLabelText("Password");
    expect(fetch).toHaveBeenCalledTimes(2);
  });
});

it("rejects noncanonical recovery codes and discards canceled authenticator reset fields", async () => {
  const fetch = vi
    .fn<typeof globalThis.fetch>()
    .mockResolvedValueOnce(
      Response.json({ partial_session_token: "recovery-once" }),
    )
    .mockResolvedValueOnce(rejectedCode());
  vi.stubGlobal("fetch", fetch);
  const user = userEvent.setup();
  mount();
  await password(user);
  await user.click(screen.getByRole("button", { name: "Use a recovery code" }));
  const code = screen.getByRole("textbox", { name: "Recovery code" });
  await user.type(code, "abcd-efgh-ijkl-mnop");
  await user.click(screen.getByRole("button", { name: "Sign in" }));
  expect(code).toHaveAttribute("aria-invalid", "true");
  expect(fetch).toHaveBeenCalledTimes(1);
  await user.clear(code);
  await user.type(code, "ABCD-EFGH-IJKL-MNOP");
  const reset = screen.getByRole("checkbox", {
    name: "Reset my authenticator",
  });
  await user.click(reset);
  const newCode = screen.getByRole("textbox", {
    name: "New authenticator code",
  });
  await user.type(newCode, "1");
  await user.click(screen.getByRole("button", { name: "Sign in" }));
  expect(newCode).toHaveAttribute("aria-invalid", "true");
  await user.click(reset);
  expect(
    screen.queryByRole("textbox", { name: "Setup key" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Sign in" }));
  await screen.findByLabelText("Password");
  const request = fetch.mock.calls[1]?.[0] as Request;
  expect(await request.json()).toEqual({
    partial_session_token: "recovery-once",
    code: "ABCD-EFGH-IJKL-MNOP",
    reset_authenticator: false,
  });
});

it("navigates client-side when the sign-in redirect targets a dashboard route", async () => {
  const fetch = vi
    .fn<typeof globalThis.fetch>()
    .mockResolvedValueOnce(
      Response.json({ partial_session_token: "finish-once" }),
    )
    .mockResolvedValueOnce(Response.json({ redirect: "/" }));
  vi.stubGlobal("fetch", fetch);
  const user = userEvent.setup();
  const router = mount();
  await password(user);
  await user.type(
    screen.getByRole("textbox", { name: "Authenticator code" }),
    "012345",
  );
  await user.click(screen.getByRole("button", { name: "Sign in" }));
  expect(
    await screen.findByRole("heading", { name: "Destination" }),
  ).toBeVisible();
  expect(router.state.location.pathname).toBe("/");
});

it("keeps new recovery codes visible across session changes and blocks leaving without confirmation", async () => {
  const codes = ["ABCD-EFGH-IJKL-MNOP", "QRST-UVWX-YZ23-4567"];
  const fetch = vi
    .fn<typeof globalThis.fetch>()
    .mockResolvedValueOnce(
      Response.json({ partial_session_token: "reset-once" }),
    )
    .mockResolvedValueOnce(
      Response.json({ redirect: "/", recovery_codes: codes }),
    );
  vi.stubGlobal("fetch", fetch);
  const user = userEvent.setup();
  const router = mount();
  await password(user);
  await user.click(screen.getByRole("button", { name: "Use a recovery code" }));
  await user.type(
    screen.getByRole("textbox", { name: "Recovery code" }),
    "ABCD-EFGH-IJKL-MNOP",
  );
  await user.click(
    screen.getByRole("checkbox", { name: "Reset my authenticator" }),
  );
  await user.type(
    screen.getByRole("textbox", { name: "New authenticator code" }),
    "012345",
  );
  await user.click(screen.getByRole("button", { name: "Sign in" }));
  const display = await screen.findByRole("textbox", {
    name: "New recovery codes",
  });
  expect(display).toHaveValue(codes.join("\n"));
  await act(async () => {
    const session: components["schemas"]["SessionView"] = {
      id: 1,
      username: "alice",
      displayName: "Alice",
      role: "user",
    };
    queryClient.setQueryData(sessionQueryOptions().queryKey, session);
  });
  expect(router.state.location.pathname).toBe("/login/recovery");
  expect(display).toBeVisible();
  expect(screen.getByRole("button", { name: "Continue" })).toBeDisabled();
  const beforeUnload = new Event("beforeunload", { cancelable: true });
  window.dispatchEvent(beforeUnload);
  expect(beforeUnload.defaultPrevented).toBe(true);
  const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
  await act(async () => {
    void router.navigate({ to: "/" });
  });
  expect(confirm).toHaveBeenCalledOnce();
  expect(display).toBeVisible();
  confirm.mockReturnValue(true);
  await act(async () => {
    await router.navigate({ to: "/" });
  });
  expect(
    await screen.findByRole("heading", { name: "Destination" }),
  ).toBeVisible();
  expect(
    screen.queryByRole("textbox", { name: "New recovery codes" }),
  ).not.toBeInTheDocument();
  const afterLeaving = new Event("beforeunload", { cancelable: true });
  window.dispatchEvent(afterLeaving);
  expect(afterLeaving.defaultPrevented).toBe(false);
});

it("requires saved confirmation and does not report a rejected clipboard write as success", async () => {
  const onContinue = vi.fn().mockResolvedValue(undefined);
  const user = userEvent.setup();
  vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(
    new DOMException("Denied", "NotAllowedError"),
  );
  mountStandalone(() => (
    <RecoveryCodes codes={["ABCD-EFGH-IJKL-MNOP"]} onContinue={onContinue} />
  ));
  await user.click(await screen.findByRole("button", { name: "Copy codes" }));
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(screen.queryByRole("status")).not.toBeInTheDocument();
  const next = screen.getByRole("button", { name: "Continue" });
  await user.click(next);
  expect(onContinue).not.toHaveBeenCalled();
  const beforeSaving = new Event("beforeunload", { cancelable: true });
  window.dispatchEvent(beforeSaving);
  expect(beforeSaving.defaultPrevented).toBe(true);
  await user.click(
    screen.getByRole("checkbox", { name: "I have saved my recovery codes" }),
  );
  const afterSaving = new Event("beforeunload", { cancelable: true });
  window.dispatchEvent(afterSaving);
  expect(afterSaving.defaultPrevented).toBe(false);
  await user.click(next);
  expect(onContinue).toHaveBeenCalledOnce();
});
