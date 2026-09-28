import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FederationFlow } from "@/api/raw-paths";
import { loadDocument } from "@/app/load-document";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { apiError, fakeApi, publicApi, renderApp } from "@/test/app";

vi.mock("@/app/load-document", () => ({ loadDocument: vi.fn() }));

const flowPath = "/api/prohibitorum/auth/federation/flows/f-1";
const address = "/federation/flow/f-1";

const identify: FederationFlow = {
  provider: { slug: "vrchat", displayName: "VRChat", protocol: "vrchat" },
  intent: "enroll",
  step: "identify",
  requiresLocalUsername: false,
  expiresAt: "2026-10-03T14:32:00Z",
};

const proof: FederationFlow = {
  ...identify,
  step: "proof",
  profileUrl: "https://vrchat.com/home/user/usr_3f2a9c",
  proofUrl: "https://id.example/verify/vrchat/proof-token",
};

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

function flowApi(flow: FederationFlow = identify) {
  let current = flow;
  const api = fakeApi({
    ...publicApi(null),
    [`GET ${flowPath}`]: () => Response.json(current),
  });
  return {
    ...api,
    /** What the next read of the flow answers. */
    moveTo: (next: FederationFlow) => {
      current = next;
    },
  };
}

function usernameField() {
  return screen.queryByRole("textbox", {
    name: "Username for your new account",
  });
}

describe("VRChat profile verification", () => {
  it.each([
    ["login", "Sign in through VRChat"],
    ["link", "Link your VRChat profile"],
    ["invite", "Verify your VRChat profile"],
    ["enroll", "Verify your VRChat profile"],
  ] as const)("titles a %s flow", async (intent, title) => {
    flowApi({ ...identify, intent });
    renderApp(address, queryClient);
    expect(await screen.findByRole("heading", { name: title })).toHaveFocus();
  });

  it("names the profile, then moves to the proof with the link to copy", async () => {
    const api = flowApi();
    api.routes[`POST ${flowPath}/prepare`] = () => Response.json(proof);
    const user = userEvent.setup();
    renderApp(address, queryClient);
    await screen.findByRole("heading", { name: "Verify your VRChat profile" });
    expect(
      screen.getByText(/VRChat only proves the profile is yours/),
    ).toBeVisible();
    expect(screen.getByRole("link", { name: "vrchat.com" })).toHaveAttribute(
      "target",
      "_blank",
    );
    await user.type(
      screen.getByRole("textbox", {
        name: "VRChat profile address or user ID",
      }),
      " usr_3f2a9c ",
    );
    await user.click(screen.getByRole("button", { name: "Continue" }));
    expect(
      await screen.findByRole("textbox", { name: "Verification link" }),
    ).toHaveValue(proof.proofUrl);
    expect(api.sent("POST", `${flowPath}/prepare`)[0]?.body).toEqual({
      identity: "usr_3f2a9c",
    });
    expect(screen.getByText("usr_3f2a9c")).toBeVisible();
    expect(screen.getByRole("link", { name: "Open profile" })).toHaveAttribute(
      "href",
      proof.profileUrl,
    );
    expect(
      screen.getByRole("heading", { name: "Verify your VRChat profile" }),
    ).toHaveFocus();
    expect(usernameField()).not.toBeInTheDocument();
  });

  it("marks an identity the server cannot read on its field", async () => {
    const api = flowApi();
    api.routes[`POST ${flowPath}/prepare`] = () =>
      apiError("vrchat_identity_invalid", 400);
    const user = userEvent.setup();
    renderApp(address, queryClient);
    const field = await screen.findByRole("textbox", {
      name: "VRChat profile address or user ID",
    });
    await user.type(field, "not a profile");
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await waitFor(() => expect(field).toHaveAttribute("aria-invalid", "true"));
    expect(
      screen.getByText(
        "Enter a VRChat profile address or a user ID beginning with usr_.",
      ),
    ).toBeVisible();
  });

  it("asks for a username only when the flow needs one", async () => {
    flowApi({ ...proof, requiresLocalUsername: true });
    renderApp(address, queryClient);
    expect(
      await screen.findByRole("textbox", {
        name: "Username for your new account",
      }),
    ).toBeVisible();
    expect(
      screen.getByText("This is your username on Test instance."),
    ).toBeVisible();
  });

  it("reads the flow again and focuses the username when the server asks for one", async () => {
    const api = flowApi(proof);
    api.routes[`POST ${flowPath}/verify`] = () => {
      api.moveTo({ ...proof, requiresLocalUsername: true });
      return apiError("local_username_required", 400);
    };
    const user = userEvent.setup();
    renderApp(address, queryClient);
    await user.click(await screen.findByRole("button", { name: "Verify" }));
    await waitFor(() => expect(usernameField()).toHaveFocus());
    expect(api.sent("GET", flowPath)).toHaveLength(2);
  });

  it("draws the step the flow is really at when the page is out of date", async () => {
    const api = flowApi(identify);
    api.routes[`POST ${flowPath}/prepare`] = () => {
      api.moveTo(proof);
      return apiError("federation_action_invalid", 409);
    };
    const user = userEvent.setup();
    renderApp(address, queryClient);
    await user.type(
      await screen.findByRole("textbox", {
        name: "VRChat profile address or user ID",
      }),
      "usr_3f2a9c",
    );
    await user.click(screen.getByRole("button", { name: "Continue" }));
    expect(
      await screen.findByRole("textbox", { name: "Verification link" }),
    ).toBeVisible();
    expect(api.sent("GET", flowPath)).toHaveLength(2);
  });

  it("says the profile is verified, then goes on", async () => {
    const api = flowApi({ ...proof, requiresLocalUsername: true });
    const target = "/oauth/authorize?client_id=wiki";
    api.routes[`POST ${flowPath}/verify`] = () =>
      Response.json({ redirect: target });
    const user = userEvent.setup();
    renderApp(address, queryClient);
    await user.type(
      await screen.findByRole("textbox", {
        name: "Username for your new account",
      }),
      "alice",
    );
    await user.click(screen.getByRole("button", { name: "Verify" }));
    expect(
      await screen.findByRole("heading", { name: "Profile verified" }),
    ).toBeVisible();
    expect(api.sent("POST", `${flowPath}/verify`)[0]?.body).toEqual({
      localUsername: "alice",
    });
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await waitFor(() => expect(loadDocument).toHaveBeenCalledWith(target));
  });
});
