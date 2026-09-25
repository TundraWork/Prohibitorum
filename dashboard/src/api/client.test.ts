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

  it("keeps the masked request and the response of a failed write", async () => {
    fetchBoundary.mockResolvedValueOnce(
      Response.json(
        { code: "bad_credentials", requestId: "request-password" },
        { status: 401, headers: { "x-request-id": "request-password" } },
      ),
    );
    const error = await client
      .POST("/api/prohibitorum/me/sudo/complete", {
        body: { current_password: "old secret", totp_code: "123456" },
      })
      .catch((failure: unknown) => failure);

    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({
      kind: "http",
      status: 401,
      code: "bad_credentials",
      requestId: "request-password",
      exchange: {
        method: "POST",
        path: "/api/prohibitorum/me/sudo/complete",
        response: {
          status: 401,
          body: '{\n  "code": "bad_credentials",\n  "requestId": "request-password"\n}',
        },
      },
    });
    const { exchange } = error as ApiError;
    expect(JSON.parse(exchange?.requestBody ?? "")).toEqual({
      current_password: "••••••",
      totp_code: "••••••",
    });
    expect(exchange?.requestBody).not.toContain("secret");
    expect(exchange?.requestBody).not.toContain("123456");
    expect(exchange?.response?.headers).toContainEqual([
      "x-request-id",
      "request-password",
    ]);
    expect(JSON.stringify(describeError(error))).not.toContain("secret");
  });

  it("keeps the request of a network failure without a response", async () => {
    fetchBoundary.mockRejectedValueOnce(new TypeError("Failed to fetch"));
    const error = await client
      .POST("/api/prohibitorum/me/credentials/rename", {
        body: { id: 7, nickname: "Desk key" },
      })
      .catch((failure: unknown) => failure);

    expect(error).toMatchObject({
      kind: "network",
      exchange: {
        method: "POST",
        path: "/api/prohibitorum/me/credentials/rename",
      },
    });
    const { exchange } = error as ApiError;
    expect(JSON.parse(exchange?.requestBody ?? "")).toEqual({
      id: 7,
      nickname: "Desk key",
    });
    expect(exchange?.response).toBeUndefined();
  });
});
