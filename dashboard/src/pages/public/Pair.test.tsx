import { I18nProvider } from "@lingui/react";
import {
  browserSupportsWebAuthn,
  startRegistration,
} from "@simplewebauthn/browser";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import QRCode from "qrcode";
import { StrictMode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { pairingStatusQueryOptions } from "@/api/queries";
import type { PairingStart, PairingStatus } from "@/api/raw-paths";
import { loadDocument } from "@/app/load-document";
import { createQueryClient } from "@/app/query-client";
import { createAppRouter } from "@/app/router";
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

const beginPath = "/api/prohibitorum/auth/devices/pair/begin";
const statusPath = "/api/prohibitorum/auth/devices/pair/status";
const completePath = "/api/prohibitorum/auth/devices/pair/complete";
const target = "/oauth/authorize?client_id=wiki";

let started = 0;

function pairing(overrides: Partial<PairingStart> = {}): PairingStart {
  started += 1;
  const code = started === 1 ? "K7QM4XTB" : "HN3PWR8C";
  return {
    pairingId: `pairing-${started}`,
    code,
    displayCode: `${code.slice(0, 4)}-${code.slice(4)}`,
    expiresAt: new Date(Date.now() + 4 * 60_000 + 32_000).toISOString(),
    ...overrides,
  };
}

let queryClient: QueryClient;
const notifyError = vi.fn();

beforeEach(() => {
  i18n.activate("en");
  started = 0;
  notifyError.mockReset();
  queryClient = createQueryClient(notifyError);
  vi.mocked(browserSupportsWebAuthn).mockReturnValue(false);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
  vi.mocked(loadDocument).mockReset();
  vi.mocked(QRCode.toCanvas).mockClear();
});

function pairApi({
  start = () => Response.json(pairing()),
  status = () => ({ status: "pending" }),
  session = null,
}: {
  start?: () => Response;
  status?: () => PairingStatus;
  session?: typeof testSession | null;
} = {}) {
  return fakeApi({
    ...publicApi(session),
    [`POST ${beginPath}`]: start,
    [`GET ${statusPath}`]: () => Response.json(status()),
    [`POST ${completePath}`]: () =>
      Response.json({ session: testSession, redirect: target }),
  });
}

/** Lands the next status read the way the poll would. */
function poll(id = "pairing-1") {
  return queryClient.refetchQueries({
    queryKey: pairingStatusQueryOptions(id).queryKey,
  });
}

async function waiting() {
  return screen.findByText(/Waiting for approval…/);
}

describe("signing in with another device", () => {
  it("starts one pairing and shows its code, its link and the time left", async () => {
    const api = pairApi();
    const router = createAppRouter(
      { queryClient },
      createMemoryHistory({ initialEntries: ["/pair"] }),
    );
    render(
      <StrictMode>
        <I18nProvider i18n={i18n}>
          <QueryClientProvider client={queryClient}>
            <RouterProvider router={router} />
          </QueryClientProvider>
        </I18nProvider>
      </StrictMode>,
    );

    expect(
      await screen.findByRole("heading", {
        name: "Sign in with another device",
      }),
    ).toHaveFocus();
    expect(
      screen.getByText(
        "On a device that's already signed in to Test instance, open Devices and enter the code below.",
      ),
    ).toBeVisible();
    expect(await screen.findByText("K7QM-4XTB")).toHaveAttribute(
      "aria-hidden",
      "true",
    );
    // Read out one character at a time rather than as a word.
    expect(screen.getByText("K 7 Q M 4 X T B")).toHaveClass("sr-only");
    expect(await waiting()).toHaveTextContent(/The code expires in 4:3[12]\./);
    await waitFor(() => expect(QRCode.toCanvas).toHaveBeenCalled());
    expect(vi.mocked(QRCode.toCanvas).mock.calls.at(-1)?.[1]).toBe(
      `${window.location.origin}/devices?code=K7QM4XTB`,
    );
    expect(
      screen.getByRole("img", {
        name: "QR code that opens this pairing on the other device",
      }),
    ).toBeInTheDocument();
    // The countdown is not announced; nothing has happened yet.
    expect(screen.getByRole("status")).toHaveTextContent("");
    expect(api.sent("POST", beginPath)).toHaveLength(1);
  });

  it("asks again every 2.5 seconds while the pairing waits, and not after", () => {
    const interval = pairingStatusQueryOptions("p").refetchInterval as (
      query: unknown,
    ) => number | false;
    const at = (data?: PairingStatus) => interval({ state: { data } });
    expect(at()).toBe(2500);
    expect(at({ status: "pending" })).toBe(2500);
    expect(at({ status: "approved" })).toBe(false);
    expect(at({ status: "expired" })).toBe(false);
  });

  it("keeps waiting through a failed read without reporting it", async () => {
    let fail = true;
    const api = pairApi();
    api.routes[`GET ${statusPath}`] = () =>
      fail
        ? apiError("internal_error", 500)
        : Response.json({ status: "pending" });
    renderApp("/pair", queryClient);
    await waiting();
    await poll();
    fail = false;
    await poll();
    expect(await waiting()).toBeVisible();
    expect(api.sent("GET", statusPath).length).toBeGreaterThanOrEqual(2);
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("shows an expired code and gets a new one on request", async () => {
    let status: PairingStatus = { status: "pending" };
    const api = pairApi({ status: () => status });
    const user = userEvent.setup();
    renderApp("/pair", queryClient);
    await waiting();

    status = { status: "expired" };
    await poll();
    expect(
      await screen.findByText("This pairing code has expired.", {
        selector: ".alert__title, .alert__title *",
      }),
    ).toBeVisible();
    expect(screen.getByRole("status")).toHaveTextContent(
      "This pairing code has expired.",
    );
    expect(screen.queryByText(/Waiting for approval/)).toBeNull();

    status = { status: "pending" };
    await user.click(screen.getByRole("button", { name: "Get a new code" }));
    expect(await screen.findByText("HN3P-WR8C")).toBeVisible();
    await waiting();
    expect(api.sent("POST", beginPath)).toHaveLength(2);
    expect(
      screen.getByRole("heading", { name: "Sign in with another device" }),
    ).toHaveFocus();
  });

  it("marks the code expired when its time runs out, and stops asking", async () => {
    const api = pairApi({
      start: () =>
        Response.json(
          pairing({ expiresAt: new Date(Date.now() + 3000).toISOString() }),
        ),
    });
    renderApp("/pair", queryClient);
    await screen.findByText("K7QM-4XTB");
    expect(
      await screen.findByRole(
        "button",
        { name: "Get a new code" },
        { timeout: 6000 },
      ),
    ).toBeVisible();
    const reads = api.sent("GET", statusPath).length;
    await poll();
    expect(api.sent("GET", statusPath)).toHaveLength(reads);
  });

  it("signs in as soon as it is approved, keeping where the sign-in was headed", async () => {
    let status: PairingStatus = { status: "pending" };
    const api = pairApi({ status: () => status });
    renderApp(`/pair?return_to=${encodeURIComponent(target)}`, queryClient);
    await waiting();

    status = { status: "approved" };
    await poll();
    expect(
      await screen.findByRole("heading", { name: "This device is signed in" }),
    ).toHaveFocus();
    const [complete] = api.sent("POST", completePath);
    expect(complete?.body).toEqual({ pairingId: "pairing-1" });
    expect(complete?.url.searchParams.get("return_to")).toBe(target);
    expect(api.sent("POST", completePath)).toHaveLength(1);
  });

  it("offers to try again when signing in fails, and does not retry by itself", async () => {
    let status: PairingStatus = { status: "pending" };
    let fail = true;
    const api = pairApi({ status: () => status });
    api.routes[`POST ${completePath}`] = () =>
      fail
        ? apiError("internal_error", 500)
        : Response.json({ session: testSession, redirect: target });
    const user = userEvent.setup();
    renderApp("/pair", queryClient);
    await waiting();

    status = { status: "approved" };
    await poll();
    const retry = await screen.findByRole("button", { name: "Try again" });
    expect(notifyError).toHaveBeenCalledTimes(1);
    // The way back stays while the page waits on the reader.
    expect(
      screen.getByRole("link", { name: "Back to sign in" }),
    ).toBeInTheDocument();
    expect(api.sent("POST", completePath)).toHaveLength(1);

    fail = false;
    await user.click(retry);
    expect(
      await screen.findByRole("heading", { name: "This device is signed in" }),
    ).toBeVisible();
    expect(api.sent("POST", completePath)).toHaveLength(2);
  });

  it("shows the code as expired when the approved pairing is gone by the time it completes", async () => {
    let status: PairingStatus = { status: "pending" };
    const api = pairApi({ status: () => status });
    api.routes[`POST ${completePath}`] = () => apiError("pairing_expired", 410);
    renderApp("/pair", queryClient);
    await waiting();

    status = { status: "approved" };
    await poll();
    expect(
      await screen.findByRole("button", { name: "Get a new code" }),
    ).toBeVisible();
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("goes on without a passkey", async () => {
    let status: PairingStatus = { status: "pending" };
    pairApi({ status: () => status });
    const user = userEvent.setup();
    renderApp("/pair", queryClient);
    await waiting();
    status = { status: "approved" };
    await poll();
    await user.click(await screen.findByRole("button", { name: "Not now" }));
    await waitFor(() => expect(loadDocument).toHaveBeenCalledWith(target));
  });

  it("adds a passkey, then goes on", async () => {
    vi.stubGlobal("isSecureContext", true);
    vi.mocked(browserSupportsWebAuthn).mockReturnValue(true);
    vi.mocked(startRegistration).mockResolvedValue({
      id: "cred",
      rawId: "cred",
      type: "public-key",
      response: { clientDataJSON: "", attestationObject: "" },
      clientExtensionResults: {},
    });
    let status: PairingStatus = { status: "pending" };
    const api = pairApi({ status: () => status });
    api.routes["POST /api/prohibitorum/me/credentials/register/begin"] = () =>
      Response.json({ challenge: "c" });
    api.routes["POST /api/prohibitorum/me/credentials/register/complete"] =
      () => Response.json({ id: 1 });
    const user = userEvent.setup();
    renderApp("/pair", queryClient);
    await waiting();
    status = { status: "approved" };
    await poll();
    await user.click(
      await screen.findByRole("button", { name: "Add a passkey" }),
    );
    await waitFor(() => expect(loadDocument).toHaveBeenCalledWith(target));
    expect(
      api.sent("POST", "/api/prohibitorum/me/credentials/register/complete"),
    ).toHaveLength(1);
  });

  it("leaves only a way on once adding a passkey has timed out", async () => {
    vi.stubGlobal("isSecureContext", true);
    vi.mocked(browserSupportsWebAuthn).mockReturnValue(true);
    let status: PairingStatus = { status: "pending" };
    const api = pairApi({ status: () => status });
    api.routes["POST /api/prohibitorum/me/credentials/register/begin"] = () =>
      apiError("sudo_required", 403);
    const user = userEvent.setup();
    renderApp("/pair", queryClient);
    await waiting();
    status = { status: "approved" };
    await poll();
    await user.click(
      await screen.findByRole("button", { name: "Add a passkey" }),
    );
    const next = await screen.findByRole("button", { name: "Continue" });
    expect(screen.queryByRole("button", { name: "Not now" })).toBeNull();
    await user.click(next);
    await waitFor(() => expect(loadDocument).toHaveBeenCalledWith(target));
  });

  it("offers a new code when the first one could not be started", async () => {
    let fail = true;
    const api = pairApi({
      start: () =>
        fail ? apiError("rate_limited", 429) : Response.json(pairing()),
    });
    const user = userEvent.setup();
    renderApp("/pair", queryClient);
    const again = await screen.findByRole("button", {
      name: "Get a new code",
    });
    expect(notifyError).toHaveBeenCalledTimes(1);
    fail = false;
    await user.click(again);
    expect(await screen.findByText("K7QM-4XTB")).toBeVisible();
    expect(api.sent("POST", beginPath)).toHaveLength(2);
  });

  it("leads back to sign-in with the same return address", async () => {
    pairApi();
    renderApp(`/pair?return_to=${encodeURIComponent(target)}`, queryClient);
    await waiting();
    expect(
      screen.getByRole("link", { name: "Back to sign in" }),
    ).toHaveAttribute("href", `/login?return_to=${encodeURIComponent(target)}`);
  });

  it("sends a signed-in reader home instead of starting a pairing", async () => {
    const api = pairApi({ session: testSession });
    const router = renderApp("/pair", queryClient);
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(api.sent("POST", beginPath)).toHaveLength(0);
  });

  it("refuses a link that names its return address twice", async () => {
    const api = pairApi();
    renderApp("/pair?return_to=%2Fa&return_to=%2Fb", queryClient);
    expect(
      await screen.findByText(
        "This sign-in link is invalid. Open a new sign-in link.",
      ),
    ).toBeVisible();
    expect(api.sent("POST", beginPath)).toHaveLength(0);
  });
});
