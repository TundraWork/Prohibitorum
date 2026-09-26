import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { client } from "@/api/client";
import { sessionQueryOptions } from "@/api/queries";
import { createQueryClient } from "@/app/query-client";
import { installApiMocks } from "@/devtools/mock/install";
import { getMockConfig, resetMockConfig } from "@/devtools/mock/model";

type Application = Parameters<typeof installApiMocks>[0];

/** The console surface the middleware drives when the config changes. */
function application(queryClient: ReturnType<typeof createQueryClient>) {
  return {
    queryClient,
    router: {
      invalidate: vi.fn(),
      navigate: vi.fn(),
      subscribe: vi.fn(() => () => {}),
      state: { location: { pathname: "/login" } },
    },
  } as unknown as Application;
}

beforeEach(() => {
  window.localStorage.clear();
  resetMockConfig();
  // The address bar a walkthrough arrives on, set before install because that
  // is when it is read: `installApiMocks` subscribes to `popstate` and to the
  // router's `onResolved`, and both only ever see a location that comes after
  // the one the app was opened on.
  window.history.replaceState(
    null,
    "",
    "/admin/saml-applications/13?mock&mock.admin.samlApps=4",
  );
});

afterEach(() => {
  resetMockConfig();
});

/**
 * One case rather than several: `installApiMocks` guards on a module-level
 * flag, so the middleware can only be registered once per file, and the facts
 * below all describe that one install.
 */
describe("first-load mock activation", () => {
  it("is in place before the first read, so the first load is not sent to the server", async () => {
    const queryClient = createQueryClient(vi.fn());
    installApiMocks(application(queryClient));

    // Read at install time, so the config already agrees with the address bar
    // by the time install returns. Leaving this to the navigation
    // subscriptions is what left a first-load `?mock` unanswered.
    const config = getMockConfig();
    expect(config.enabled).toBe(true);
    expect(config.admin.samlApps).toBe(4);

    // The read `_protected` performs on a cold load: nothing cached, so the
    // freshness it enforces sends it to the client. Before the fix the
    // middleware was registered behind an import nobody awaited, so this left
    // for the server, 401'd, and the loader turned that into a redirect to
    // `/login`. Answered from the mock, it is a signed-in account.
    const session = await queryClient.fetchQuery(sessionQueryOptions());
    expect(session).toMatchObject({ username: "mock", role: "admin" });

    // The middleware owns every read of the first load, not the session one
    // alone; a path with no fixture still fails as `mock_unmocked` rather than
    // being answered by the server, because the mock owns whole verbs rather
    // than a list of paths.
    const factors = await client.GET("/api/prohibitorum/me/factors");
    expect(factors.response.status).toBe(200);
    await expect(client.GET("/api/prohibitorum/me/apps")).rejects.toMatchObject(
      {
        code: "mock_unmocked",
      },
    );
  });
});
