import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { client } from "@/api/client";
import { installApiMocks } from "@/devtools/mock/install";
import { resetMockConfig, updateMockConfig } from "@/devtools/mock/model";

type Application = Parameters<typeof installApiMocks>[0];

/** The console surface the middleware drives when the config changes. */
function application(): Application {
  return {
    queryClient: {
      invalidateQueries: vi.fn(),
      fetchQuery: vi.fn(async () => null),
    },
    router: {
      invalidate: vi.fn(),
      navigate: vi.fn(),
      state: { location: { pathname: "/login" } },
    },
  } as unknown as Application;
}

beforeEach(() => {
  window.localStorage.clear();
  resetMockConfig();
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  resetMockConfig();
});

describe("mock middleware latency", () => {
  it("holds an answer back for the configured delay", async () => {
    updateMockConfig((draft) => {
      draft.enabled = true;
      draft.delayMs = 700;
    });
    installApiMocks(application());

    const pending = client.GET("/api/prohibitorum/me");
    let settled = false;
    void pending.then(() => {
      settled = true;
    });

    await vi.advanceTimersByTimeAsync(699);
    expect(settled).toBe(false);

    await vi.advanceTimersByTimeAsync(1);
    const result = await pending;
    expect(settled).toBe(true);
    expect(result.response.status).toBe(200);
  });
});
