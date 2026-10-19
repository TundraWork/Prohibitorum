import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { components } from "@/api/generated/schema";
import { AccountPasskeysSection } from "@/pages/admin/users/AccountPasskeysSection";
import { accessHarness, refused } from "@/test/account-access";

type Credential = components["schemas"]["CredentialView"];

const hoursAgo = (hours: number) =>
  new Date(Date.now() - hours * 3_600_000).toISOString();

const laptop: Credential = {
  id: 7,
  nickname: "Work laptop",
  createdAt: hoursAgo(24 * 100),
  lastUsedAt: hoursAgo(24),
  backupState: true,
  credentialIdSuffix: "a1b2",
  attestationType: "none",
  transports: ["internal"],
};
const unnamed: Credential = {
  id: 8,
  createdAt: hoursAgo(24 * 3),
  backupState: false,
  credentialIdSuffix: "c3d4",
  attestationType: "none",
  transports: null,
};

const path = "/api/prohibitorum/accounts/2/credentials";

describe("the passkeys section", () => {
  const harness = accessHarness();
  const serve =
    (items: Credential[], extra?: (request: Request) => Response | undefined) =>
    (request: Request) => {
      const canned = extra?.(request);
      if (canned) return canned;
      if (request.method === "GET" && request.url.endsWith(path)) {
        return Response.json({ items, nextCursor: "" });
      }
      return undefined;
    };

  it("lists each passkey with its name, when it was added and used, and the end of its id", async () => {
    harness.mount(
      <AccountPasskeysSection accountId={2} />,
      serve([laptop, unnamed]),
    );

    const list = await screen.findByRole("list", { name: "Passkeys" });
    const rows = within(list).getAllByRole("listitem");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent("Work laptop");
    expect(rows[0]).toHaveTextContent("Added 3 months ago");
    expect(rows[0]).toHaveTextContent("Last used yesterday");
    expect(rows[0]).toHaveTextContent("…a1b2");
    expect(rows[1]).toHaveTextContent("Unnamed passkey");
    expect(rows[1]).toHaveTextContent("Not used yet");
    expect(
      screen.getByRole("heading", { level: 2, name: "Passkeys" }),
    ).toBeVisible();
  });

  it("says so when the account has none", async () => {
    harness.mount(<AccountPasskeysSection accountId={2} />, serve([]));

    expect(await screen.findByText("No passkeys")).toBeVisible();
  });

  it("tells a failed read once, in the section, and not as a toast", async () => {
    harness.mount(<AccountPasskeysSection accountId={2} />, () => refused());

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(harness.notifyError).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Try again" })).toBeVisible();
  });

  it("removes a passkey only after the confirmation, then reads the list again", async () => {
    const user = userEvent.setup();
    let reads = 0;
    harness.mount(
      <AccountPasskeysSection accountId={2} />,
      serve([laptop], (request) => {
        if (request.method === "GET") reads += 1;
        return undefined;
      }),
    );

    await user.click(
      await screen.findByRole("button", { name: "Remove Work laptop" }),
    );
    const dialog = await screen.findByRole("alertdialog", {
      name: "Remove this passkey?",
    });
    expect(harness.sent("POST", "/credentials/delete")).toHaveLength(0);
    await user.click(
      within(dialog).getByRole("button", { name: "Remove passkey" }),
    );

    await waitFor(() =>
      expect(harness.sent("POST", "/credentials/delete")).toHaveLength(1),
    );
    expect(
      await harness.sent("POST", "/credentials/delete")[0]?.json(),
    ).toEqual({
      accountId: 2,
      credentialId: 7,
    });
    await waitFor(() => expect(reads).toBe(2));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  });

  it("keeps the passkey when the confirmation is cancelled", async () => {
    const user = userEvent.setup();
    harness.mount(<AccountPasskeysSection accountId={2} />, serve([laptop]));

    await user.click(
      await screen.findByRole("button", { name: "Remove Work laptop" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));

    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    expect(harness.sent("POST", "/credentials/delete")).toHaveLength(0);
  });

  it("reports a refused removal once, inline, and closes the question", async () => {
    const user = userEvent.setup();
    harness.mount(
      <AccountPasskeysSection accountId={2} />,
      serve([laptop], (request) =>
        request.method === "POST" ? refused() : undefined,
      ),
    );

    await user.click(
      await screen.findByRole("button", { name: "Remove Work laptop" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Remove passkey" }),
    );

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(harness.notifyError).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    expect(screen.getByText("Work laptop")).toBeVisible();
  });
});
