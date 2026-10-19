import { I18nProvider } from "@lingui/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { LatestConnectionTestView } from "@/api/raw-admin-paths";
import { createQueryClient } from "@/app/query-client";
import { diagnosticResult } from "@/devtools/mock/fixtures";
import { i18n } from "@/i18n";
import {
  ProviderDiagnosticsSection,
  runIdPattern,
} from "@/pages/admin/identity-providers/ProviderDiagnosticsSection";
import { apiError, type FakeApi, fakeApi } from "@/test/app";

const slug = "corp";
const base = `/api/prohibitorum/identity-providers/${slug}`;
const runId = "Rk9mAbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_x";

const notifyError = vi.fn<(error: unknown, scope?: string) => void>();
let api: FakeApi;
let queryClient: ReturnType<typeof createQueryClient>;

beforeEach(() => {
  i18n.activate("en");
  notifyError.mockReset();
});

afterEach(() => {
  queryClient?.clear();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

const outcome = (name: "succeeded" | "fallback" | "failed" | "running") =>
  diagnosticResult(name);

/** The summary of a test that finished two minutes ago. */
function latest(overrides: Partial<LatestConnectionTestView> = {}): {
  latest: LatestConnectionTestView;
} {
  return {
    latest: {
      status: "succeeded",
      finishedAt: new Date(Date.now() - 2 * 60_000).toISOString(),
      stale: false,
      fields: {},
      ...overrides,
    },
  };
}

const effectiveConfig = {
  mode: "discovery",
  fetchedAt: new Date(Date.now() - 2 * 60_000).toISOString(),
  callbackUrl: "https://id.example.test/api/prohibitorum/test/callback",
  fields: {
    issuer: { value: "https://idp.example.test", source: "discovery" },
    authorizationEndpoint: {
      value: "https://idp.example.test/authorize",
      source: "discovery",
    },
    tokenEndpoint: {
      value: "https://idp.example.test/token",
      source: "override",
    },
    userinfoEndpoint: { value: "", source: "discovery" },
    jwksEndpoint: { value: "none", source: "manual" },
    tokenAuthMethod: { value: "client_secret_basic", source: "discovery" },
    pkceMethod: { value: "S256", source: "manual" },
    responseType: { value: "code", source: "manual" },
    scopes: { value: [], source: "manual" },
  },
};

/** Keeps `?test=` in state the way the route does. */
function Harness({ initial }: { initial: string | undefined }) {
  const [current, setCurrent] = useState(initial);
  return (
    <ProviderDiagnosticsSection
      slug={slug}
      runId={current}
      onRunIdChange={setCurrent}
    />
  );
}

function mount(
  routes: Record<string, (request: Request) => Response | Promise<Response>>,
  initial?: string,
): UserEvent {
  api = fakeApi({
    [`GET ${base}/tests/latest`]: () => Response.json({ latest: null }),
    ...routes,
  });
  queryClient = createQueryClient(notifyError);
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <Harness initial={initial} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return userEvent.setup();
}

const resultPath = `${base}/tests/${runId}`;
const completePath = `${resultPath}/complete`;

describe("the run id", () => {
  it("is 43 URL-safe characters and nothing else", () => {
    expect(runIdPattern.test(runId)).toBe(true);
    expect(runIdPattern.test("short")).toBe(false);
  });
});

describe("the latest connection test", () => {
  it("shows a passed test beside the title", async () => {
    mount({ [`GET ${base}/tests/latest`]: () => Response.json(latest()) });
    expect(await screen.findByText(/^Passed/)).toHaveTextContent(
      "Passed 2 minutes ago",
    );
    expect(screen.queryByText("Configuration changed since")).toBeNull();
  });

  it("shows a failed test", async () => {
    mount({
      [`GET ${base}/tests/latest`]: () =>
        Response.json(latest({ status: "failed" })),
    });
    expect(await screen.findByText(/^Failed/)).toHaveTextContent(
      "Failed 2 minutes ago",
    );
  });

  it("says the configuration changed since a stale test", async () => {
    mount({
      [`GET ${base}/tests/latest`]: () =>
        Response.json(latest({ stale: true })),
    });
    expect(await screen.findByText(/^Passed/)).toHaveTextContent(
      "Passed 2 minutes ago",
    );
    expect(screen.getByText("Configuration changed since")).toBeVisible();
  });

  it("shows nothing before any test finished", async () => {
    mount({});
    await screen.findByRole("button", { name: "Start a test" });
    await waitFor(() =>
      expect(api.sent("GET", `${base}/tests/latest`)).toHaveLength(1),
    );
    expect(screen.queryByText(/^Passed/)).toBeNull();
    expect(screen.queryByText(/^Failed/)).toBeNull();
  });
});

describe("the effective configuration", () => {
  const route = {
    [`GET ${base}/effective-config`]: () => Response.json(effectiveConfig),
  };

  it("reads nothing until asked", async () => {
    mount(route);
    await screen.findByRole("button", { name: "Read configuration" });
    expect(api.sent("GET", `${base}/effective-config`)).toHaveLength(0);
  });

  it("lists each value with where it came from, and says special values in words", async () => {
    const user = mount(route);
    await user.click(
      await screen.findByRole("button", { name: "Read configuration" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Effective configuration",
    });

    expect(within(dialog).getByText(/^Read$/)).toHaveTextContent(
      "Read 2 minutes ago",
    );
    const row = (label: string) =>
      within(dialog).getByText(label).closest("div") as HTMLElement;
    expect(row("Issuer")).toHaveTextContent("https://idp.example.test");
    expect(row("Issuer")).toHaveTextContent("Discovered");
    expect(row("Token")).toHaveTextContent("Overridden");
    expect(row("PKCE")).toHaveTextContent("Manual");
    expect(row("UserInfo")).toHaveTextContent("Not used");
    expect(row("JWKS")).toHaveTextContent("Not used — the ID token is skipped");
    expect(row("Scopes")).toHaveTextContent("None sent");
    expect(
      within(dialog).getByText(
        "Register this address with the provider before running a connection test.",
      ),
    ).toBeVisible();
    expect(
      within(dialog).getByRole("button", {
        name: "Copy the test callback address",
      }),
    ).toBeVisible();
    expect(within(dialog).getByText(effectiveConfig.callbackUrl)).toBeVisible();
  });

  it("reads again from the footer and closes", async () => {
    const user = mount(route);
    await user.click(
      await screen.findByRole("button", { name: "Read configuration" }),
    );
    const dialog = await screen.findByRole("dialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Read again" }),
    );
    await waitFor(() =>
      expect(api.sent("GET", `${base}/effective-config`)).toHaveLength(2),
    );
    await user.click(within(dialog).getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("reports a refused read once and opens nothing", async () => {
    const user = mount({
      [`GET ${base}/effective-config`]: () => apiError("rate_limited", 429),
    });
    await user.click(
      await screen.findByRole("button", { name: "Read configuration" }),
    );
    await waitFor(() => expect(notifyError).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

describe("starting a test", () => {
  function stubAssign() {
    const assign = vi.fn();
    vi.spyOn(window, "location", "get").mockReturnValue({
      ...window.location,
      assign,
    });
    return assign;
  }

  it("leaves for the provider's authorization address", async () => {
    const assign = stubAssign();
    const user = mount({
      [`POST ${base}/tests`]: () =>
        Response.json({
          id: runId,
          authorizationUrl: "https://idp.example.test/authorize?x=1",
          expiresAt: new Date(Date.now() + 600_000).toISOString(),
        }),
    });
    await user.click(
      await screen.findByRole("button", { name: "Start a test" }),
    );
    await waitFor(() =>
      expect(assign).toHaveBeenCalledWith(
        "https://idp.example.test/authorize?x=1",
      ),
    );
  });

  it("keeps its toast when the start fails, and does not leave", async () => {
    const assign = stubAssign();
    const user = mount({
      [`POST ${base}/tests`]: () => apiError("federation_state_invalid", 400),
    });
    await user.click(
      await screen.findByRole("button", { name: "Start a test" }),
    );
    await waitFor(() => expect(notifyError).toHaveBeenCalledTimes(1));
    expect(notifyError.mock.calls[0]?.[1]).toBe("diagnostic");
    expect(assign).not.toHaveBeenCalled();
  });

  it("reports a rate-limited start once", async () => {
    stubAssign();
    const user = mount({
      [`POST ${base}/tests`]: () => apiError("rate_limited", 429),
    });
    await user.click(
      await screen.findByRole("button", { name: "Start a test" }),
    );
    await waitFor(() => expect(notifyError).toHaveBeenCalledTimes(1));
  });
});

describe("the result dialog", () => {
  const resultRoutes = (
    name: "succeeded" | "fallback" | "failed" | "running",
  ) => ({
    [`GET ${resultPath}`]: () => Response.json(outcome(name)),
    [`POST ${completePath}`]: () => Response.json(outcome(name)),
  });

  it("shows a passed run: stages in order, then mapped fields with their sources", async () => {
    mount(resultRoutes("succeeded"), runId);
    const dialog = await screen.findByRole("dialog", {
      name: "Connection test",
    });

    expect(await within(dialog).findByText("Test passed")).toBeVisible();
    const stages = within(
      within(dialog).getByRole("list", { name: "Test stages" }),
    ).getAllByRole("listitem");
    expect(stages.map((item) => item.textContent)).toEqual([
      expect.stringContaining("Discovery"),
      expect.stringContaining("Authorization"),
      expect.stringContaining("Callback"),
      expect.stringContaining("Token exchange"),
      expect.stringContaining("ID token"),
      expect.stringContaining("UserInfo"),
    ]);
    expect(stages[0]).toHaveTextContent("42 ms");
    expect(stages[0]).toHaveTextContent("Passed");
    expect(stages[3]).toHaveTextContent("HTTP 200");

    const panel = within(dialog).getByRole("tabpanel");
    expect(panel).toHaveTextContent("mock-user@example.test");
    expect(panel).toHaveTextContent("ID token · preferred_username");
    expect(panel).toHaveTextContent("UserInfo · picture");
    expect(panel).toHaveTextContent("Yes");
  });

  it("shows a subject past integer precision as the string received, and skips the ID token", async () => {
    const user = mount(resultRoutes("fallback"), runId);
    const dialog = await screen.findByRole("dialog");
    await within(dialog).findByText("Test passed");

    expect(within(dialog).getByRole("tabpanel")).toHaveTextContent(
      "9007199254740993",
    );
    expect(within(dialog).getByText("Skipped")).toBeVisible();

    await user.click(within(dialog).getByRole("tab", { name: "ID token" }));
    expect(
      await within(dialog).findByText("The provider returned no ID token."),
    ).toBeVisible();
    await user.click(within(dialog).getByRole("tab", { name: "UserInfo" }));
    expect(
      await within(dialog).findByText(/"id": 9007199254740993/),
    ).toBeVisible();
  });

  it("shows the documents of a passed run", async () => {
    const user = mount(resultRoutes("succeeded"), runId);
    const dialog = await screen.findByRole("dialog");
    await within(dialog).findByText("Test passed");
    await user.click(within(dialog).getByRole("tab", { name: "ID token" }));
    expect(
      await within(dialog).findByText(/"aud": "mock-client-1"/),
    ).toBeVisible();
  });

  it("shows a failed run with the provider's message wrapped and no tabs", async () => {
    mount(resultRoutes("failed"), runId);
    const dialog = await screen.findByRole("dialog");

    expect(await within(dialog).findByText("Test failed")).toBeVisible();
    expect(within(dialog).getByText("invalid_request")).toBeVisible();
    expect(within(dialog).getByText(/AADSTS9002346/)).toBeVisible();
    expect(within(dialog).queryByRole("tab")).toBeNull();
  });

  it("acknowledges a finished run once", async () => {
    mount(
      {
        [`GET ${resultPath}`]: () =>
          Response.json({ ...outcome("succeeded"), status: "ready" }),
        [`POST ${completePath}`]: () => Response.json(outcome("succeeded")),
      },
      runId,
    );
    await screen.findByRole("dialog");
    await waitFor(() => expect(api.sent("POST", completePath)).toHaveLength(1));
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(api.sent("POST", completePath)).toHaveLength(1);
  });

  it("closes through its footer and clears the run", async () => {
    const user = mount(resultRoutes("succeeded"), runId);
    const dialog = await screen.findByRole("dialog");
    await within(dialog).findByText("Test passed");
    await user.click(within(dialog).getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("offers a new test from the footer", async () => {
    const assign = vi.fn();
    vi.spyOn(window, "location", "get").mockReturnValue({
      ...window.location,
      assign,
    });
    const user = mount(
      {
        ...resultRoutes("succeeded"),
        [`POST ${base}/tests`]: () =>
          Response.json({
            id: runId,
            authorizationUrl: "https://idp.example.test/authorize",
            expiresAt: new Date(Date.now() + 600_000).toISOString(),
          }),
      },
      runId,
    );
    const dialog = await screen.findByRole("dialog");
    await user.click(
      await within(dialog).findByRole("button", { name: "Test again" }),
    );
    await waitFor(() => expect(assign).toHaveBeenCalledTimes(1));
  });

  it("warns after thirty seconds of waiting and checks again on request", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const user = mount(resultRoutes("running"), runId);
    const dialog = await screen.findByRole("dialog");
    await within(dialog).findByText("Checking");
    expect(
      within(dialog).queryByText(/taking longer than expected/),
    ).toBeNull();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(31_000);
    });
    expect(
      await within(dialog).findByText(/taking longer than expected/),
    ).toBeVisible();

    const before = api.sent("GET", resultPath).length;
    await user.click(
      within(dialog).getByRole("button", { name: "Check again" }),
    );
    await waitFor(() =>
      expect(api.sent("GET", resultPath).length).toBe(before + 1),
    );
  });
});
