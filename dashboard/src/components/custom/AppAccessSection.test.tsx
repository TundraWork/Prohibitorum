import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AppGroupView } from "@/api/raw-admin-paths";
import { createQueryClient } from "@/app/query-client";
import { AppAccessSection } from "@/components/custom/AppAccessSection";
import { i18n } from "@/i18n";

const engineering: AppGroupView = {
  id: 1,
  kind: "manual",
  slug: "engineering",
  displayName: "Engineering",
  exposedToDownstream: false,
};
const passkeyPeople: AppGroupView = {
  id: 2,
  kind: "rule",
  slug: "passkey-people",
  displayName: "Passkey people",
  exposedToDownstream: false,
};
const designers: AppGroupView = {
  id: 3,
  kind: "manual",
  slug: "designers",
  displayName: "Designers",
  exposedToDownstream: false,
};

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifySuccess = vi.fn<(message: MessageDescriptor) => void>();
const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifySuccess.mockReset();
  notifyError.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(notifyError, notifySuccess);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

function writes(suffix: string) {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter(
      (request) =>
        request.method !== "GET" &&
        new URL(request.url).pathname.endsWith(suffix),
    );
}

function mount({
  restricted = true,
  selected,
  directory = [engineering, passkeyPeople, designers],
  isAdmin = true,
  extra,
}: {
  restricted?: boolean;
  selected: AppGroupView[];
  directory?: AppGroupView[];
  isAdmin?: boolean;
  extra?: (request: Request) => Response | Promise<Response> | undefined;
}) {
  fetchBoundary.mockImplementation(async (request) => {
    const canned = extra?.(request);
    if (canned) return canned;
    const path = new URL(request.url).pathname;
    if (request.method === "GET" && path.endsWith("/access")) {
      return Response.json({
        app: {
          kind: "oidc",
          appId: "grafana",
          displayName: "Grafana",
          accessRestricted: restricted,
        },
        accessRestricted: restricted,
        providers: [],
        groups: selected,
      });
    }
    if (request.method === "GET" && path.endsWith("/groups")) {
      return Response.json(directory);
    }
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <AppAccessSection kind="oidc" appId="grafana" isAdmin={isAdmin} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

async function step(name: string) {
  const order = await screen.findByRole("list", { name: "Order of checks" });
  const found = within(order)
    .getAllByRole("listitem")
    .find((item) => item.textContent?.includes(name));
  if (!found) throw new Error(`no step ${name}`);
  return found;
}

describe("the order strip", () => {
  it("lists the selected groups under the step that uses them", async () => {
    mount({ selected: [engineering, passkeyPeople] });

    const denied = await step("Accounts denied by hand in");
    const allowed = await step("Accounts allowed by hand in");
    const rule = await step("Matches a rule");
    const rest = await step("Everyone else");
    expect(denied).toHaveTextContent("Engineering");
    expect(denied).not.toHaveTextContent("Passkey people");
    expect(allowed).toHaveTextContent("Engineering");
    expect(rule).toHaveTextContent("Passkey people");
    expect(rule).not.toHaveTextContent("Engineering");
    expect(rest).toHaveTextContent("Can't sign in");
  });

  it("says None selected for a step no group takes part in", async () => {
    mount({ selected: [engineering] });

    expect(await step("Matches a rule")).toHaveTextContent("None selected");
  });

  it("is left out while the application is open", async () => {
    mount({ restricted: false, selected: [engineering] });

    expect(await screen.findByText("Every account can use this")).toBeVisible();
    expect(
      screen.queryByRole("list", { name: "Order of checks" }),
    ).not.toBeInTheDocument();
  });
});

describe("the selected groups", () => {
  it("name each kind, the slug and a link for administrators", async () => {
    mount({ selected: [engineering, passkeyPeople] });

    const list = await screen.findByRole("list", { name: "Selected groups" });
    const [first, second] = within(list).getAllByRole("listitem") as [
      HTMLElement,
      HTMLElement,
    ];
    expect(first).toHaveTextContent("Chosen by hand");
    expect(first).toHaveTextContent("engineering");
    expect(
      within(first).getByRole("link", { name: "Engineering" }),
    ).toHaveAttribute("href", "/admin/groups/1");
    expect(second).toHaveTextContent("Rule");
  });

  it("show a manager the names as text, not links", async () => {
    mount({ selected: [engineering], isAdmin: false });

    const list = await screen.findByRole("list", { name: "Selected groups" });
    expect(within(list).queryByRole("link")).not.toBeInTheDocument();
    expect(list).toHaveTextContent("Engineering");
  });

  it("stay listed, without removal, while the application is open", async () => {
    mount({ restricted: false, selected: [engineering] });

    const list = await screen.findByRole("list", { name: "Selected groups" });
    expect(list).toHaveTextContent("Engineering");
    expect(
      within(list).queryByRole("button", { name: "Remove Engineering" }),
    ).not.toBeInTheDocument();
  });
});

describe("removing a group", () => {
  it("goes ahead without a question while others remain", async () => {
    const user = userEvent.setup();
    mount({ selected: [engineering, passkeyPeople] });

    await user.click(
      await screen.findByRole("button", { name: "Remove Engineering" }),
    );

    await waitFor(() => expect(writes("/groups")).toHaveLength(1));
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(await writes("/groups")[0]?.json()).toEqual({ groupIds: [2] });
  });

  it("asks first when it is the last group of a restricted application", async () => {
    const user = userEvent.setup();
    mount({ selected: [engineering] });

    await user.click(
      await screen.findByRole("button", { name: "Remove Engineering" }),
    );

    const dialog = await screen.findByRole("alertdialog", {
      name: "Remove the last group?",
    });
    expect(dialog).toHaveTextContent(
      "Nobody can use Grafana until you select one.",
    );
    expect(writes("/groups")).toHaveLength(0);

    await user.click(within(dialog).getByRole("button", { name: "Remove" }));
    await waitFor(() => expect(writes("/groups")).toHaveLength(1));
    expect(await writes("/groups")[0]?.json()).toEqual({ groupIds: [] });
  });

  it("reports a failed removal once, inline, with no toast", async () => {
    const user = userEvent.setup();
    mount({
      selected: [engineering, passkeyPeople],
      extra: (request) =>
        request.method === "PUT"
          ? Response.json(
              { code: "rate_limited", requestId: "r1" },
              { status: 429 },
            )
          : undefined,
    });

    await user.click(
      await screen.findByRole("button", { name: "Remove Engineering" }),
    );

    expect(
      await screen.findByText("Too many requests. Wait a moment."),
    ).toBeVisible();
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("adding a group", () => {
  it("opens the picker on the current selection and saves the whole list", async () => {
    const user = userEvent.setup();
    mount({ selected: [engineering] });

    await user.click(
      await screen.findByRole("button", { name: "Choose groups" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Choose the groups that take part",
    });
    await user.click(within(dialog).getByRole("combobox"));
    await user.click(await screen.findByRole("option", { name: /Designers/ }));
    await user.keyboard("{Escape}");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => expect(writes("/groups")).toHaveLength(1));
    expect(await writes("/groups")[0]?.json()).toEqual({ groupIds: [1, 3] });
  });
});

const rateLimited = () =>
  Response.json({ code: "rate_limited", requestId: "r1" }, { status: 429 });
const refuseWrites = (suffix: string) => (request: Request) =>
  request.method !== "GET" && new URL(request.url).pathname.endsWith(suffix)
    ? rateLimited()
    : undefined;

describe("failures", () => {
  it("reports an unreadable policy once, in the section, with no toast", async () => {
    mount({
      selected: [],
      extra: (request) =>
        request.method === "GET" &&
        new URL(request.url).pathname.endsWith("/access")
          ? Response.json({ code: "internal_error" }, { status: 500 })
          : undefined,
    });

    expect(
      await screen.findByText("Could not load the access policy."),
    ).toBeVisible();
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("reports a refused restriction change once, inline", async () => {
    const user = userEvent.setup();
    mount({
      restricted: false,
      selected: [],
      extra: refuseWrites("/access/set-restricted"),
    });

    await user.click(await screen.findByRole("switch", { name: "Restricted" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Restrict" }));

    expect(
      await screen.findByText("Too many requests. Wait a moment."),
    ).toBeVisible();
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
    expect(notifySuccess).not.toHaveBeenCalled();
  });

  it("reports a refused last-group removal once, inline", async () => {
    const user = userEvent.setup();
    mount({ selected: [engineering], extra: refuseWrites("/groups") });

    await user.click(
      await screen.findByRole("button", { name: "Remove Engineering" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Remove" }));

    expect(
      await screen.findByText("Too many requests. Wait a moment."),
    ).toBeVisible();
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
    expect(notifySuccess).not.toHaveBeenCalled();
  });

  it("keeps the picker open with the error when saving is refused", async () => {
    const user = userEvent.setup();
    mount({ selected: [engineering], extra: refuseWrites("/groups") });

    await user.click(
      await screen.findByRole("button", { name: "Choose groups" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Choose the groups that take part",
    });
    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    expect(
      await within(dialog).findByText("Too many requests. Wait a moment."),
    ).toBeVisible();
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(dialog).toBeVisible();
    expect(notifyError).not.toHaveBeenCalled();
    expect(notifySuccess).not.toHaveBeenCalled();
  });

  it("cannot be dismissed while the save is in flight", async () => {
    const user = userEvent.setup();
    let release: (response: Response) => void = () => {};
    mount({
      selected: [engineering],
      extra: (request) =>
        request.method === "PUT"
          ? new Promise<Response>((resolve) => {
              release = resolve;
            })
          : undefined,
    });

    await user.click(
      await screen.findByRole("button", { name: "Choose groups" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Choose the groups that take part",
    });
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(writes("/groups")).toHaveLength(1));
    await user.keyboard("{Escape}");

    expect(dialog).toBeVisible();
    release(rateLimited());
    expect(
      await within(dialog).findByText("Too many requests. Wait a moment."),
    ).toBeVisible();
  });
});
