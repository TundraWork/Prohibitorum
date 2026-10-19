import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AppGroupView, GroupApplicationView } from "@/api/raw-admin-paths";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { GroupDangerSection } from "@/pages/admin/groups/GroupDangerSection";
import { apiError } from "@/test/app";
import { groupFixture } from "@/test/group";

const navigate = vi.fn<(options: { to: string }) => Promise<void>>();
vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  useNavigate: () => navigate,
}));

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifyError.mockReset();
  navigate.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(
    notifyError,
    vi.fn<(message: MessageDescriptor) => void>(),
  );
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

const apps = (count: number): GroupApplicationView[] =>
  Array.from({ length: count }, (_, index) => ({
    kind: "oidc",
    appId: `client-${index + 1}`,
    displayName: `Application ${index + 1}`,
  }));

function deletes() {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter(
      (request) =>
        request.method === "POST" &&
        new URL(request.url).pathname.endsWith("/delete"),
    );
}

function mount(
  group: AppGroupView,
  used: GroupApplicationView[] | "failing" | "pending",
  extra?: (request: Request) => Response | Promise<Response> | undefined,
) {
  fetchBoundary.mockImplementation(async (request) => {
    const canned = extra?.(request);
    if (canned) return canned;
    const path = new URL(request.url).pathname;
    if (path.endsWith("/applications")) {
      if (used === "pending") return new Promise<Response>(() => {});
      if (used === "failing") {
        return apiError("internal_error", 500);
      }
      return Response.json({ items: used, nextCursor: "" });
    }
    return new Response(null, { status: 204 });
  });
  return render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <GroupDangerSection group={group} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

const deleteButton = () => screen.getByRole("button", { name: "Delete" });

describe("the danger section", () => {
  it("is named like the other pages' and offers deletion only", async () => {
    mount(groupFixture(), []);

    expect(
      screen.getByRole("heading", { level: 2, name: "Delete" }),
    ).toBeVisible();
    expect(screen.getByText("Delete this group")).toBeVisible();
    expect(screen.queryByRole("button", { name: /Disable/ })).toBeNull();
    await waitFor(() => expect(deleteButton()).toBeEnabled());
  });
});

describe("while applications use the group", () => {
  it("disables Delete and says why, as text outside the button", async () => {
    mount(groupFixture(), apps(2));

    const reason =
      "2 applications use this group. Remove it from their access first.";
    expect(await screen.findByText(reason)).toBeVisible();
    expect(deleteButton()).toBeDisabled();
    expect(deleteButton()).not.toContainElement(screen.getByText(reason));
  });

  it("uses the singular for exactly one application", async () => {
    mount(groupFixture(), apps(1));

    expect(
      await screen.findByText(
        "1 application uses this group. Remove it from its access first.",
      ),
    ).toBeVisible();
    expect(deleteButton()).toBeDisabled();
  });

  it("opens nothing when the disabled button is pressed", async () => {
    const user = userEvent.setup();
    mount(groupFixture(), apps(2));
    await screen.findByText(/2 applications use this group/);

    await user.click(deleteButton());

    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(deletes()).toHaveLength(0);
  });
});

describe("while nothing uses the group", () => {
  it("enables Delete and gives no reason", async () => {
    mount(groupFixture(), []);

    await waitFor(() => expect(deleteButton()).toBeEnabled());
    expect(screen.queryByText(/use this group/)).toBeNull();
  });

  it("waits for the applications read before it allows deleting", () => {
    mount(groupFixture(), "pending");

    expect(deleteButton()).toBeDisabled();
    expect(screen.queryByText(/use this group/)).toBeNull();
  });

  it("allows deleting when the read fails, without a toast, and leaves the refusal to the server", async () => {
    mount(groupFixture(), "failing");

    await waitFor(() => expect(deleteButton()).toBeEnabled());
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("the delete confirmation", () => {
  it("names the group and deletes only after confirming, then leaves for the list", async () => {
    const user = userEvent.setup();
    mount(groupFixture({ id: 7, displayName: "Staff" }), []);
    await waitFor(() => expect(deleteButton()).toBeEnabled());

    await user.click(deleteButton());
    const dialog = await screen.findByRole("alertdialog", {
      name: "Delete Staff?",
    });
    expect(within(dialog).getByText(/Staff is removed for good/)).toBeVisible();
    expect(deletes()).toHaveLength(0);
    await user.click(
      within(dialog).getByRole("button", { name: "Delete group" }),
    );

    await waitFor(() => expect(deletes()).toHaveLength(1));
    expect(new URL(deletes()[0]?.url ?? "").pathname).toBe(
      "/api/prohibitorum/groups/7/delete",
    );
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/admin/groups" }),
    );
  });

  it("does nothing when the confirmation is cancelled", async () => {
    const user = userEvent.setup();
    mount(groupFixture(), []);
    await waitFor(() => expect(deleteButton()).toBeEnabled());

    await user.click(deleteButton());
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));

    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(deletes()).toHaveLength(0);
    expect(navigate).not.toHaveBeenCalled();
  });

  it("keeps the confirmation open while the write is in flight", async () => {
    const user = userEvent.setup();
    let release: (response: Response) => void = () => {};
    mount(groupFixture(), [], (request) =>
      new URL(request.url).pathname.endsWith("/delete")
        ? new Promise<Response>((resolve) => {
            release = resolve;
          })
        : undefined,
    );
    await waitFor(() => expect(deleteButton()).toBeEnabled());

    await user.click(deleteButton());
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Delete group" }),
    );
    await waitFor(() => expect(deletes()).toHaveLength(1));
    await user.keyboard("{Escape}");

    expect(screen.getByRole("alertdialog")).toBeVisible();
    release(new Response(null, { status: 204 }));
  });

  it("reports a server refusal once, inline, and stays on the page", async () => {
    const user = userEvent.setup();
    mount(groupFixture(), [], (request) =>
      new URL(request.url).pathname.endsWith("/delete")
        ? apiError("group_in_use", 409)
        : undefined,
    );
    await waitFor(() => expect(deleteButton()).toBeEnabled());

    await user.click(deleteButton());
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Delete group" }),
    );

    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    const alerts = await screen.findAllByRole("alert");
    expect(alerts).toHaveLength(1);
    expect(alerts[0]).toHaveTextContent(
      "One or more applications still use this user group.",
    );
    expect(notifyError).not.toHaveBeenCalled();
    expect(navigate).not.toHaveBeenCalled();
  });
});
