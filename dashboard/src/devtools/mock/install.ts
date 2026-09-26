import type { QueryClient } from "@tanstack/react-query";
import type { RegisteredRouter } from "@tanstack/react-router";
import { client } from "@/api/client";
import { ApiError } from "@/api/errors";
import { sessionQueryOptions } from "@/api/queries";
import { buildMockReply } from "@/devtools/mock/fixtures";
import {
  getMockConfig,
  subscribeMockConfig,
  updateMockConfig,
} from "@/devtools/mock/model";
import { applyMockQuery } from "@/devtools/mock/query";

type Application = { queryClient: QueryClient; router: RegisteredRouter };

const publicPrefixes = ["/login", "/preview", "/__dev"];

function isPublicPath(pathname: string): boolean {
  return publicPrefixes.some(
    (prefix) => pathname === prefix || pathname.startsWith(`${prefix}/`),
  );
}

let installed = false;

/**
 * Answers the app's requests from the mock config, and refreshes everything the
 * console already loaded whenever that config changes.
 *
 * While the master switch is on the mock owns every read, and the writes
 * switch extends that to the other methods; a request with no fixture fails
 * rather than reaching the server, so nothing is answered from two sources at
 * once. Requests of a verb the mock does not own still go to the server.
 *
 * The query client is handed in rather than imported so the invalidation runs
 * against the very instance the pages render against, and the router follows so
 * a sign-in change moves between the console and the sign-in page.
 */
export function installApiMocks(application: Application): void {
  if (installed) return;
  installed = true;

  // A URL that asks for mocked data turns the mock on before anything reads it,
  // and keeps doing so as the address changes — a walkthrough can then set up a
  // screen by editing the address bar rather than by opening the panel. The
  // subscription below picks the change up and refreshes what is already drawn.
  const applyLocation = () => {
    applyMockQuery(window.location.search);
  };
  applyLocation();
  window.addEventListener("popstate", applyLocation);
  application.router.subscribe("onResolved", applyLocation);

  client.use({
    async onRequest({ schemaPath, request }) {
      const config = getMockConfig();
      if (!config.enabled) return undefined;
      const reply = buildMockReply(
        {
          method: request.method,
          schemaPath,
          url: request.url,
          body: request.method === "GET" ? undefined : await jsonBody(request),
        },
        config,
      );
      if (!reply) return undefined;
      // Latency is part of what the panel fakes: an instant reply hides the
      // pending states a page shows while it waits.
      await wait(config.delayMs);
      // Applied before the reply leaves, so the reads that follow the write
      // already agree with it.
      if (reply.effect) updateMockConfig(reply.effect);
      // Thrown rather than returned: a response returned from `onRequest`
      // skips the client's own onResponse middleware, so the ApiError shape the
      // callers switch on has to come from here.
      if (reply.kind === "error") {
        throw new ApiError({
          kind: "http",
          status: reply.status,
          code: reply.code,
          ...(reply.details ? { details: reply.details } : {}),
        });
      }
      if (reply.kind === "empty") {
        return new Response(null, { status: reply.status });
      }
      return new Response(JSON.stringify(reply.body), {
        status: reply.status,
        headers: { "content-type": "application/json" },
      });
    },
  });

  let lastEnabled = getMockConfig().enabled;
  subscribeMockConfig(() => {
    const config = getMockConfig();
    const wasEnabled = lastEnabled;
    lastEnabled = config.enabled;
    // While mocking is off, editing the panel changes nothing the app can see,
    // so it must not send the real API a burst of refetches.
    if (!config.enabled && !wasEnabled) return;
    void refresh(application);
  });
}

/**
 * The JSON body of a write, when it has one. An uploaded image and an empty
 * body are not JSON.
 */
async function jsonBody(request: Request): Promise<unknown> {
  try {
    return await request.clone().json();
  } catch {
    return undefined;
  }
}

/** Holds a mocked reply back for the configured latency. */
function wait(ms: number): Promise<void> {
  if (ms <= 0) return Promise.resolve();
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function refresh(application: Application): Promise<void> {
  const { queryClient, router } = application;
  try {
    await queryClient.invalidateQueries();
    await router.invalidate();
  } catch {
    // A redirect thrown by a loader lands here; the router already followed it.
  }
  let session: unknown;
  try {
    session = await queryClient.fetchQuery(sessionQueryOptions());
  } catch {
    return;
  }
  const { pathname } = router.state.location;
  try {
    if (session === null) {
      if (pathname !== "/login") await router.navigate({ to: "/login" });
    } else if (isPublicPath(pathname)) {
      await router.navigate({ to: "/" });
    }
  } catch {
    // Same: the loader's redirect is the intended outcome.
  }
}
