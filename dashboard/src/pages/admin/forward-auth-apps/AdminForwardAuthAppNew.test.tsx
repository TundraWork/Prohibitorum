import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import {
  apiError,
  type FakeApi,
  fakeApi,
  publicApi,
  renderApp,
  testSession,
} from "@/test/app";
import { forwardAuthAppFixture } from "@/test/forward-auth-app";

const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  notifyError.mockReset();
  queryClient = createQueryClient(notifyError);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

const base = "/api/prohibitorum";
const created = () => Response.json(forwardAuthAppFixture(), { status: 201 });

function serve(create: () => Response | Promise<Response>): FakeApi {
  return fakeApi({
    ...publicApi({ ...testSession, role: "admin" }),
    [`POST ${base}/forward-auth-apps`]: create,
    [`GET ${base}/forward-auth-apps`]: () =>
      Response.json({ items: [forwardAuthAppFixture()], nextCursor: "" }),
    [`GET ${base}/forward-auth-apps/grafana`]: () =>
      Response.json(forwardAuthAppFixture()),
    [`GET ${base}/managed-applications/forward_auth/grafana/access`]: () =>
      Response.json({
        app: {
          kind: "forward_auth",
          appId: "grafana",
          displayName: "Grafana",
          accessRestricted: false,
        },
        accessRestricted: false,
        providers: [],
        groups: [],
      }),
    [`GET ${base}/groups`]: () => Response.json([]),
    [`GET ${base}/forward-auth-apps/grafana/managers`]: () => Response.json([]),
    [`GET ${base}/oidc-applications`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${base}/saml-applications`]: () =>
      Response.json({ items: [], nextCursor: "" }),
  });
}

async function mountForm() {
  const user = userEvent.setup();
  const router = renderApp("/admin/forward-auth-apps/new", queryClient);
  await screen.findByRole("heading", {
    level: 1,
    name: "New forward-auth application",
  });
  return { user, router };
}

async function fill(
  user: ReturnType<typeof userEvent.setup>,
  values: { name?: string; clientId?: string; host?: string },
) {
  if (values.name)
    await user.type(screen.getByRole("textbox", { name: "Name" }), values.name);
  if (values.clientId)
    await user.type(
      screen.getByRole("textbox", { name: /^Client ID/ }),
      values.clientId,
    );
  if (values.host)
    await user.type(
      screen.getByRole("textbox", { name: /^Hostname/ }),
      values.host,
    );
}

const submit = () => screen.getByRole("button", { name: "Create application" });
const valid = {
  name: "Grafana",
  clientId: "grafana",
  host: "grafana.example.test",
};

describe("the form's shape", () => {
  it("groups the fields into sections a reader can name", async () => {
    serve(created);
    await mountForm();

    expect(
      screen
        .getAllByRole("heading", { level: 2 })
        .map((heading) => heading.textContent),
    ).toEqual(["Application", "Service", "Access"]);
  });

  it("states the restriction's consequence beside its switch", async () => {
    serve(created);
    await mountForm();

    expect(
      screen.getByRole("switch", { name: "Restrict access" }),
    ).toHaveAccessibleDescription(
      /Nobody can use the application until you select a user group/,
    );
  });
});

describe("validation", () => {
  it("asks for a name, a Client ID and a hostname before sending anything", async () => {
    const api = serve(created);
    const { user } = await mountForm();

    await user.click(submit());

    expect(await screen.findByText("Enter a name.")).toBeVisible();
    expect(screen.getByText("Enter a Client ID.")).toBeVisible();
    expect(screen.getByText("Enter a hostname.")).toBeVisible();
    expect(api.sent("POST", `${base}/forward-auth-apps`)).toHaveLength(0);
  });

  it("refuses a Client ID that would not survive in a path", async () => {
    const api = serve(created);
    const { user } = await mountForm();

    await fill(user, { ...valid, clientId: "bad id" });
    await user.click(submit());

    expect(
      await screen.findByText(/Use letters, digits, dots, underscores/),
    ).toBeVisible();
    expect(api.sent("POST", `${base}/forward-auth-apps`)).toHaveLength(0);
  });

  it("refuses a hostname with a scheme", async () => {
    const api = serve(created);
    const { user } = await mountForm();

    await fill(user, { ...valid, host: "https://grafana.example.test" });
    await user.click(submit());

    expect(await screen.findByText(/with no scheme and no port/)).toBeVisible();
    expect(api.sent("POST", `${base}/forward-auth-apps`)).toHaveLength(0);
  });
});

describe("creating", () => {
  it("registers the application and goes to its page", async () => {
    const api = serve(created);
    const { user, router } = await mountForm();

    await fill(user, valid);
    await user.click(screen.getByRole("switch", { name: "Restrict access" }));
    await user.click(submit());

    await screen.findByRole("heading", { level: 1, name: "Grafana" });
    expect(router.state.location.pathname).toBe(
      "/admin/forward-auth-apps/grafana",
    );
    expect(api.sent("POST", `${base}/forward-auth-apps`)[0]?.body).toEqual({
      clientId: "grafana",
      host: "grafana.example.test",
      displayName: "Grafana",
      accessRestricted: true,
    });
  });

  it("puts a taken Client ID or hostname on the Client ID, and says it once", async () => {
    serve(() => apiError("oidc_client_already_exists", 409));
    const { user } = await mountForm();

    await fill(user, valid);
    await user.click(submit());

    const field = screen.getByRole("textbox", { name: /^Client ID/ });
    await waitFor(() => expect(field).toHaveAttribute("aria-invalid", "true"));
    expect(field).toHaveAccessibleDescription(/already in use/);
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("reports any other refusal once, on the form", async () => {
    serve(() => apiError("internal_error", 500));
    const { user } = await mountForm();

    await fill(user, valid);
    await user.click(submit());

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("goes back to the list on Cancel without sending anything", async () => {
    const api = serve(created);
    const { user, router } = await mountForm();

    await user.click(screen.getByRole("link", { name: "Cancel" }));

    await screen.findByRole("heading", {
      level: 1,
      name: "Forward-auth applications",
    });
    expect(router.state.location.pathname).toBe("/admin/forward-auth-apps");
    expect(api.sent("POST", `${base}/forward-auth-apps`)).toHaveLength(0);
  });
});

describe("the list afterwards", () => {
  it("is invalidated by a creation, so it shows the new application", async () => {
    serve(created);
    const { user } = await mountForm();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    await fill(user, valid);
    await user.click(submit());
    await screen.findByRole("heading", { level: 1, name: "Grafana" });

    expect(invalidate).toHaveBeenCalledWith(
      expect.objectContaining({ queryKey: ["admin", "forward-auth-apps"] }),
    );
  });
});
