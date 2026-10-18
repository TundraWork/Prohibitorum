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
import { render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import { sessionQueryOptions } from "@/api/queries";
import { createQueryClient } from "@/app/query-client";
import { searchSerialization } from "@/app/search-params";
import { defaultLoginAppearance } from "@/components/custom/login-appearance/appearance";
import { i18n } from "@/i18n";
import { Console } from "@/pages/Console";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;

type Factors = components["schemas"]["MeFactorsView"];

const publicConfig = {
  instanceName: "Acme ID",
  hasCustomIcon: false,
  iconUrl: "",
  iconEtag: "",
  maintenanceMode: false,
  maintenanceMessage: "",
  loginAppearance: defaultLoginAppearance,
  loginImages: [],
  totp: { issuer: "Acme ID", algorithm: "SHA1", digits: 6, period: 30 },
};

const allSet: Factors = {
  passkeyCount: 2,
  passwordSet: true,
  totpEnrolled: true,
  recoveryCodesRemaining: 8,
};

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifyError.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(notifyError);
  queryClient.setQueryData<components["schemas"]["SessionView"]>(
    sessionQueryOptions().queryKey,
    {
      id: 1,
      username: "alice",
      oidcSubject: "00000000-0000-4000-8000-000000000001",
      displayName: "Alice Liddell",
      role: "user",
    },
  );
  queryClient.setQueryData(["public", "config"], publicConfig);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

function serve(factors: Factors | Response, deviceCount = 2) {
  fetchBoundary.mockImplementation(async (request) => {
    const path = new URL(request.url).pathname;
    if (path.endsWith("/me/factors")) {
      return factors instanceof Response ? factors : Response.json(factors);
    }
    if (path.endsWith("/me/sessions")) {
      return Response.json(
        Array.from({ length: deviceCount }, (_, index) => ({
          id: `session-${index}`,
          isCurrent: index === 0,
          issuedAt: new Date().toISOString(),
          expiresAt: new Date(Date.now() + 86_400_000).toISOString(),
          lastSeenIp: "192.0.2.1",
          userAgent: "",
        })),
      );
    }
    return new Response(null, { status: 404 });
  });
}

function mount() {
  const root = createRootRoute({ component: Outlet });
  const route = (path: string, component: () => React.ReactElement | null) =>
    createRoute({ getParentRoute: () => root, path, component });
  const router = createRouter({
    ...searchSerialization,
    routeTree: root.addChildren([
      route("/", Console),
      route("/security", () => <h1>Security page</h1>),
      route("/devices", () => <h1>Devices page</h1>),
    ]),
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

describe("console home", () => {
  it("greets the account by its display name under the instance's name", async () => {
    serve(allSet);
    mount();

    expect(
      await screen.findByRole("heading", {
        level: 1,
        name: "Welcome, Alice Liddell",
      }),
    ).toBeInTheDocument();
    expect(screen.getByText("Acme ID account")).toBeInTheDocument();
  });

  it("summarises the sign-in factors and the devices from the account's reads", async () => {
    serve(allSet, 3);
    mount();

    // Each section is replaced when its read lands, so wait for a value first.
    await screen.findByRole("link", { name: /2 passkeys/ });
    await screen.findByRole("link", { name: /3 signed-in devices/ });
    const section = (title: string) =>
      within(
        screen
          .getByRole("heading", { name: title })
          .closest("section") as HTMLElement,
      );
    expect(
      section("Sign-in").getByRole("link", {
        name: /Password and authenticator.*On/,
      }),
    ).toBeInTheDocument();
    expect(
      section("Sign-in").getByRole("link", {
        name: /Recovery codes.*8 codes left/,
      }),
    ).toBeInTheDocument();
    expect(
      section("Devices").getByRole("link", { name: /3 signed-in devices/ }),
    ).toBeInTheDocument();
  });

  it("says plainly what is not set up yet and hides the recovery codes without an authenticator", async () => {
    serve({
      passkeyCount: 0,
      passwordSet: false,
      totpEnrolled: false,
      recoveryCodesRemaining: 0,
    });
    mount();

    expect(
      await screen.findByRole("link", { name: /No passkeys yet/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /Not set up/ }),
    ).toBeInTheDocument();
    expect(screen.queryByText("Recovery codes")).not.toBeInTheDocument();
  });

  it("counts a single passkey and a single device in the singular", async () => {
    serve({ ...allSet, passkeyCount: 1 }, 1);
    mount();

    expect(
      await screen.findByRole("link", { name: /1 passkey/ }),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole("link", { name: /1 signed-in device/ }),
    ).toBeInTheDocument();
  });

  it("flags recovery codes running low at three, and not at four", async () => {
    serve({ ...allSet, recoveryCodesRemaining: 3 });
    mount();
    const low = await screen.findByRole("link", { name: /Recovery codes/ });
    expect(within(low).getByText("Running low")).toBeInTheDocument();
    expect(within(low).getByText("3 codes left")).toBeInTheDocument();
  });

  it("leaves four recovery codes unflagged", async () => {
    serve({ ...allSet, recoveryCodesRemaining: 4 });
    mount();
    const row = await screen.findByRole("link", { name: /Recovery codes/ });
    expect(within(row).getByText("4 codes left")).toBeInTheDocument();
    expect(within(row).queryByText("Running low")).not.toBeInTheDocument();
  });

  it("flags recovery codes that are all used", async () => {
    serve({ ...allSet, recoveryCodesRemaining: 0 });
    mount();
    const row = await screen.findByRole("link", { name: /Recovery codes/ });
    expect(within(row).getByText("All used")).toBeInTheDocument();
  });

  it("leaves plenty of recovery codes unflagged", async () => {
    serve(allSet);
    mount();
    const row = await screen.findByRole("link", { name: /Recovery codes/ });
    expect(within(row).getByText("8 codes left")).toBeInTheDocument();
    expect(within(row).queryByText("Running low")).not.toBeInTheDocument();
    expect(within(row).queryByText("All used")).not.toBeInTheDocument();
  });

  it("says a password without an authenticator is not set up, and offers no recovery codes", async () => {
    serve({ ...allSet, totpEnrolled: false });
    mount();
    const row = await screen.findByRole("link", {
      name: /Password and authenticator/,
    });
    expect(within(row).getByText("Not set up")).toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: /Recovery codes/ }),
    ).not.toBeInTheDocument();
  });

  it("links the sign-in rows to Security and the devices row to Devices", async () => {
    serve(allSet);
    mount();

    for (const name of [
      /Passkeys.*2 passkeys/,
      /Password and authenticator.*On/,
      /Recovery codes.*8 codes left/,
    ]) {
      expect(await screen.findByRole("link", { name })).toHaveAttribute(
        "href",
        "/security",
      );
    }
    expect(
      await screen.findByRole("link", { name: /2 signed-in devices/ }),
    ).toHaveAttribute("href", "/devices");
  });

  it("keeps the rest of the page when one read fails, and says so in its section", async () => {
    serve(Response.json({ code: "internal" }, { status: 500 }));
    mount();

    expect(
      await screen.findByRole("link", { name: /2 signed-in devices/ }),
    ).toBeInTheDocument();
    expect(
      await screen.findByText("This section could not be loaded"),
    ).toBeInTheDocument();
    expect(notifyError).not.toHaveBeenCalled();
    expect(
      screen.getByRole("heading", { name: "Welcome, Alice Liddell" }),
    ).toBeInTheDocument();
  });
});
