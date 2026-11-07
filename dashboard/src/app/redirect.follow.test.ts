import type { RegisteredRouter } from "@tanstack/react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { loadDocument } from "@/app/load-document";
import { followRedirect } from "@/app/redirect";

vi.mock("@/app/load-document", () => ({ loadDocument: vi.fn() }));

/** A router with the dashboard pages a hop can lead to, and nothing else. */
function pagesRouter() {
  const pages = ["/consent", "/login", "/error", "/choose-account"];
  const navigate = vi.fn(async (_: { href: string }) => undefined);
  const router = {
    getMatchedRoutes: (pathname: string) => [
      [],
      {},
      pages.includes(pathname) ? {} : undefined,
    ],
    navigate,
  };
  return { router: router as unknown as RegisteredRouter, navigate };
}

/** The hops' answers by path; anything else is a 404. */
function hops(answers: Record<string, () => Response>) {
  const fetch = vi.fn(async (request: Request) => {
    const answer = answers[new URL(request.url).pathname];
    return answer ? answer() : new Response(null, { status: 404 });
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.mocked(loadDocument).mockReset();
});

describe("followRedirect", () => {
  it("opens a dashboard page in place", async () => {
    const fetch = hops({});
    const { router, navigate } = pagesRouter();
    await followRedirect(router, "/consent?ticket=t-1");
    expect(navigate).toHaveBeenCalledWith({ href: "/consent?ticket=t-1" });
    expect(fetch).not.toHaveBeenCalled();
    expect(loadDocument).not.toHaveBeenCalled();
  });

  it("asks a protocol hop where it leads, through every hop, and opens the page it names", async () => {
    const fetch = hops({
      "/api/prohibitorum/account-chooser/continue": () =>
        Response.json({ location: "/oauth/authorize?client_id=wiki" }),
      "/oauth/authorize": () =>
        Response.json({
          location: `${window.location.origin}/consent?ticket=t-1&u=b0b0`,
        }),
    });
    const { router, navigate } = pagesRouter();
    await followRedirect(
      router,
      "/api/prohibitorum/account-chooser/continue?ticket=c-1",
    );
    expect(navigate).toHaveBeenCalledWith({
      href: "/consent?ticket=t-1&u=b0b0",
    });
    expect(loadDocument).not.toHaveBeenCalled();
    const asked = fetch.mock.calls.map(([request]) => request);
    expect(asked.map((request) => new URL(request.url).pathname)).toEqual([
      "/api/prohibitorum/account-chooser/continue",
      "/oauth/authorize",
    ]);
    for (const request of asked) {
      expect(request.headers.get("X-Prohibitorum-Navigation")).toBe("follow");
      expect(request.redirect).toBe("manual");
    }
  });

  it("loads the address the server holds for an application as a document", async () => {
    hops({
      "/oauth/authorize": () => Response.json({ location: "/oauth/handoff" }),
    });
    const { router, navigate } = pagesRouter();
    await followRedirect(router, "/oauth/authorize?client_id=wiki");
    expect(loadDocument).toHaveBeenCalledWith("/oauth/handoff");
    expect(navigate).not.toHaveBeenCalled();
  });

  it.each([
    [
      "a redirect the server did not name",
      () => Response.redirect("https://app.example/cb", 302),
    ],
    ["a failure", () => new Response("no", { status: 500 })],
    ["an answer that is not a location", () => Response.json({})],
  ])("loads the hop itself as a document after %s", async (_, answer) => {
    hops({ "/oauth/authorize": answer });
    const { router, navigate } = pagesRouter();
    await followRedirect(router, "/oauth/authorize?client_id=wiki");
    expect(loadDocument).toHaveBeenCalledWith(
      "/oauth/authorize?client_id=wiki",
    );
    expect(navigate).not.toHaveBeenCalled();
  });

  it("loads anything else as a document without asking", async () => {
    const fetch = hops({});
    const { router } = pagesRouter();
    await followRedirect(router, "/federation/github/login");
    await followRedirect(router, "https://elsewhere.example/x");
    expect(fetch).not.toHaveBeenCalled();
    expect(vi.mocked(loadDocument).mock.calls).toEqual([
      ["/federation/github/login"],
      ["https://elsewhere.example/x"],
    ]);
  });

  it("stops following a hop that keeps naming hops", async () => {
    const fetch = hops({
      "/oauth/authorize": () =>
        Response.json({ location: "/oauth/authorize?again=1" }),
    });
    const { router } = pagesRouter();
    await followRedirect(router, "/oauth/authorize");
    expect(fetch.mock.calls.length).toBeLessThanOrEqual(4);
    expect(loadDocument).toHaveBeenCalledWith("/oauth/authorize?again=1");
  });
});
