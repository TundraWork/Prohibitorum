import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import { factorsQueryOptions, sessionQueryOptions } from "@/api/queries";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { PasswordTotpPanel } from "@/pages/security/PasswordTotpPanel";

vi.mock("qrcode", () => ({
  default: { toCanvas: vi.fn().mockResolvedValue(undefined) },
}));

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
let queryClient: QueryClient;

function json(body: unknown) {
  return Response.json(body);
}

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(() => undefined);
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
  queryClient.clear();
  vi.unstubAllGlobals();
});

function mount(factors: {
  passwordSet: boolean;
  totpEnrolled: boolean;
  passkeyCount: number;
  recoveryCodesRemaining: number;
}) {
  fetchBoundary.mockImplementation(async (request) => {
    const path = new URL(request.url).pathname;
    if (path.endsWith("/me/factors")) return json(factors);
    if (path.endsWith("/me/sudo/methods")) {
      return json({ methods: ["password_totp"], fresh: true });
    }
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <PasswordTotpPanel />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

const bothSet = {
  passwordSet: true,
  totpEnrolled: true,
  passkeyCount: 1,
  recoveryCodesRemaining: 4,
};

describe("password and authenticator factors", () => {
  it("shows the separate cards when both factors exist, and never the combined endpoint", async () => {
    mount(bothSet);

    expect(
      await screen.findByRole("heading", { name: "Change password" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Replace authenticator" }),
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

    // Reading the state is not a write.
    const writes = fetchBoundary.mock.calls.filter(
      ([request]) => request.method !== "GET",
    );
    expect(writes).toEqual([]);
  });

  it("shows one combined card when a factor is missing, and submits the atomic endpoint", async () => {
    mount({
      passwordSet: false,
      totpEnrolled: false,
      passkeyCount: 0,
      recoveryCodesRemaining: 0,
    });

    // One card, not two: the backend only establishes both together.
    expect(
      await screen.findByRole("heading", {
        name: "Set up a password and authenticator",
      }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: "Change password" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: "Replace authenticator" }),
    ).not.toBeInTheDocument();

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

  it("keeps a swallowed failure out of the request and reports a mismatch locally", async () => {
    mount(bothSet);
    const user: UserEvent = userEvent.setup();

    await user.type(
      await screen.findByLabelText("New password"),
      "a-long-enough-password",
    );
    await user.type(screen.getByLabelText("Repeat new password"), "different");
    await user.click(screen.getByRole("button", { name: "Change password" }));

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
    await screen.findByRole("heading", { name: "Change password" });
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
