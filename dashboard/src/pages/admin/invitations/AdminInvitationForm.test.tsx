import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
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
import { groupFixture, ruleGroupFixture } from "@/test/group";
import { providerFixture } from "@/test/identity-provider";

const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;
let api: FakeApi;

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
const createPath = `${base}/invitations`;

const created = {
  url: "https://test/enroll/tok",
  expiresAt: "2026-10-24T00:00:00Z",
  groupIds: [],
  requireLocalCredential: false,
};

function serve(
  providers: ReturnType<typeof providerFixture>[],
  extra: Record<string, () => Response | Promise<Response>> = {},
) {
  api = fakeApi({
    ...publicApi({ ...testSession, role: "admin" }),
    [`GET ${base}/groups`]: () =>
      Response.json([
        groupFixture({ id: 7, slug: "staff", displayName: "Staff" }),
        ruleGroupFixture({ displayName: "Passkey users" }),
      ]),
    [`GET ${base}/identity-providers`]: () =>
      Response.json({ items: providers }),
    [`POST ${createPath}`]: () => Response.json(created),
    [`GET ${createPath}`]: () => Response.json({ items: [], nextCursor: "" }),
    ...extra,
  });
  return api;
}

async function mount() {
  const router = renderApp("/admin/invitations/new", queryClient);
  await screen.findByRole("form", { name: "New invitation" });
  return router;
}

type User = ReturnType<typeof userEvent.setup>;

async function pickProvider(user: User, label: string) {
  await user.click(screen.getByRole("combobox", { name: /Upstream provider/ }));
  const listbox = await screen.findByRole("listbox");
  await user.click(within(listbox).getByText(label));
}

async function create(user: User) {
  await user.click(screen.getByRole("button", { name: "Create invitation" }));
}

const corp = () => providerFixture({ slug: "corp", displayName: "CORP" });
const verifyOnly = () =>
  providerFixture({
    slug: "verify-only",
    displayName: "VERIFY-ONLY",
    allowSignIn: false,
  });

describe("the page", () => {
  it("is laid out in sections and leads back to the list", async () => {
    serve([corp()]);
    await mount();

    expect(
      screen.getByRole("heading", { level: 1, name: "New invitation" }),
    ).toBeVisible();
    for (const name of ["Role", "Account", "Sign-in"]) {
      expect(screen.getByRole("heading", { level: 2, name })).toBeVisible();
    }
    expect(screen.getByRole("link", { name: "Cancel" })).toHaveAttribute(
      "href",
      "/admin/invitations",
    );
  });

  it("offers the two roles as choices, ordinary account first", async () => {
    serve([corp()]);
    await mount();

    const roles = screen.getByRole("radiogroup", { name: "Role" });
    expect(within(roles).getByRole("radio", { name: "User" })).toBeChecked();
    expect(
      within(roles).getByRole("radio", { name: "Admin" }),
    ).not.toBeChecked();
  });
});

describe("the request", () => {
  it("leaves out what was left blank", async () => {
    const user = userEvent.setup();
    serve([corp()]);
    await mount();

    await create(user);

    await waitFor(() => expect(api.sent("POST", createPath)).toHaveLength(1));
    expect(api.sent("POST", createPath)[0]?.body).toEqual({
      role: "user",
      groupIds: [],
      requireLocalCredential: false,
    });
  });

  it("sends the chosen role, username, manual group as a number and provider", async () => {
    const user = userEvent.setup();
    serve([corp()]);
    await mount();

    await user.click(screen.getByRole("radio", { name: "Admin" }));
    await user.type(
      screen.getByRole("textbox", { name: /Account username/ }),
      " dana ",
    );
    await user.click(screen.getByRole("combobox", { name: /User groups/ }));
    const listbox = await screen.findByRole("listbox");
    // A rule group's membership is its rule's, so only manual groups are offered.
    expect(within(listbox).queryByText("Passkey users")).toBeNull();
    await user.click(within(listbox).getByText("Staff"));
    await user.keyboard("{Escape}");
    await pickProvider(user, "CORP");
    await create(user);

    await waitFor(() => expect(api.sent("POST", createPath)).toHaveLength(1));
    expect(api.sent("POST", createPath)[0]?.body).toEqual({
      role: "admin",
      username: "dana",
      groupIds: [7],
      expectedUpstreamIdpSlug: "corp",
      requireLocalCredential: false,
    });
  });

  it("sends requireLocalCredential as the switch left it", async () => {
    const user = userEvent.setup();
    serve([corp()]);
    await mount();

    await pickProvider(user, "CORP");
    await user.click(
      screen.getByRole("switch", { name: "Require a local sign-in method" }),
    );
    await create(user);

    await waitFor(() => expect(api.sent("POST", createPath)).toHaveLength(1));
    expect(api.sent("POST", createPath)[0]?.body).toMatchObject({
      requireLocalCredential: true,
    });
  });
});

describe("the local sign-in requirement", () => {
  it("locks the switch on, with its reason, for a sign-in-off provider", async () => {
    const user = userEvent.setup();
    serve([corp(), verifyOnly()]);
    await mount();
    const switchEl = screen.getByRole("switch", {
      name: "Require a local sign-in method",
    });
    expect(switchEl).not.toBeChecked();

    await pickProvider(user, "VERIFY-ONLY");
    expect(switchEl).toBeChecked();
    expect(switchEl).toHaveAttribute("aria-disabled", "true");
    expect(
      screen.getByText(/Sign-in is turned off for this provider, so a local/),
    ).toBeVisible();

    // Switching back to an ordinary provider unlocks it again.
    await pickProvider(user, "CORP");
    expect(switchEl).not.toBeChecked();
    expect(switchEl).not.toHaveAttribute("aria-disabled", "true");
  });

  it("offers only providers that can back an invitation", async () => {
    const user = userEvent.setup();
    serve([
      corp(),
      verifyOnly(),
      providerFixture({
        slug: "off",
        displayName: "DISABLED-ONE",
        disabled: true,
      }),
      providerFixture({
        slug: "link",
        displayName: "LINKABLE",
        mode: "link_only",
      }),
      providerFixture({ slug: "idle", displayName: "UNREADY", ready: false }),
    ]);
    await mount();

    await user.click(
      screen.getByRole("combobox", { name: /Upstream provider/ }),
    );
    const listbox = await screen.findByRole("listbox");
    expect(within(listbox).getByText("CORP")).toBeVisible();
    expect(within(listbox).getByText("VERIFY-ONLY")).toBeVisible();
    for (const hidden of ["DISABLED-ONE", "LINKABLE", "UNREADY"]) {
      expect(within(listbox).queryByText(hidden)).toBeNull();
    }
  });
});

describe("a failed create", () => {
  it("puts a link_only rejection on the provider field, once", async () => {
    const user = userEvent.setup();
    serve([corp()], {
      [`POST ${createPath}`]: () => apiError("upstream_idp_link_only", 400),
    });
    await mount();

    await pickProvider(user, "CORP");
    await create(user);

    await waitFor(() => {
      expect(
        screen.getByRole("combobox", { name: /Upstream provider/ }),
      ).toHaveAttribute("aria-invalid", "true");
    });
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("tells any other failure on the form and not as a toast", async () => {
    const user = userEvent.setup();
    serve([corp()], {
      [`POST ${createPath}`]: () => apiError("internal", 500),
    });
    await mount();

    await create(user);

    expect(await screen.findByRole("alert")).toBeVisible();
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("the new link", () => {
  it("is revealed once, with its expiry, and Done leads to the list", async () => {
    const user = userEvent.setup();
    serve([corp()]);
    const router = await mount();

    await create(user);

    const dialog = await screen.findByRole("dialog", {
      name: "Copy the registration link",
    });
    expect(
      within(dialog).getByDisplayValue("https://test/enroll/tok"),
    ).toBeVisible();
    expect(
      within(dialog).getByText(/The link expires on .*2026/),
    ).toBeVisible();
    const done = within(dialog).getByRole("button", { name: "Done" });
    expect(done).toBeDisabled();

    await user.click(
      within(dialog).getByRole("checkbox", {
        name: "I have saved the registration link",
      }),
    );
    await user.click(done);

    await waitFor(() =>
      expect(router.state.location.pathname).toBe("/admin/invitations"),
    );
  });

  it("makes the list read again", async () => {
    const user = userEvent.setup();
    serve([corp()]);
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    await mount();

    await create(user);
    await screen.findByRole("dialog", { name: "Copy the registration link" });

    expect(invalidate).toHaveBeenCalledWith(
      expect.objectContaining({ queryKey: ["admin", "invitations"] }),
    );
  });
});

describe("a failed read", () => {
  it.each([
    ["groups", `GET ${base}/groups`, "The user groups could not be loaded"],
    [
      "providers",
      `GET ${base}/identity-providers`,
      "The providers could not be loaded",
    ],
  ])(
    "tells a failed %s read once, in the page",
    async (_name, route, title) => {
      serve([corp()], { [route]: () => apiError("internal", 500) });
      await mount();

      expect(await screen.findByText(title)).toBeVisible();
      expect(screen.getAllByRole("alert")).toHaveLength(1);
      expect(notifyError).not.toHaveBeenCalled();
    },
  );
});
