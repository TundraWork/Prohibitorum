import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { SamlConsentRequest } from "@/api/raw-paths";
import { loadDocument } from "@/app/load-document";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { fakeApi, publicApi, renderApp, testSession } from "@/test/app";

vi.mock("@/app/load-document", () => ({ loadDocument: vi.fn() }));

let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  queryClient = createQueryClient(() => undefined);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
  vi.mocked(loadDocument).mockReset();
});

function samlApi(attributes: string[]) {
  const body: SamlConsentRequest = {
    sp: { id: "nextcloud", displayName: "Nextcloud" },
    account: { displayName: "Alice" },
    attributes,
  };
  return fakeApi({
    ...publicApi(testSession),
    "GET /api/prohibitorum/saml-consent": () => Response.json(body),
    "POST /api/prohibitorum/saml-consent": () =>
      Response.json({ redirect: "/saml/sso/resume?ticket=s-1" }),
  });
}

function rows() {
  return within(screen.getByRole("list", { name: "Nextcloud will receive:" }))
    .getAllByRole("listitem")
    .map((item) => item.textContent);
}

it("names the service and the attributes it will receive", async () => {
  samlApi(["Display name", "Email address"]);
  renderApp("/saml-consent?ticket=s-1", queryClient);
  expect(
    await screen.findByRole("heading", { name: "Continue to Nextcloud?" }),
  ).toHaveFocus();
  expect(rows()).toEqual(["Display name", "Email address"]);
  expect(screen.getByText(/You only need to confirm this once/)).toBeVisible();
});

it("says the basic account information goes when no attribute is configured", async () => {
  samlApi([]);
  renderApp("/saml-consent?ticket=s-1", queryClient);
  await screen.findByRole("heading", { name: "Continue to Nextcloud?" });
  expect(rows()).toEqual(["Your basic account information"]);
});

it("continues with a full load of the endpoint that posts the assertion", async () => {
  const api = samlApi(["Display name"]);
  const user = userEvent.setup();
  renderApp("/saml-consent?ticket=s-1", queryClient);
  await user.click(await screen.findByRole("button", { name: "Continue" }));
  await waitFor(() =>
    expect(loadDocument).toHaveBeenCalledWith("/saml/sso/resume?ticket=s-1"),
  );
  expect(api.sent("POST", "/api/prohibitorum/saml-consent")[0]?.body).toEqual({
    ticket: "s-1",
    decision: "approve",
  });
  expect(screen.getByRole("button", { name: "Not now" })).toBeDisabled();
});

it("declines with its own word", async () => {
  const api = samlApi(["Display name"]);
  api.routes["POST /api/prohibitorum/saml-consent"] = () =>
    Response.json({ redirect: "/" });
  const user = userEvent.setup();
  renderApp("/saml-consent?ticket=s-1", queryClient);
  await user.click(await screen.findByRole("button", { name: "Not now" }));
  await waitFor(() => expect(loadDocument).toHaveBeenCalledWith("/"));
  expect(api.sent("POST", "/api/prohibitorum/saml-consent")[0]?.body).toEqual({
    ticket: "s-1",
    decision: "decline",
  });
});
