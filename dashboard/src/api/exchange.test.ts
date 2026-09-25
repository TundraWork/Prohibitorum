import { describe, expect, it } from "vitest";
import { httpFailure, networkFailure } from "@/api/exchange";

const origin = "https://id.example.test";

function post(path: string, body: BodyInit | null): Request {
  return new Request(`${origin}${path}`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body,
  });
}

describe("request details of a failure", () => {
  it("masks secret fields at any depth and keeps everything else", async () => {
    const request = post(
      "/api/prohibitorum/me/password/set",
      JSON.stringify({
        password: "hunter2",
        nested: {
          totp_code: "123456",
          code_challenge: "visible-challenge",
          list: [{ recovery_codes: ["aaaa", "bbbb"], label: "kept" }],
        },
        token: null,
      }),
    );

    const error = await httpFailure(
      request,
      new Response(null, { status: 500 }),
    );

    expect(JSON.parse(error.exchange?.requestBody ?? "")).toEqual({
      password: "••••••",
      nested: {
        totp_code: "••••••",
        code_challenge: "visible-challenge",
        list: [{ recovery_codes: "••••••", label: "kept" }],
      },
      token: "••••••",
    });
    expect(error.exchange?.requestBody).toContain('\n  "password": "••••••"');
    expect(error.exchange?.requestBody).not.toContain("hunter2");
    expect(error.exchange?.requestBody).not.toContain("123456");
  });

  it("masks secret query parameters and leaves out the origin", async () => {
    const request = new Request(
      `${origin}/api/prohibitorum/device/lookup?code=ABCD-1234&lang=en&code=again`,
    );

    const error = await httpFailure(
      request,
      new Response(null, { status: 404 }),
    );

    expect(error.exchange).toMatchObject({
      method: "GET",
      path: "/api/prohibitorum/device/lookup?code=••••••&lang=en&code=••••••",
    });
    expect(error.exchange?.requestBody).toBeUndefined();
  });

  it("leaves out a request body that is not JSON rather than show it unmasked", async () => {
    const request = post("/api/prohibitorum/me/avatar", "password=hunter2");

    const error = await httpFailure(
      request,
      new Response(null, { status: 400 }),
    );

    expect(error.exchange).toBeDefined();
    expect(error.exchange?.requestBody).toBeUndefined();
  });

  it("keeps the response status, headers in order, and a JSON body indented", async () => {
    const response = Response.json(
      { code: "no_session", requestId: "request-1" },
      {
        status: 401,
        headers: { "x-request-id": "request-1", "cache-control": "no-store" },
      },
    );

    const error = await httpFailure(post("/api/x", "{}"), response);

    expect(error).toMatchObject({
      kind: "http",
      status: 401,
      code: "no_session",
      requestId: "request-1",
    });
    expect(error.exchange?.response).toEqual({
      status: 401,
      headers: [...response.headers],
      body: '{\n  "code": "no_session",\n  "requestId": "request-1"\n}',
    });
    expect(error.exchange?.response?.headers.map(([name]) => name)).toEqual([
      ...response.headers.keys(),
    ]);
    expect(error.exchange?.response?.headers).toContainEqual([
      "cache-control",
      "no-store",
    ]);
  });

  it("keeps a body that is not JSON as sent and leaves out an empty one", async () => {
    const html = await httpFailure(
      post("/api/x", "{}"),
      new Response("<html>Bad gateway</html>", { status: 502 }),
    );
    const empty = await httpFailure(
      post("/api/x", "{}"),
      new Response("", { status: 503 }),
    );

    expect(html).toMatchObject({ status: 502, code: undefined });
    expect(html.exchange?.response?.body).toBe("<html>Bad gateway</html>");
    expect(empty.exchange?.response).toBeDefined();
    expect(empty.exchange?.response?.body).toBeUndefined();
  });

  it("records only the request of a network failure", async () => {
    const cause = new TypeError("Failed to fetch");
    const error = await networkFailure(
      post("/api/prohibitorum/me/password/set", '{"password":"hunter2"}'),
      cause,
    );

    expect(error).toMatchObject({ kind: "network", cause });
    expect(error.exchange).toEqual({
      method: "POST",
      path: "/api/prohibitorum/me/password/set",
      requestBody: '{\n  "password": "••••••"\n}',
    });
  });
});
