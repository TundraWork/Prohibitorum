import { I18nProvider } from "@lingui/react";
import {
  browserSupportsWebAuthn,
  browserSupportsWebAuthnAutofill,
  startAuthentication,
  WebAuthnAbortService,
  WebAuthnError,
} from "@simplewebauthn/browser";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createBrowserHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  type RouterHistory,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { ApiError, describeError } from "@/api/errors";
import {
  authStatusQueryOptions,
  publicConfigQueryOptions,
  publicFederationProvidersQueryOptions,
} from "@/api/queries";
import type { PublicConfig } from "@/api/raw-paths";
import { createQueryClient } from "@/app/query-client";
import { searchSerialization } from "@/app/search-params";
import { i18n } from "@/i18n";
import { PasswordPage } from "@/pages/Login";

vi.mock("@simplewebauthn/browser", () => ({
  browserSupportsWebAuthn: vi.fn(),
  browserSupportsWebAuthnAutofill: vi.fn(),
  startAuthentication: vi.fn(),
  WebAuthnAbortService: { cancelCeremony: vi.fn() },
  WebAuthnError: class extends Error {
    code = "";
  },
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
const picked = {
  id: "credential-id",
  rawId: "credential-id",
  type: "public-key",
  response: {
    authenticatorData: "a",
    clientDataJSON: "c",
    signature: "s",
    userHandle: "u",
  },
  clientExtensionResults: {},
};

let queryClient: QueryClient;
let history: RouterHistory;
const notices = vi.fn<(error: unknown) => void>();
const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
/** The answer to /auth/login/complete. */
let completeAnswer: () => Response;

beforeEach(() => {
  vi.resetAllMocks();
  window.history.replaceState(null, "", "/login");
  history = createBrowserHistory();
  i18n.activate("en");
  queryClient = createQueryClient(notices);
  queryClient.setQueryData(publicConfigQueryOptions().queryKey, config);
  queryClient.setQueryData(authStatusQueryOptions().queryKey, {
    bootstrapped: true,
  });
  queryClient.setQueryData(
    publicFederationProvidersQueryOptions().queryKey,
    [],
  );
  vi.stubGlobal("isSecureContext", true);
  vi.mocked(browserSupportsWebAuthn).mockReturnValue(true);
  vi.mocked(browserSupportsWebAuthnAutofill).mockResolvedValue(true);
  completeAnswer = () => Response.json({ redirect: "/" });
  fetchBoundary.mockImplementation(async (request) => {
    const url = new URL(request.url);
    if (url.pathname === "/api/prohibitorum/auth/login/begin") {
      return Response.json({
        challenge: url.search ? "autofill" : "button",
        rpId: "id.example",
        timeout: url.search ? 300_000 : 60_000,
      });
    }
    if (url.pathname === "/api/prohibitorum/auth/login/complete") {
      return completeAnswer();
    }
    return Response.json(
      { code: "unexpected", requestId: "r" },
      { status: 500 },
    );
  });
  vi.stubGlobal("fetch", fetchBoundary);
});

afterEach(() => {
  history.destroy();
  queryClient.clear();
  vi.unstubAllGlobals();
});

function mount() {
  const root = createRootRoute({ component: Outlet });
  const login = createRoute({
    getParentRoute: () => root,
    path: "/login",
    component: PasswordPage,
  });
  const elsewhere = createRoute({
    getParentRoute: () => root,
    path: "/",
    component: () => <h1>Destination</h1>,
  });
  const router = createRouter({
    ...searchSerialization,
    routeTree: root.addChildren([login, elsewhere]),
    history,
  });
  return render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

/** The login requests sent so far, as path plus search. */
function loginCalls(): string[] {
  return fetchBoundary.mock.calls
    .map(([request]) => new URL(request.url))
    .filter((url) => url.pathname.includes("/auth/login/"))
    .map(
      (url) =>
        `${url.pathname.replace("/api/prohibitorum/auth", "")}${url.search}`,
    );
}

function aborted() {
  const error = new WebAuthnError({} as never);
  Object.assign(error, { code: "ERROR_CEREMONY_ABORTED" });
  return error;
}

/**
 * The browser: an autofill request stays open until the ceremony is
 * cancelled, unless `pick` settles it; a button ceremony answers `modal`.
 */
function browser({
  pick,
  modal,
}: {
  pick?: Promise<unknown>;
  modal?: () => Promise<unknown>;
} = {}) {
  let cancelOpen: (() => void) | undefined;
  vi.mocked(WebAuthnAbortService.cancelCeremony).mockImplementation(() => {
    cancelOpen?.();
    cancelOpen = undefined;
  });
  vi.mocked(startAuthentication).mockImplementation(
    async ({ useBrowserAutofill }) => {
      cancelOpen?.();
      if (!useBrowserAutofill)
        return (await (modal?.() ?? new Promise(() => {}))) as never;
      return new Promise((resolve, reject) => {
        cancelOpen = () => reject(aborted());
        pick?.then((value) => resolve(value as never), reject);
      });
    },
  );
}

function failureText(code: string) {
  return i18n._(describeError(new ApiError({ kind: "http", code })));
}

it("marks the username field for passkey autofill", async () => {
  browser();
  mount();
  expect(
    await screen.findByRole("textbox", { name: "Username" }),
  ).toHaveAttribute("autocomplete", "username webauthn");
});

it("starts a conditional ceremony on arrival where the browser supports autofill", async () => {
  browser();
  mount();
  await waitFor(() =>
    expect(loginCalls()).toEqual(["/login/begin?mediation=conditional"]),
  );
  expect(vi.mocked(startAuthentication)).toHaveBeenCalledWith(
    expect.objectContaining({ useBrowserAutofill: true }),
  );
});

it("offers no autofill where the browser does not support it", async () => {
  vi.mocked(browserSupportsWebAuthnAutofill).mockResolvedValue(false);
  browser();
  mount();
  await screen.findByRole("textbox", { name: "Username" });
  await waitFor(() =>
    expect(browserSupportsWebAuthnAutofill).toHaveBeenCalled(),
  );
  expect(loginCalls()).toEqual([]);
  expect(startAuthentication).not.toHaveBeenCalled();
});

it("signs in with the passkey picked from autofill", async () => {
  browser({ pick: Promise.resolve(picked) });
  mount();
  expect(
    await screen.findByRole("heading", { name: "Destination" }),
  ).toBeVisible();
  expect(loginCalls()).toEqual([
    "/login/begin?mediation=conditional",
    "/login/complete?mediation=conditional",
  ]);
  const complete = fetchBoundary.mock.calls[1]?.[0];
  expect(await complete?.json()).toEqual(picked);
});

it("reports a failed completion like the passkey button, then offers autofill again", async () => {
  completeAnswer = () =>
    Response.json(
      { code: "login_verification_failed", requestId: "r" },
      { status: 401 },
    );
  vi.mocked(startAuthentication)
    .mockResolvedValueOnce(picked as never)
    .mockImplementation(() => new Promise(() => {}));
  mount();
  const alert = await screen.findByRole("alert");
  expect(alert).toHaveTextContent(failureText("login_verification_failed"));
  await waitFor(() =>
    expect(loginCalls()).toEqual([
      "/login/begin?mediation=conditional",
      "/login/complete?mediation=conditional",
      "/login/begin?mediation=conditional",
    ]),
  );
});

it("says nothing and does not retry when the autofill ceremony cannot begin", async () => {
  fetchBoundary.mockImplementation(async () =>
    Response.json({ code: "mock_unmocked", requestId: "r" }, { status: 501 }),
  );
  browser();
  mount();
  await waitFor(() =>
    expect(loginCalls()).toEqual(["/login/begin?mediation=conditional"]),
  );
  await new Promise((resolve) => setTimeout(resolve, 50));
  expect(loginCalls()).toHaveLength(1);
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(notices).not.toHaveBeenCalled();
  expect(startAuthentication).not.toHaveBeenCalled();
});

it("pauses autofill for the passkey button and offers it again afterwards", async () => {
  let refuse!: (error: unknown) => void;
  browser({
    modal: () =>
      new Promise((_, reject) => {
        refuse = reject;
      }),
  });
  const user = userEvent.setup();
  mount();
  await waitFor(() => expect(startAuthentication).toHaveBeenCalledTimes(1));

  await user.click(
    screen.getByRole("button", { name: "Sign in with a passkey" }),
  );
  await waitFor(() => expect(startAuthentication).toHaveBeenCalledTimes(2));
  expect(WebAuthnAbortService.cancelCeremony).toHaveBeenCalled();
  expect(loginCalls()).toEqual([
    "/login/begin?mediation=conditional",
    "/login/begin",
  ]);
  expect(vi.mocked(startAuthentication).mock.calls[1]?.[0]).toMatchObject({
    optionsJSON: { challenge: "button" },
  });
  expect(
    vi.mocked(startAuthentication).mock.calls[1]?.[0].useBrowserAutofill,
  ).toBeUndefined();

  refuse(new DOMException("dismissed", "NotAllowedError"));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    i18n._(
      describeError(
        new ApiError({ kind: "local", code: "passkey_incomplete" }),
      ),
    ),
  );
  await waitFor(() =>
    expect(loginCalls()).toEqual([
      "/login/begin?mediation=conditional",
      "/login/begin",
      "/login/begin?mediation=conditional",
    ]),
  );
  await waitFor(() => expect(startAuthentication).toHaveBeenCalledTimes(3));
  expect(
    vi.mocked(startAuthentication).mock.calls[2]?.[0].useBrowserAutofill,
  ).toBe(true);
});

it("cancels the waiting ceremony when the page goes away", async () => {
  browser();
  const view = mount();
  await waitFor(() => expect(startAuthentication).toHaveBeenCalledTimes(1));
  expect(WebAuthnAbortService.cancelCeremony).not.toHaveBeenCalled();
  view.unmount();
  expect(WebAuthnAbortService.cancelCeremony).toHaveBeenCalled();
  await new Promise((resolve) => setTimeout(resolve, 20));
  expect(loginCalls()).toEqual(["/login/begin?mediation=conditional"]);
});
