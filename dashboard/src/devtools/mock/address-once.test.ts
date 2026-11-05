import { afterEach, describe, expect, it, vi } from "vitest";
import { installApiMocks } from "@/devtools/mock/install";
import {
  getMockConfig,
  resetMockConfig,
  updateMockConfig,
} from "@/devtools/mock/model";

type Application = Parameters<typeof installApiMocks>[0];

afterEach(() => {
  resetMockConfig();
});

/**
 * One case: `installApiMocks` wires itself once per module, and this file
 * needs to be the one that wires it to hold the router's callback.
 */
describe("the address's mock parameters", () => {
  it("are applied once, so a mocked write is not undone by the next navigation", () => {
    window.localStorage.clear();
    resetMockConfig();
    window.history.replaceState(
      null,
      "",
      "/sign-out?mock.session.others=ma&mock.writes=1",
    );
    let onResolved: (() => void) | undefined;
    const app = {
      queryClient: {
        invalidateQueries: vi.fn(),
        fetchQuery: vi.fn(async () => null),
      },
      router: {
        invalidate: vi.fn(),
        navigate: vi.fn(),
        subscribe: vi.fn((_event: string, callback: () => void) => {
          onResolved = callback;
          return () => {};
        }),
        state: { location: { pathname: "/sign-out" } },
      },
    } as unknown as Application;
    installApiMocks(app);
    expect(getMockConfig().session.others).toBe("ma");

    // A sign-out, as the writes make it, while the address still says "ma".
    updateMockConfig((draft) => {
      draft.session.others = "m";
    });
    onResolved?.();
    expect(getMockConfig().session.others).toBe("m");

    // An address with other parameters is still applied.
    window.history.replaceState(null, "", "/sign-out?mock.session.others=mam");
    onResolved?.();
    expect(getMockConfig().session.others).toBe("mam");
  });
});
