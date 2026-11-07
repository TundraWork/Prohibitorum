import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AccountChooser, AccountChooserAccount } from "@/api/raw-paths";
import { readLastUsed, writeLastUsed } from "@/app/account-storage";
import { loadDocument } from "@/app/load-document";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import {
  apiError,
  browserAccount,
  browserAccountsApi,
  type FakeApi,
  fakeApi,
  publicApi,
  renderApp,
  testSession,
} from "@/test/app";

vi.mock("@/app/load-document", () => ({ loadDocument: vi.fn() }));

const chooserPath = "/api/prohibitorum/account-chooser";

function chooserAccount(
  accountId: number,
  username: string,
  displayName: string,
  allowed = true,
): AccountChooserAccount {
  return {
    accountId,
    u: `${username.slice(0, 2)}0${accountId}`,
    username,
    displayName,
    allowed,
  };
}

const lin = chooserAccount(12, "lin", "Lin Xiaoyu");
const ops = chooserAccount(40, "ops-oncall", "Ops on call");
const qa = chooserAccount(41, "qa-test", "QA test", false);

function chooser(accounts: AccountChooserAccount[]): AccountChooser {
  return {
    app: { kind: "oidc", key: "oidc:wiki", name: "Wiki" },
    accounts,
    loginHint: "",
    otherAccountUrl:
      "/login?return_to=%2Fapi%2Fprohibitorum%2Faccount-chooser%2Fcontinue%3Fticket%3Dc-1",
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

function chooserApi(
  read: () => Response | Promise<Response> = () =>
    Response.json(chooser([lin, ops, qa])),
): FakeApi {
  return fakeApi({
    ...publicApi(testSession),
    [`GET ${chooserPath}`]: read,
    [`POST ${chooserPath}`]: () =>
      Response.json({
        redirect: "/oauth/authorize?client_id=wiki&account_choice=c-1",
      }),
  });
}

const row = (name: RegExp) => screen.getByRole("button", { name });

describe("choosing an account for an application", () => {
  it("draws three placeholder rows while the accounts load", async () => {
    chooserApi(() => new Promise<Response>(() => {}));
    renderApp("/choose-account?ticket=c-1", queryClient);
    expect(await screen.findByRole("status")).toHaveTextContent("Loading");
    await waitFor(() =>
      expect(
        document.querySelectorAll('li [data-slot="skeleton"].rounded-full'),
      ).toHaveLength(3),
    );
  });

  it("lists the accounts in order, marks the one without access, and ends with another account", async () => {
    chooserApi();
    renderApp("/choose-account?ticket=c-1", queryClient);
    expect(
      await screen.findByRole("heading", { name: "Choose an account" }),
    ).toBeVisible();
    const list = screen.getByRole("list", { name: "Accounts" });
    expect(
      within(list)
        .getAllByRole("button")
        .map((button) => button.textContent),
    ).toEqual([
      "LLin Xiaoyulin",
      "OOps on callops-oncall",
      "QQA testqa-testNot allowed to use this app",
      "Use another account",
    ]);
    expect(row(/QA test/)).toHaveAttribute("aria-disabled", "true");
    expect(row(/Lin Xiaoyu/)).not.toHaveAttribute("aria-disabled");
    expect(
      document.querySelector('[data-slot="stage-strip"]'),
    ).toHaveTextContent("Wiki");
  });

  it("labels the account last used for this application without moving it", async () => {
    writeLastUsed("oidc:wiki", ops.u);
    chooserApi();
    renderApp("/choose-account?ticket=c-1", queryClient);
    const opsRow = await screen.findByRole("button", { name: /Ops on call/ });
    expect(within(opsRow).getByText("Last used")).toBeVisible();
    expect(screen.getAllByText("Last used")).toHaveLength(1);
    const list = screen.getByRole("list", { name: "Accounts" });
    expect(within(list).getAllByRole("button")[1]).toBe(opsRow);
    expect(loadDocument).not.toHaveBeenCalled();
  });

  it("holds every row while the chosen one goes, then loads the protocol and remembers the choice", async () => {
    const api = chooserApi();
    let answer!: (response: Response) => void;
    api.routes[`POST ${chooserPath}`] = () =>
      new Promise<Response>((resolve) => {
        answer = resolve;
      });
    const user = userEvent.setup();
    renderApp("/choose-account?ticket=c-1", queryClient);
    await user.click(
      await screen.findByRole("button", { name: /Ops on call/ }),
    );

    await waitFor(() =>
      expect(row(/Ops on call/)).toHaveAttribute("aria-busy", "true"),
    );
    expect(
      row(/Ops on call/).querySelector('[data-slot="spinner"]'),
    ).not.toBeNull();
    expect(document.querySelectorAll('li [data-slot="spinner"]')).toHaveLength(
      1,
    );
    expect(row(/Lin Xiaoyu/)).toHaveAttribute("aria-disabled", "true");
    expect(row(/Use another account/)).toHaveAttribute("aria-disabled", "true");
    await user.click(row(/Lin Xiaoyu/));
    expect(api.sent("POST", chooserPath)).toHaveLength(1);
    expect(api.sent("POST", chooserPath)[0]?.body).toEqual({
      ticket: "c-1",
      accountId: 40,
    });

    answer(
      Response.json({
        redirect: "/oauth/authorize?client_id=wiki&account_choice=c-1",
      }),
    );
    await waitFor(() =>
      expect(loadDocument).toHaveBeenCalledWith(
        "/oauth/authorize?client_id=wiki&account_choice=c-1",
      ),
    );
    expect(readLastUsed("oidc:wiki")).toBe(ops.u);
    // Still held while the next page loads; the back-forward cache lets go.
    expect(row(/Ops on call/)).toHaveAttribute("aria-busy", "true");
    window.dispatchEvent(
      Object.assign(new Event("pageshow"), { persisted: true }),
    );
    await waitFor(() =>
      expect(row(/Ops on call/)).not.toHaveAttribute("aria-busy"),
    );
  });

  it("opens the consent page in place, keeping the stage, when authorize leads there", async () => {
    const api = chooserApi();
    let lead!: (response: Response) => void;
    api.routes["GET /oauth/authorize"] = () =>
      new Promise<Response>((resolve) => {
        lead = resolve;
      });
    api.routes["GET /api/prohibitorum/consent"] = () =>
      Response.json({
        client: { clientId: "wiki", displayName: "Wiki", host: "wiki.example" },
        account: { displayName: "Ops on call" },
        scopes: ["openid"],
      });
    const user = userEvent.setup();
    renderApp("/choose-account?ticket=c-1", queryClient);
    await user.click(
      await screen.findByRole("button", { name: /Ops on call/ }),
    );
    const stage = document.querySelector('[data-slot="stage"]');

    await waitFor(() =>
      expect(api.sent("GET", "/oauth/authorize")).toHaveLength(1),
    );
    const [asked] = api.sent("GET", "/oauth/authorize");
    expect(asked?.url.search).toBe("?client_id=wiki&account_choice=c-1");
    expect(asked?.headers.get("X-Prohibitorum-Navigation")).toBe("follow");
    // Held while the hop answers.
    expect(row(/Ops on call/)).toHaveAttribute("aria-busy", "true");

    lead(
      Response.json({
        location: `${window.location.origin}/consent?ticket=t-9&return_to=%2Foauth%2Fauthorize%3Fclient_id%3Dwiki`,
      }),
    );
    expect(
      await screen.findByRole("heading", {
        name: "Allow Wiki to access your account?",
      }),
    ).toBeVisible();
    expect(api.sent("GET", "/api/prohibitorum/consent")[0]?.url.search).toBe(
      "?ticket=t-9",
    );
    expect(loadDocument).not.toHaveBeenCalled();
    // The same stage carries on: the layout was never drawn again.
    expect(document.querySelector('[data-slot="stage"]')).toBe(stage);
  });

  it("loads an application's address, held by the server, as a document", async () => {
    const api = chooserApi();
    api.routes["GET /oauth/authorize"] = () =>
      Response.json({ location: "/oauth/handoff" });
    const user = userEvent.setup();
    renderApp("/choose-account?ticket=c-1", queryClient);
    await user.click(
      await screen.findByRole("button", { name: /Ops on call/ }),
    );
    await waitFor(() =>
      expect(loadDocument).toHaveBeenCalledWith("/oauth/handoff"),
    );
    expect(row(/Ops on call/)).toHaveAttribute("aria-busy", "true");
  });

  it("does not choose an account without access", async () => {
    const api = chooserApi();
    const user = userEvent.setup();
    renderApp("/choose-account?ticket=c-1", queryClient);
    await user.click(await screen.findByRole("button", { name: /QA test/ }));
    expect(api.sent("POST", chooserPath)).toHaveLength(0);
  });

  it("leaves only another account to pick when none has access", async () => {
    chooserApi(() =>
      Response.json(
        chooser([
          { ...lin, allowed: false },
          { ...qa, allowed: false },
        ]),
      ),
    );
    const user = userEvent.setup();
    const router = renderApp("/choose-account?ticket=c-1", queryClient);
    const list = await screen.findByRole("list", { name: "Accounts" });
    const enabled = within(list)
      .getAllByRole("button")
      .filter((button) => !button.hasAttribute("aria-disabled"));
    expect(enabled.map((button) => button.textContent)).toEqual([
      "Use another account",
    ]);
    await user.click(row(/Use another account/));
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(
      new URLSearchParams(router.state.location.searchStr).get("return_to"),
    ).toBe("/api/prohibitorum/account-chooser/continue?ticket=c-1");
  });

  it("reads the list again when the chosen account has signed out meanwhile", async () => {
    let accounts = [lin, ops];
    const api = chooserApi(() => Response.json(chooser(accounts)));
    api.routes[`POST ${chooserPath}`] = () => {
      accounts = [lin];
      return apiError("account_not_signed_in", 401);
    };
    const user = userEvent.setup();
    renderApp("/choose-account?ticket=c-1", queryClient);
    await user.click(
      await screen.findByRole("button", { name: /Ops on call/ }),
    );
    await waitFor(() =>
      expect(
        screen.queryByRole("button", { name: /Ops on call/ }),
      ).not.toBeInTheDocument(),
    );
    expect(row(/Lin Xiaoyu/)).not.toHaveAttribute("aria-disabled");
    expect(loadDocument).not.toHaveBeenCalled();
  });

  it("says a choice that ran out has expired", async () => {
    chooserApi(() => apiError("account_chooser_expired", 404));
    renderApp("/choose-account?ticket=c-1", queryClient);
    expect(
      await screen.findByText(
        "This account choice has expired. Go back to the app and sign in again.",
      ),
    ).toBeVisible();
    expect(
      screen.getByRole("heading", { name: "Unable to continue" }),
    ).toBeVisible();
  });
});

describe("choosing between accounts an address matches", () => {
  it("opens the address again with the chosen account's own u", async () => {
    const first = browserAccount(testSession, "ab12cd34");
    const second = browserAccount(
      { id: 2, username: "bob", displayName: "Bob" },
      "ab12ef56",
    );
    fakeApi({
      ...publicApi(testSession),
      "GET /api/prohibitorum/auth/accounts": browserAccountsApi([
        first,
        second,
      ]),
    });
    const user = userEvent.setup();
    const router = renderApp(
      "/choose-account?return_to=%2Fsecurity",
      queryClient,
    );
    await user.click(await screen.findByRole("button", { name: /Bob/ }));
    await waitFor(() =>
      expect(router.state.location.pathname).toBe("/security"),
    );
    expect(new URLSearchParams(router.state.location.searchStr).get("u")).toBe(
      "ab12ef56",
    );
  });
});
