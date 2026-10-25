import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import {
  AppManagerNote,
  AppManagersSection,
} from "@/components/custom/AppManagersSection";
import { i18n } from "@/i18n";

const ana = {
  id: 7,
  username: "ana",
  displayName: "Ana Lima",
  disabled: false,
  assignedAt: new Date(Date.now() - 14 * 86_400_000).toISOString(),
};
const ben = {
  id: 8,
  username: "ben",
  displayName: "Ben Ito",
  disabled: false,
  assignedAt: new Date(Date.now() - 3_600_000).toISOString(),
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
        request.method === "POST" &&
        new URL(request.url).pathname.endsWith(suffix),
    );
}

function mount({
  managers = [ana],
  isAdmin = true,
  extra,
}: {
  managers?: (typeof ana)[];
  isAdmin?: boolean;
  extra?: (request: Request) => Response | undefined;
} = {}) {
  fetchBoundary.mockImplementation(async (request) => {
    const canned = extra?.(request);
    if (canned) return canned;
    const path = new URL(request.url).pathname;
    if (request.method === "GET" && path.endsWith("/managers")) {
      return Response.json(managers);
    }
    if (request.method === "GET" && path.endsWith("/accounts")) {
      return Response.json({
        items: [
          { id: 8, username: "ben", displayName: "Ben Ito", disabled: false },
          { id: 9, username: "old", displayName: "Old Hand", disabled: true },
        ],
      });
    }
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <AppManagersSection kind="oidc" appId="grafana" isAdmin={isAdmin} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

async function openAssign(user: ReturnType<typeof userEvent.setup>) {
  await user.click(
    await screen.findByRole("button", { name: "Assign a manager" }),
  );
  const dialog = await screen.findByRole("dialog", {
    name: "Assign a manager",
  });
  await user.click(within(dialog).getByRole("combobox"));
  await user.click(await screen.findByRole("option", { name: /Ben Ito/ }));
  return dialog;
}

describe("the managers", () => {
  it("state the real scope and show each manager", async () => {
    mount({ managers: [ana, ben] });

    expect(
      await screen.findByText(
        "Managers can change every setting on this page except the list of managers.",
      ),
    ).toBeVisible();
    const list = await screen.findByRole("list", { name: "Managers" });
    const [first] = within(list).getAllByRole("listitem") as [HTMLElement];
    expect(first).toHaveTextContent("Ana Lima");
    expect(first).toHaveTextContent("@ana");
    expect(first).toHaveTextContent("Assigned 2 weeks ago");
  });

  it("show an empty state with no manager", async () => {
    mount({ managers: [] });

    expect(await screen.findByText("No managers assigned yet")).toBeVisible();
  });

  it("confirm before removing one", async () => {
    const user = userEvent.setup();
    mount();

    await user.click(
      await screen.findByRole("button", { name: "Remove Ana Lima" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    expect(writes("/managers/remove")).toHaveLength(0);
    await user.click(within(dialog).getByRole("button", { name: "Remove" }));

    await waitFor(() => expect(writes("/managers/remove")).toHaveLength(1));
    expect(await writes("/managers/remove")[0]?.json()).toEqual({
      accountId: 7,
    });
    await waitFor(() =>
      expect(notifySuccess).toHaveBeenCalledWith(
        expect.objectContaining({ id: "success.app.manager-removed" }),
      ),
    );
  });
});

describe("assigning a manager", () => {
  it("lists disabled accounts with the reason and cannot pick them", async () => {
    const user = userEvent.setup();
    mount();

    await user.click(
      await screen.findByRole("button", { name: "Assign a manager" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Assign a manager",
    });
    await user.click(within(dialog).getByRole("combobox"));

    const option = await screen.findByRole("option", { name: /Old Hand/ });
    expect(option).toHaveTextContent("Disabled account");
    expect(option).toHaveAttribute("aria-disabled", "true");
  });

  it("lists an account that already manages with the reason", async () => {
    const user = userEvent.setup();
    mount({ managers: [ana, ben] });

    await user.click(
      await screen.findByRole("button", { name: "Assign a manager" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Assign a manager",
    });
    await user.click(within(dialog).getByRole("combobox"));

    const option = await screen.findByRole("option", { name: /Ben Ito/ });
    expect(option).toHaveTextContent("Already manages this application");
    expect(option).toHaveAttribute("aria-disabled", "true");
  });

  it("assigns the chosen account and says so", async () => {
    const user = userEvent.setup();
    mount();

    const dialog = await openAssign(user);
    await user.click(within(dialog).getByRole("button", { name: "Assign" }));

    await waitFor(() => expect(writes("/managers")).toHaveLength(1));
    expect(await writes("/managers")[0]?.json()).toEqual({ accountId: 8 });
    await waitFor(() =>
      expect(notifySuccess).toHaveBeenCalledWith(
        expect.objectContaining({ id: "success.app.manager-assigned" }),
      ),
    );
    await waitFor(() =>
      expect(
        screen.queryByRole("dialog", { name: "Assign a manager" }),
      ).not.toBeInTheDocument(),
    );
  });

  it("shows a refused assignment in the dialog, once, with no success toast", async () => {
    const user = userEvent.setup();
    mount({
      extra: (request) =>
        request.method === "POST" &&
        new URL(request.url).pathname.endsWith("/managers")
          ? Response.json(
              { code: "rate_limited", requestId: "r9" },
              { status: 429 },
            )
          : undefined,
    });

    const dialog = await openAssign(user);
    await user.click(within(dialog).getByRole("button", { name: "Assign" }));

    expect(
      await within(dialog).findByText("Too many requests. Wait a moment."),
    ).toBeVisible();
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(notifySuccess).not.toHaveBeenCalled();
    expect(notifyError).not.toHaveBeenCalled();
    expect(dialog).toBeVisible();
  });

  it("asks for an account when none is chosen", async () => {
    const user = userEvent.setup();
    mount();

    await user.click(
      await screen.findByRole("button", { name: "Assign a manager" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Assign a manager",
    });
    await user.click(within(dialog).getByRole("button", { name: "Assign" }));

    expect(
      await within(dialog).findByText("Choose an account to assign."),
    ).toBeVisible();
    expect(writes("/managers")).toHaveLength(0);
  });
});

describe("a delegated manager", () => {
  it("sees no managers section and the page never asks for the list", async () => {
    mount({ isAdmin: false });

    await waitFor(() =>
      expect(
        screen.queryByRole("heading", { name: "Managers" }),
      ).not.toBeInTheDocument(),
    );
    expect(fetchBoundary).not.toHaveBeenCalled();
  });

  it("is told so by the note, which an administrator never sees", () => {
    const { rerender } = render(
      <I18nProvider i18n={i18n}>
        <AppManagerNote isAdmin={false} />
      </I18nProvider>,
    );
    expect(
      screen.getByText("You are assigned to manage this application."),
    ).toBeVisible();

    rerender(
      <I18nProvider i18n={i18n}>
        <AppManagerNote isAdmin />
      </I18nProvider>,
    );
    expect(
      screen.queryByText("You are assigned to manage this application."),
    ).not.toBeInTheDocument();
  });
});

describe("failures", () => {
  it("reports an unreadable list once, in the section, with no toast", async () => {
    mount({
      extra: (request) =>
        request.method === "GET" &&
        new URL(request.url).pathname.endsWith("/managers")
          ? Response.json({ code: "internal_error" }, { status: 500 })
          : undefined,
    });

    expect(
      await screen.findByText("Could not load the managers."),
    ).toBeVisible();
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("reports a refused removal once, inline, with no success toast", async () => {
    const user = userEvent.setup();
    mount({
      extra: (request) =>
        request.method === "POST" &&
        new URL(request.url).pathname.endsWith("/managers/remove")
          ? Response.json(
              { code: "rate_limited", requestId: "r2" },
              { status: 429 },
            )
          : undefined,
    });

    await user.click(
      await screen.findByRole("button", { name: "Remove Ana Lima" }),
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
});
