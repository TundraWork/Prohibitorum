import type { QueryClient } from "@tanstack/react-query";
import type { RegisteredRouter } from "@tanstack/react-router";
import { client } from "@/api/client";
import { httpFailure } from "@/api/exchange";
import { sessionQueryOptions } from "@/api/queries";
import { buildMockReply } from "@/devtools/mock/fixtures";
import {
  getMockConfig,
  subscribeMockConfig,
  updateMockConfig,
} from "@/devtools/mock/model";
import { applyMockQuery } from "@/devtools/mock/query";

type Application = { queryClient: QueryClient; router: RegisteredRouter };

/** The sign-in steps, which a signed-in account has no use for. */
const signInPaths = ["/login", "/login/totp", "/login/recovery"];

/**
 * The public pages that are not a sign-in step. Each decides for itself what a
 * session means — a consent page sends an anonymous reader to sign in, the
 * maintenance page offers a way out either way, an enrollment or a VRChat
 * verification needs none — so a change of session in the panel leaves the
 * reader on them. The pages with a token in the path are matched by prefix.
 */
const publicPaths = [
  "/consent",
  "/saml-consent",
  "/error",
  "/maintenance",
  "/welcome",
  "/setup-signin",
];
const publicPrefixes = ["/enroll/", "/federation/flow/", "/verify/vrchat/"];

export function isPublicPath(pathname: string): boolean {
  return (
    publicPaths.includes(pathname) ||
    publicPrefixes.some((prefix) => pathname.startsWith(prefix))
  );
}

/**
 * Where a change of session in the panel moves the reader, if anywhere: a
 * signed-in account off the sign-in steps and home, an anonymous one out of
 * the console and to the sign-in page.
 */
export function refreshTarget(
  pathname: string,
  signedIn: boolean,
): "/" | "/login" | undefined {
  if (signedIn) return signInPaths.includes(pathname) ? "/" : undefined;
  if (signInPaths.includes(pathname) || isPublicPath(pathname)) {
    return undefined;
  }
  return "/login";
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
    async onRequest({ schemaPath, request, id }) {
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
      // skips the client's own onResponse middleware, so the error is built
      // here, from the same response a server would send, the way the client
      // builds it. The request ID is part of that envelope; without one the
      // client would not trust the code.
      if (reply.kind === "error") {
        throw await httpFailure(
          request,
          Response.json(
            {
              code: reply.code,
              requestId: `mock-${id}`,
              ...(reply.details ? { details: reply.details } : {}),
            },
            { status: reply.status },
          ),
        );
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
  const target = refreshTarget(
    router.state.location.pathname,
    session !== null,
  );
  try {
    if (target !== undefined) await router.navigate({ to: target });
  } catch {
    // Same: the loader's redirect is the intended outcome.
  }
}
