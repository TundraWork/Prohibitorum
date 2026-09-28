import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import { loadDocument } from "@/app/load-document";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import {
  apiError,
  fakeApi,
  publicApi,
  renderApp,
  testSession,
} from "@/test/app";

vi.mock("@/app/load-document", () => ({ loadDocument: vi.fn() }));
vi.mock("qrcode", () => ({
  default: { toCanvas: vi.fn().mockResolvedValue(undefined) },
}));

type Identity = components["schemas"]["AccountIdentityView"];

const target = "/oauth/authorize?client_id=wiki";
const address = `/setup-signin?redirect=${encodeURIComponent(target)}`;

function identity(id: number, providerDisplayName: string): Identity {
  return {
    id,
    providerSlug: providerDisplayName.toLowerCase(),
    providerDisplayName,
    protocol: "oidc",
    subject: String(id),
    linkedAt: "2026-09-01T00:00:00Z",
    data: {},
  };
}

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

function setupApi(
  identities: Identity[] = [identity(1, "GitLab")],
  session: typeof testSession | null = testSession,
) {
  return fakeApi({
    ...publicApi(session),
    "GET /api/prohibitorum/me/identities": () => Response.json(identities),
  });
}

describe("adding a sign-in after a first federated one", () => {
  it.each([
    ["no redirect", "/setup-signin"],
    ["a protocol-relative one", "/setup-signin?redirect=%2F%2Fx"],
    ["a backslashed one", "/setup-signin?redirect=%2F%5Cx"],
    ["another site", "/setup-signin?redirect=https%3A%2F%2Fx"],
  ])("refuses %s before asking anything", async (_, path) => {
    const api = setupApi();
    renderApp(path, queryClient);
    expect(await screen.findByText("Invalid return address.")).toBeVisible();
    expect(api.sent("GET", "/api/prohibitorum/me/identities")).toHaveLength(0);
  });

  it("sends an anonymous reader to sign in", async () => {
    setupApi([identity(1, "GitLab")], null);
    const router = renderApp(address, queryClient);
    expect(
      await screen.findByRole("heading", { name: "Sign in" }),
    ).toBeVisible();
    expect(router.state.location.pathname).toBe("/login");
  });

  it("goes straight on for an account with nothing linked", async () => {
    setupApi([]);
    renderApp(address, queryClient);
    await waitFor(() => expect(loadDocument).toHaveBeenCalledWith(target));
    expect(
      screen.queryByRole("heading", { name: "Add a way to sign in" }),
    ).not.toBeInTheDocument();
  });

  it("names every linked provider once, joined the way the language joins them", async () => {
    setupApi([
      identity(1, "GitLab"),
      identity(2, "GitHub"),
      identity(3, "GitLab"),
    ]);
    renderApp(address, queryClient);
    expect(
      await screen.findByRole("heading", { name: "Add a way to sign in" }),
    ).toHaveFocus();
    expect(
      screen.getByText(
        "Right now you can only sign in through GitLab and GitHub. Add another way to sign in, so losing access to GitLab and GitHub doesn't lock you out.",
      ),
    ).toBeVisible();
  });

  it("leaves only a way on once the step has timed out", async () => {
    const api = setupApi();
    api.routes["POST /api/prohibitorum/me/password-totp/verify"] = () =>
      apiError("sudo_required", 403);
    const user = userEvent.setup();
    renderApp(address, queryClient);
    await user.click(
      await screen.findByRole("button", {
        name: "Use a password and an authenticator instead",
      }),
    );
    await screen.findByRole("heading", { name: "Set a password" });
    await user.type(screen.getByLabelText("Password"), "correct horse");
    await user.type(screen.getByLabelText("Confirm password"), "correct horse");
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await user.type(
      await screen.findByRole("textbox", { name: "Authenticator code" }),
      "123456",
    );
    await user.click(screen.getByRole("button", { name: "Verify and finish" }));
    expect(
      await screen.findByRole("heading", { name: "Add a way to sign in" }),
    ).toBeVisible();
    const buttons = within(screen.getByRole("main")).getAllByRole("button");
    expect(buttons).toHaveLength(1);
    expect(buttons[0]).toHaveAccessibleName("Continue");
    await user.click(buttons[0] as HTMLElement);
    await waitFor(() => expect(loadDocument).toHaveBeenCalledWith(target));
  });

  it("skips on to where the sign-in was headed", async () => {
    setupApi();
    const user = userEvent.setup();
    renderApp(address, queryClient);
    await user.click(
      await screen.findByRole("button", { name: "Skip for now" }),
    );
    await waitFor(() => expect(loadDocument).toHaveBeenCalledWith(target));
    expect(
      screen.getByText("You can also add one later from Security."),
    ).toBeVisible();
  });

  it("keeps the authenticator's secret after going back to the choice", async () => {
    setupApi();
    const user = userEvent.setup();
    renderApp(address, queryClient);
    const setupKey = async () => {
      await user.click(
        await screen.findByRole("button", {
          name: "Use a password and an authenticator instead",
        }),
      );
      await user.type(
        await screen.findByLabelText("Password"),
        "correct horse",
      );
      await user.type(
        screen.getByLabelText("Confirm password"),
        "correct horse",
      );
      await user.click(screen.getByRole("button", { name: "Continue" }));
      const key = await screen.findByRole("textbox", { name: "Setup key" });
      return (key as HTMLInputElement).value;
    };
    const first = await setupKey();
    await user.click(screen.getByRole("button", { name: "Back" }));
    await user.click(await screen.findByRole("button", { name: "Back" }));
    await screen.findByRole("heading", { name: "Add a way to sign in" });
    expect(await setupKey()).toBe(first);
  });
});
