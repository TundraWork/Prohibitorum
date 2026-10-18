import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { AccountPanel } from "@/pages/profile/AccountPanel";

const mePath = "/api/prohibitorum/me";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifySuccess = vi.fn<(message: MessageDescriptor) => void>();
let queryClient: QueryClient;
/** The bodies `PUT /me` received. */
let saved: unknown[];

const session = {
  id: 1,
  username: "aiko",
  displayName: "Aiko Tanaka",
  role: "user",
  oidcSubject: "3f6c1c9e-8b1d-4c39-9f0e-2a7b5d14e8c2",
};

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifySuccess.mockReset();
  saved = [];
  fetchBoundary.mockImplementation(async (request) => {
    if (new URL(request.url).pathname !== mePath) {
      return new Response(null, { status: 404 });
    }
    if (request.method === "PUT") {
      const body = (await request.json()) as { displayName: string };
      saved.push(body);
      return Response.json({ ...session, displayName: body.displayName });
    }
    return Response.json(session);
  });
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(() => undefined, notifySuccess);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function mount() {
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <AccountPanel />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

const nameField = () =>
  screen.findByRole<HTMLInputElement>("textbox", { name: "Display name" });
const save = () => screen.queryByRole("button", { name: "Save" });
const discard = () => screen.queryByRole("button", { name: "Discard" });

describe("the account section", () => {
  it("offers no Save or Discard until the name changes", async () => {
    const user = userEvent.setup();
    mount();
    const field = await nameField();
    expect(field).toHaveValue("Aiko Tanaka");
    expect(save()).not.toBeInTheDocument();
    expect(discard()).not.toBeInTheDocument();

    await user.type(field, "!");
    expect(save()).toBeVisible();
    expect(discard()).toBeVisible();
  });

  it("restores the saved name and hides both buttons on Discard", async () => {
    const user = userEvent.setup();
    mount();
    const field = await nameField();
    await user.clear(field);
    await user.type(field, "Someone else");
    await user.click(discard() as HTMLElement);

    expect(field).toHaveValue("Aiko Tanaka");
    expect(save()).not.toBeInTheDocument();
    expect(discard()).not.toBeInTheDocument();
  });

  it("hides the buttons again when the edit is typed back to the saved name", async () => {
    const user = userEvent.setup();
    mount();
    const field = await nameField();
    await user.type(field, "x");
    await user.type(field, "{Backspace}");
    expect(save()).not.toBeInTheDocument();
  });

  it("saves the new name, says so, and updates the name the shell reads", async () => {
    const user = userEvent.setup();
    mount();
    const field = await nameField();
    await user.clear(field);
    await user.type(field, "Aiko T.");
    await user.click(save() as HTMLElement);

    await waitFor(() =>
      expect(notifySuccess).toHaveBeenCalledWith(
        expect.objectContaining({ id: "success.display-name.saved" }),
      ),
    );
    expect(queryClient.getQueryData<typeof session>(["session", "me"])).toEqual(
      expect.objectContaining({ displayName: "Aiko T." }),
    );
    expect(saved).toEqual([{ displayName: "Aiko T." }]);
    await waitFor(() => expect(save()).not.toBeInTheDocument());
  });

  it("names a too-long name as the field's error and sends nothing", async () => {
    const user = userEvent.setup();
    mount();
    const field = await nameField();
    await user.clear(field);
    await user.type(field, "a".repeat(61));
    await user.tab();

    expect(
      await screen.findByText("Use 60 characters or fewer."),
    ).toBeVisible();
    expect(field).toBeInvalid();
    await user.click(save() as HTMLElement);
    expect(saved).toEqual([]);
  });

  it("names control characters as the field's error", async () => {
    mount();
    const field = await nameField();
    // userEvent.type would treat a bare control character as a key; set it directly.
    fireEvent.change(field, { target: { value: "Aiko\u0007" } });
    fireEvent.blur(field);

    expect(
      await screen.findByText("Remove control characters from the name."),
    ).toBeVisible();
  });

  it("shows the username and OIDC subject read-only, with the subject's hint", async () => {
    mount();
    await nameField();
    const username = screen.getByRole("textbox", { name: "Username" });
    const subject = screen.getByRole("textbox", { name: "OIDC subject" });
    expect(username).toHaveValue("aiko");
    expect(username).toHaveAttribute("readonly");
    expect(subject).toHaveValue(session.oidcSubject);
    expect(subject).toHaveAttribute("readonly");
    expect(subject).toHaveAccessibleDescription(
      "Applications identify you by this value. It never changes.",
    );
    expect(username).toHaveAccessibleDescription("What you type to sign in.");
  });

  it("copies each identifier whole", async () => {
    const user = userEvent.setup({ delay: null });
    mount();
    await nameField();
    const writeText = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockResolvedValue(undefined);
    const [copyUsername, copySubject] = screen.getAllByRole("button", {
      name: "Copy",
    });
    await user.click(copyUsername as HTMLElement);
    expect(writeText).toHaveBeenLastCalledWith("aiko");
    await user.click(copySubject as HTMLElement);
    expect(writeText).toHaveBeenLastCalledWith(session.oidcSubject);
  });

  it("shows a failed save in the form and reports it once", async () => {
    const notifyError = vi.fn();
    queryClient.clear();
    queryClient = createQueryClient(notifyError, notifySuccess);
    const answer = fetchBoundary.getMockImplementation();
    fetchBoundary.mockImplementation(async (request) =>
      request.method === "PUT"
        ? Response.json(
            { code: "invalid_nickname", message: "bad" },
            { status: 400 },
          )
        : (answer?.(request) ?? new Response(null, { status: 404 })),
    );
    const user = userEvent.setup();
    mount();
    const field = await nameField();
    await user.type(field, "!");
    await user.click(save() as HTMLElement);

    await waitFor(() =>
      expect(
        document.querySelector("[data-form-error-summary]"),
      ).not.toBeNull(),
    );
    expect(notifyError).not.toHaveBeenCalled();
    expect(save()).toBeVisible();
  });
});
