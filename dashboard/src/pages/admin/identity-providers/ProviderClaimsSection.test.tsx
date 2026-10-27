import { I18nProvider } from "@lingui/react";
import { QueryClientProvider, useSuspenseQuery } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Suspense } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { identityProviderQueryOptions } from "@/api/queries";
import type { LatestConnectionTestView } from "@/api/raw-admin-paths";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { ProviderClaimsSection } from "@/pages/admin/identity-providers/ProviderClaimsSection";
import { apiError, type FakeApi, fakeApi } from "@/test/app";
import {
  type Provider,
  providerFixture,
  publicOidcConfig,
} from "@/test/identity-provider";

type QueryClient = ReturnType<typeof createQueryClient>;

const slug = "corp";
const putPath = `/api/prohibitorum/identity-providers/${slug}`;
const latestPath = `${putPath}/tests/latest`;

let queryClient: QueryClient;
let api: FakeApi;
const notifyError = vi.fn<(error: unknown) => void>();

beforeEach(() => {
  i18n.activate("en");
  notifyError.mockReset();
});

function provider(overrides: Partial<Provider> = {}): Provider {
  return providerFixture({
    slug,
    displayName: "Corporate SSO",
    config: publicOidcConfig,
    ...overrides,
  });
}

/** A test that finished two minutes ago and read every field. */
function latest(
  overrides: Partial<LatestConnectionTestView> = {},
  absent: readonly string[] = [],
): { latest: LatestConnectionTestView } {
  const field = (name: string, source: string, claim: string) => [
    name,
    { source, claim, present: !absent.includes(name) },
  ];
  return {
    latest: {
      status: "succeeded",
      finishedAt: new Date(Date.now() - 2 * 60_000).toISOString(),
      stale: false,
      fields: Object.fromEntries([
        field("issuer", "id_token", "iss"),
        field("subject", "id_token", "sub"),
        field("username", "id_token", "preferred_username"),
        field("displayName", "id_token", "name"),
        field("email", "id_token", "email"),
        field("emailVerified", "id_token", "email_verified"),
        field("picture", "userinfo", "picture"),
      ]),
      ...overrides,
    },
  };
}

function mount(record: Provider) {
  queryClient = createQueryClient(notifyError);
  queryClient.setQueryData(
    identityProviderQueryOptions(record.slug).queryKey,
    record,
  );
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <Suspense fallback={null}>
          <ProviderObserver slug={record.slug} />
        </Suspense>
        <ProviderClaimsSection provider={record} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

/** Holds the provider query active, as the detail page's route does. */
function ProviderObserver({ slug }: { slug: string }) {
  useSuspenseQuery(identityProviderQueryOptions(slug));
  return null;
}

const claimInput = (name: string) => screen.getByRole("textbox", { name });
const save = () => screen.getByRole("button", { name: "Save" });
const subjectAction = () =>
  screen.getByRole("button", { name: "Change the subject claim and save" });

async function setClaim(
  user: ReturnType<typeof userEvent.setup>,
  name: string,
  value: string,
) {
  await user.clear(claimInput(name));
  await user.type(claimInput(name), value);
}

/** The one configuration write's body. */
function sentConfig() {
  const sent = api.sent("PUT", putPath);
  expect(sent).toHaveLength(1);
  const body = sent[0]?.body as { config: Record<string, unknown> };
  return body.config;
}

/** What the last-test column says for one account field. */
function returned(field: string) {
  const cell = document.querySelector(`[data-claim-result="${field}"]`);
  expect(cell).not.toBeNull();
  return cell as HTMLElement;
}

describe("the last test column", () => {
  it("names the source and claim each field came from, and says when it was", async () => {
    api = fakeApi({ [`GET ${latestPath}`]: () => Response.json(latest()) });
    mount(provider());

    expect(await screen.findByText(/From the test/)).toHaveTextContent(
      "From the test 2 minutes ago",
    );
    expect(returned("username")).toHaveTextContent(
      "ID token · preferred_username",
    );
    expect(returned("displayName")).toHaveTextContent("ID token · name");
    expect(returned("email")).toHaveTextContent("ID token · email");
    expect(returned("picture")).toHaveTextContent("UserInfo · picture");
    expect(returned("subject")).toHaveTextContent("ID token · sub");
    expect(screen.queryByText("Missing")).toBeNull();
    expect(
      screen.queryByText("Configuration changed since"),
    ).not.toBeInTheDocument();
  });

  it("marks a field the provider did not send as missing", async () => {
    api = fakeApi({
      [`GET ${latestPath}`]: () =>
        Response.json(latest({}, ["picture", "displayName"])),
    });
    mount(provider());

    expect(await screen.findAllByText("Missing")).toHaveLength(2);
    expect(returned("picture")).toHaveTextContent("Missing");
    expect(returned("displayName")).toHaveTextContent("Missing");
    expect(returned("email")).toHaveTextContent("ID token · email");
  });

  it("never reports whether the verified flag came back", async () => {
    // The flag is recorded present after every run, whether or not the
    // provider sent it, so the column has nothing true to say about it.
    api = fakeApi({
      [`GET ${latestPath}`]: () => Response.json(latest({}, ["emailVerified"])),
    });
    mount(provider());

    await screen.findByText(/From the test/);
    expect(
      document.querySelector("[data-claim-result=emailVerified]"),
    ).toBeNull();
    expect(screen.queryByText(/email_verified/)).toBeNull();
    expect(screen.queryByText("Missing")).toBeNull();
  });

  it("says the configuration changed since a stale test", async () => {
    api = fakeApi({
      [`GET ${latestPath}`]: () => Response.json(latest({ stale: true })),
    });
    mount(provider());

    await waitFor(() =>
      expect(
        screen.getByLabelText("Claim mapping"),
      ).toHaveAccessibleDescription(
        /From the test 2 minutes ago.*Configuration changed since/,
      ),
    );
    // The rows say so too, so the results recede wherever the page is scrolled.
    expect(document.querySelector("[data-claim-result=email]")).toHaveAttribute(
      "data-stale",
      "true",
    );
  });

  it("does not mark the results of a current test as out of date", async () => {
    api = fakeApi({
      [`GET ${latestPath}`]: () => Response.json(latest({})),
    });
    mount(provider());

    await screen.findByText(/From the test/);
    expect(
      document.querySelector("[data-claim-result=email]"),
    ).not.toHaveAttribute("data-stale");
  });

  it("says a failed test failed, rather than marking every field missing", async () => {
    api = fakeApi({
      [`GET ${latestPath}`]: () =>
        Response.json(latest({ status: "failed", fields: {} })),
    });
    mount(provider());

    expect(await screen.findByText(/The last test failed/)).toHaveTextContent(
      /The last test failed 2 minutes ago.*Run it again from Diagnostics\./,
    );
    expect(screen.queryByText("Missing")).toBeNull();
    expect(document.querySelector("[data-claim-result]")).toBeNull();
  });

  it("says a failed test is also out of date when the configuration moved", async () => {
    api = fakeApi({
      [`GET ${latestPath}`]: () =>
        Response.json(latest({ status: "failed", fields: {}, stale: true })),
    });
    mount(provider());

    expect(await screen.findByText(/The last test failed/)).toHaveTextContent(
      /The last test failed 2 minutes ago.*and the configuration has changed since\. Run it again from Diagnostics\./,
    );
  });

  it("is left out when no test has finished", async () => {
    api = fakeApi({
      [`GET ${latestPath}`]: () => Response.json({ latest: null }),
    });
    mount(provider());

    await waitFor(() => expect(api.sent("GET", latestPath)).toHaveLength(1));
    expect(claimInput("Username")).toBeVisible();
    expect(document.querySelector("[data-claim-result]")).toBeNull();
    expect(screen.queryByText(/From the test/)).toBeNull();
  });

  it("is left out, quietly, when the summary cannot be read", async () => {
    api = fakeApi({
      [`GET ${latestPath}`]: () => apiError("internal_error", 500),
    });
    mount(provider());

    await waitFor(() =>
      expect(api.sent("GET", latestPath).length).toBeGreaterThan(0),
    );
    expect(document.querySelector("[data-claim-result]")).toBeNull();
    expect(screen.queryAllByRole("alert")).toHaveLength(0);
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("the claim mapping", () => {
  it("shows each account field with its claim, and says what the subject is for", async () => {
    api = fakeApi({
      [`GET ${latestPath}`]: () => Response.json({ latest: null }),
    });
    mount(provider());

    expect(claimInput("Username")).toHaveValue("preferred_username");
    expect(claimInput("Display name")).toHaveValue("name");
    expect(claimInput("Email")).toHaveValue("email");
    expect(claimInput("Picture")).toHaveValue("picture");
    expect(claimInput("Subject")).toHaveValue("sub");
    expect(
      screen.getByText("Accounts are linked by the value of this claim."),
    ).toBeVisible();
    expect(
      screen.getByText(
        /Names with dots are matched against the exact claim key/,
      ),
    ).toBeVisible();
  });

  it("holds no footer while nothing is edited", () => {
    api = fakeApi({
      [`GET ${latestPath}`]: () => Response.json({ latest: null }),
    });
    mount(provider());

    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("asks for a claim name before saving", async () => {
    api = fakeApi({
      [`GET ${latestPath}`]: () => Response.json({ latest: null }),
    });
    mount(provider());
    const user = userEvent.setup();

    await user.clear(claimInput("Email"));
    await user.click(save());

    expect(await screen.findByText("Enter a claim name.")).toBeVisible();
    expect(api.sent("PUT", putPath)).toHaveLength(0);
  });
});

describe("the accounts a provider lets in", () => {
  it("says every domain is accepted while the list is empty", () => {
    api = fakeApi({
      [`GET ${latestPath}`]: () => Response.json({ latest: null }),
    });
    mount(provider());

    expect(screen.getByText("Every domain is accepted.")).toBeVisible();
  });

  it("keeps the rest of the record when only the verified switch changes", async () => {
    const stored = provider({
      config: { ...publicOidcConfig, allowedDomains: ["example.com"] },
    });
    api = fakeApi({
      [`GET ${latestPath}`]: () => Response.json({ latest: null }),
      [`PUT ${putPath}`]: () => Response.json(stored),
    });
    mount(stored);
    const user = userEvent.setup();

    await user.click(
      screen.getByRole("switch", { name: "Require a verified email address" }),
    );
    await user.click(save());

    await waitFor(() => expect(api.sent("PUT", putPath)).toHaveLength(1));
    expect(sentConfig()).toMatchObject({
      allowedDomains: ["example.com"],
      requireVerifiedEmail: false,
      issuerUrl: "https://idp.example.com",
      subjectClaim: "sub",
    });
  });
});

describe("changing the subject claim", () => {
  const linked = (count: number) => provider({ linkedAccountCount: count });

  it("states the consequence and names the action while accounts are linked", async () => {
    api = fakeApi({
      [`GET ${latestPath}`]: () => Response.json({ latest: null }),
    });
    mount(linked(18));
    const user = userEvent.setup();

    await setClaim(user, "Subject", "node_id");

    expect(
      await screen.findByText(/18 accounts are linked to Corporate SSO/),
    ).toHaveTextContent(
      /by the sub claim\. After the change to node_id, these links no longer match/,
    );
    expect(subjectAction()).toBeVisible();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("says nothing of it for a claim that is not the subject", async () => {
    api = fakeApi({
      [`GET ${latestPath}`]: () => Response.json({ latest: null }),
    });
    mount(linked(18));
    const user = userEvent.setup();

    await setClaim(user, "Email", "mail");

    expect(save()).toBeVisible();
    expect(screen.queryByText(/accounts are linked to/)).toBeNull();
  });

  it("sends the change only once it is confirmed", async () => {
    api = fakeApi({
      [`GET ${latestPath}`]: () => Response.json({ latest: null }),
      [`PUT ${putPath}`]: () => Response.json(linked(18)),
    });
    mount(linked(18));
    const user = userEvent.setup();

    await setClaim(user, "Subject", "node_id");
    await user.click(subjectAction());
    const dialog = await screen.findByRole("alertdialog", {
      name: /^Change the subject claim for Corporate\sSSO\?$/,
    });
    expect(api.sent("PUT", putPath)).toHaveLength(0);
    // The footer carries the full consequence; the dialog only asks.
    expect(
      within(dialog).getByText(
        /18 linked accounts will no longer match and will be treated as a new identity/,
      ),
    ).toBeVisible();
    expect(within(dialog).queryByText(/are linked to/)).toBeNull();
    await user.click(
      within(dialog).getByRole("button", { name: "Change and save" }),
    );

    await waitFor(() => expect(api.sent("PUT", putPath)).toHaveLength(1));
    expect(sentConfig()).toMatchObject({ subjectClaim: "node_id" });
  });

  it("spins only the confirmation while the confirmed change is sent", async () => {
    let release: (response: Response) => void = () => {};
    api = fakeApi({
      [`GET ${latestPath}`]: () => Response.json({ latest: null }),
      [`PUT ${putPath}`]: () =>
        new Promise<Response>((resolve) => {
          release = resolve;
        }),
    });
    mount(linked(18));
    const user = userEvent.setup();

    await setClaim(user, "Subject", "node_id");
    const action = subjectAction();
    await user.click(action);
    const dialog = await screen.findByRole("alertdialog");
    const confirm = within(dialog).getByRole("button", {
      name: "Change and save",
    });
    await user.click(confirm);
    await waitFor(() => expect(api.sent("PUT", putPath)).toHaveLength(1));

    const spinners = document.querySelectorAll('[data-slot="spinner"]');
    expect(spinners).toHaveLength(1);
    expect(confirm).toContainElement(spinners[0] as HTMLElement);
    expect(action).toBeDisabled();
    release(Response.json(linked(18)));
  });

  it("sends nothing and keeps the edit when the confirmation is cancelled", async () => {
    api = fakeApi({
      [`GET ${latestPath}`]: () => Response.json({ latest: null }),
    });
    mount(linked(1));
    const user = userEvent.setup();

    await setClaim(user, "Subject", "node_id");
    await user.click(subjectAction());
    const dialog = await screen.findByRole("alertdialog");
    expect(
      within(dialog).getByText(/1 linked account will no longer match/),
    ).toBeVisible();
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));

    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(api.sent("PUT", putPath)).toHaveLength(0);
    expect(claimInput("Subject")).toHaveValue("node_id");
  });

  it("goes back to the plain save when the edit is undone", async () => {
    api = fakeApi({
      [`GET ${latestPath}`]: () => Response.json({ latest: null }),
    });
    mount(linked(18));
    const user = userEvent.setup();

    await setClaim(user, "Subject", "node_id");
    await user.click(screen.getByRole("button", { name: "Discard" }));

    expect(claimInput("Subject")).toHaveValue("sub");
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("saves straight away when no account is linked", async () => {
    api = fakeApi({
      [`GET ${latestPath}`]: () => Response.json({ latest: null }),
      [`PUT ${putPath}`]: () => Response.json(linked(0)),
    });
    mount(linked(0));
    const user = userEvent.setup();

    await setClaim(user, "Subject", "node_id");
    expect(screen.queryByText(/accounts are linked to/)).toBeNull();
    await user.click(save());

    await waitFor(() => expect(api.sent("PUT", putPath)).toHaveLength(1));
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(sentConfig()).toMatchObject({ subjectClaim: "node_id" });
  });
});

describe("a refused write", () => {
  it("reports a refused save once, on the section", async () => {
    api = fakeApi({
      [`GET ${latestPath}`]: () => Response.json({ latest: null }),
      [`PUT ${putPath}`]: () => apiError("internal_error", 500),
    });
    mount(provider());
    const user = userEvent.setup();

    await setClaim(user, "Email", "mail");
    await user.click(save());

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("reports a refusal of the confirmed change once, after closing the dialog", async () => {
    api = fakeApi({
      [`GET ${latestPath}`]: () => Response.json({ latest: null }),
      [`PUT ${putPath}`]: () => apiError("internal_error", 500),
    });
    mount(provider({ linkedAccountCount: 3 }));
    const user = userEvent.setup();

    await setClaim(user, "Subject", "node_id");
    await user.click(subjectAction());
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Change and save" }),
    );

    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
    expect(claimInput("Subject")).toHaveValue("node_id");
  });

  it("puts the server's bare refusal on the subject claim, once", async () => {
    api = fakeApi({
      [`GET ${latestPath}`]: () => Response.json({ latest: null }),
      [`PUT ${putPath}`]: () => apiError("bad_request", 400),
    });
    mount(provider());
    const user = userEvent.setup();

    await setClaim(user, "Email", "mail");
    await user.click(save());

    await waitFor(() => expect(claimInput("Subject")).toBeInvalid());
    expect(screen.queryAllByRole("alert")).toHaveLength(0);
    expect(notifyError).not.toHaveBeenCalled();
  });
});
