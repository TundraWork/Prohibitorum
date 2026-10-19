import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { afterEach, beforeEach, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";

/** Everything the API is asked, answered by `serve` or, failing that, an empty 204. */
type Serve = (request: Request) => Response | Promise<Response> | undefined;

/**
 * The scaffolding an account's access sections share: a stubbed network, a
 * query client whose error toast is a spy, and a mount that renders one
 * section inside the providers it reads from. Call it once in a `describe`.
 */
export function accessHarness() {
  const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
  const notifyError = vi.fn<(error: unknown) => void>();
  let queryClient: QueryClient;

  beforeEach(() => {
    i18n.activate("en");
    fetchBoundary.mockReset();
    notifyError.mockReset();
    vi.stubGlobal("fetch", fetchBoundary);
    queryClient = createQueryClient(
      notifyError,
      vi.fn<(message: MessageDescriptor) => void>(),
    );
  });

  afterEach(() => {
    queryClient.clear();
    vi.unstubAllGlobals();
  });

  return {
    notifyError,
    mount(ui: ReactElement, serve: Serve) {
      fetchBoundary.mockImplementation(
        async (request) =>
          (await serve(request)) ?? new Response(null, { status: 204 }),
      );
      render(
        <I18nProvider i18n={i18n}>
          <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>
        </I18nProvider>,
      );
    },
    /** The requests of one method whose path ends with `suffix`. */
    sent(method: string, suffix: string) {
      return fetchBoundary.mock.calls
        .map(([request]) => request)
        .filter(
          (request) =>
            request.method === method &&
            new URL(request.url).pathname.endsWith(suffix),
        );
    },
  };
}

export const refused = () =>
  Response.json({ code: "server_error", requestId: "req-1" }, { status: 500 });
