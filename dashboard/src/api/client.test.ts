import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { client, requireJsonData } from "@/api/client";
import { ApiError, describeError, isCancellation } from "@/api/errors";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();

beforeEach(() => {
  fetchBoundary.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
});

afterEach(() => vi.unstubAllGlobals());

describe("typed API transport", () => {
  it("accepts an empty logout but rejects missing data for a JSON query", async () => {
    fetchBoundary.mockImplementation(
      async () => new Response(null, { status: 204 }),
    );

    const result = await client.POST("/api/prohibitorum/auth/logout");
    expect(result.data).toBeUndefined();
    expect(result.response.status).toBe(204);
    await expect(
      requireJsonData(client.GET("/api/prohibitorum/auth/status")),
    ).rejects.toMatchObject({ kind: "invalid-response", status: 204 });
  });

  it("rejects malformed successful JSON without exposing its contents", async () => {
    fetchBoundary.mockResolvedValue(
      new Response("<html>private proxy error</html>"),
    );
    const error = await requireJsonData(
      client.GET("/api/prohibitorum/auth/status"),
    ).catch((failure: unknown) => failure);

    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ kind: "invalid-response" });
    expect(JSON.stringify(describeError(error))).not.toContain(
      "private proxy error",
    );
  });

  it("keeps no_session separate from sudo_required despite their shared status", async () => {
    fetchBoundary.mockResolvedValueOnce(
      Response.json(
        { code: "no_session", requestId: "request-session" },
        { status: 401 },
      ),
    );
    fetchBoundary.mockResolvedValueOnce(
      Response.json(
        {
          code: "sudo_required",
          details: { factor: "password" },
          requestId: "request-sudo",
        },
        { status: 401 },
      ),
    );
    const noSession = await requireJsonData(
      client.GET("/api/prohibitorum/me"),
    ).catch((error: unknown) => error);
    const sudoRequired = await client
      .POST("/api/prohibitorum/me/credentials/rename", {
        body: { id: 7, nickname: "Desk key" },
      })
      .catch((error: unknown) => error);

    expect(noSession).toMatchObject({
      kind: "http",
      status: 401,
      code: "no_session",
    });
    expect(sudoRequired).toMatchObject({
      kind: "http",
      status: 401,
      code: "sudo_required",
      details: { factor: "password" },
      requestId: "request-sudo",
    });
    expect(describeError(noSession).id).not.toBe(
      describeError(sudoRequired).id,
    );
  });

  it("does not trust unknown error codes, reasons, malformed envelopes, or unsafe IDs", async () => {
    const generic = describeError(new Error());
    fetchBoundary.mockResolvedValueOnce(
      Response.json(
        {
          code: "__proto__",
          details: { reason: "<script>unsafe reason</script>" },
          requestId: "request-unknown",
        },
        { status: 422 },
      ),
    );
    fetchBoundary.mockResolvedValueOnce(
      Response.json(
        { code: "no_session", details: [], requestId: "request-invalid" },
        { status: 401 },
      ),
    );
    fetchBoundary.mockResolvedValueOnce(
      new Response("<html>private server failure</html>", { status: 502 }),
    );
    const unknown = await client
      .GET("/api/prohibitorum/me")
      .catch((error: unknown) => error);
    const malformed = await client
      .GET("/api/prohibitorum/me")
      .catch((error: unknown) => error);
    const html = await client
      .GET("/api/prohibitorum/me")
      .catch((error: unknown) => error);

    expect(describeError(unknown)).toMatchObject({
      id: generic.id,
      requestId: "request-unknown",
    });
    expect(JSON.stringify(describeError(unknown))).not.toContain(
      "unsafe reason",
    );
    expect(malformed).toMatchObject({
      kind: "http",
      status: 401,
      code: undefined,
    });
    expect(html).toMatchObject({ kind: "http", status: 502, code: undefined });
    expect(describeError(html).id).toBe(generic.id);
    expect(
      describeError(
        new ApiError({
          kind: "http",
          code: "unrecognized",
          requestId: "<html>unsafe ID</html>",
        }),
      ).requestId,
    ).toBeUndefined();
  });

  it("separates network failures from cancellation", async () => {
    fetchBoundary.mockRejectedValueOnce(
      new TypeError("private network details"),
    );
    await expect(
      requireJsonData(client.GET("/api/prohibitorum/auth/status")),
    ).rejects.toMatchObject({ kind: "network" });

    const cancellation = new DOMException("Cancelled", "AbortError");
    fetchBoundary.mockRejectedValueOnce(cancellation);
    const error = await requireJsonData(
      client.GET("/api/prohibitorum/auth/status"),
    ).catch((failure: unknown) => failure);
    expect(error).toBe(cancellation);
    expect(isCancellation(error)).toBe(true);
  });

  it("sends credential values unchanged with same-origin credentials", async () => {
    const requests: Request[] = [];
    fetchBoundary.mockImplementation(async (request) => {
      requests.push(request);
      return new Response(null, { status: 204 });
    });
    await client.POST("/api/prohibitorum/me/credentials/rename", {
      body: { id: 7, nickname: "  Desk KEY  " },
    });

    const request = requests[0];
    expect(request?.credentials).toBe("same-origin");
    expect(await request?.json()).toEqual({ id: 7, nickname: "  Desk KEY  " });
  });
});
