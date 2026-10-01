import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { render } from "@testing-library/react";
import { vi } from "vitest";
import type { components } from "@/api/generated/schema";
import type { PublicConfig } from "@/api/raw-paths";
import { createAppRouter } from "@/app/router";
import { defaultLoginAppearance } from "@/components/custom/login-appearance/appearance";
import { i18n } from "@/i18n";

export const testConfig: PublicConfig = {
  instanceName: "Test instance",
  hasCustomIcon: false,
  iconUrl: "",
  iconEtag: "",
  maintenanceMode: false,
  maintenanceMessage: "",
  loginAppearance: defaultLoginAppearance,
  loginImages: [],
  totp: { issuer: "Test", algorithm: "SHA1", digits: 6, period: 30 },
};

export const testSession: components["schemas"]["SessionView"] = {
  id: 1,
  username: "alice",
  oidcSubject: "00000000-0000-4000-8000-000000000001",
  displayName: "Alice",
  role: "user",
};

export function apiError(code: string, status: number): Response {
  return Response.json({ code, requestId: `test-${code}` }, { status });
}

type Handler = (request: Request) => Response | Promise<Response>;

/**
 * Stands in for the API: `"METHOD /path"` answers from the table, anything
 * else is a 404. Every request is kept, with its body already read, so a test
 * can say what was sent.
 */
export function fakeApi(routes: Record<string, Handler>) {
  const requests: { method: string; url: URL; body: unknown }[] = [];
  const fetch = vi.fn(async (request: Request) => {
    const url = new URL(request.url);
    const text = await request.clone().text();
    requests.push({
      method: request.method,
      url,
      body: text === "" ? undefined : JSON.parse(text),
    });
    const handler = routes[`${request.method} ${url.pathname}`];
    return handler ? handler(request) : new Response(null, { status: 404 });
  });
  vi.stubGlobal("fetch", fetch);
  return {
    routes,
    /** The requests made to one path, in order. */
    sent: (method: string, path: string) =>
      requests.filter(
        (request) => request.method === method && request.url.pathname === path,
      ),
  };
}

/** The API a public page reads before anything paints. */
export function publicApi(
  session: typeof testSession | null,
  config: PublicConfig = testConfig,
): Record<string, Handler> {
  return {
    "GET /api/prohibitorum/config": () => Response.json(config),
    "GET /api/prohibitorum/auth/status": () =>
      Response.json({ bootstrapped: true }),
    "GET /api/prohibitorum/me": () =>
      session ? Response.json(session) : apiError("no_session", 401),
    "GET /api/prohibitorum/auth/federation": () => Response.json([]),
  };
}

/** Mounts the whole app, route tree and all, at `path`. */
export function renderApp(path: string, queryClient: QueryClient) {
  const router = createAppRouter(
    { queryClient },
    createMemoryHistory({ initialEntries: [path] }),
  );
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return router;
}
