import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { client } from "@/api/client";
import type { components } from "@/api/generated/schema";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { AvatarPanel } from "@/pages/profile/AvatarPanel";

type MyAvatar = components["schemas"]["MyAvatarView"];

const avatarPath = "/api/prohibitorum/me/avatar";
const selectionPath = "/api/prohibitorum/me/avatar/selection";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifySuccess = vi.fn<(message: MessageDescriptor) => void>();
let queryClient: QueryClient;
/** What `GET /me/avatar` answers; a write's handler may move it. */
let avatar: MyAvatar;
/** How an upload changes `avatar`, as the server would. */
let onUpload: () => void;
/** The names of the files the upload sent. */
let uploaded: string[];

// jsdom's File cannot be the body of the test environment's Request, so the
// upload goes out as the file's text; the rest of the write runs as it is.
const send = client.PUT;
vi.spyOn(client, "PUT").mockImplementation((async (
  path: string,
  init: { body?: unknown },
) => {
  if (path === avatarPath && init.body instanceof File) {
    uploaded.push(init.body.name);
    return send(
      path as never,
      {
        ...init,
        body: await init.body.text(),
      } as never,
    );
  }
  return send(path as never, init as never);
}) as never);

function picture(source: string, label?: string) {
  return {
    source,
    url: `https://id.example/${source}.webp`,
    ...(label ? { label } : {}),
  };
}

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifySuccess.mockReset();
  avatar = {
    activeSource: "user",
    sources: [picture("user"), picture("upstream:provider-1", "Example IdP 1")],
  };
  onUpload = () => {};
  uploaded = [];
  fetchBoundary.mockImplementation(async (request) => {
    const path = new URL(request.url).pathname;
    if (request.method === "GET" && path === avatarPath) {
      return Response.json(avatar);
    }
    if (request.method === "GET" && path === "/api/prohibitorum/me") {
      return Response.json({
        id: 1,
        username: "alice",
        displayName: "Alice",
        role: "user",
      });
    }
    if (request.method === "PUT" && path === avatarPath) onUpload();
    return new Response(null, { status: 204 });
  });
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(() => undefined, notifySuccess);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

function mount() {
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <AvatarPanel />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

function requests(method: string, path: string) {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter(
      (request) =>
        request.method === method && new URL(request.url).pathname === path,
    );
}

function fileInput(): HTMLInputElement {
  const input = document.querySelector<HTMLInputElement>('input[type="file"]');
  if (input === null) throw new Error("no file input");
  return input;
}

const png = () => new File(["png"], "me.png", { type: "image/png" });

describe("the avatar gallery", () => {
  it("offers each stored picture, no picture, and an upload, with the one in use chosen", async () => {
    mount();
    const group = await screen.findByRole("radiogroup", {
      name: "Picture to show",
    });
    const radios = within(group).getAllByRole("radio");
    expect(radios.map((radio) => radio.closest("label")?.textContent)).toEqual([
      "Uploaded picture",
      "Example IdP 1",
      "No picture",
    ]);
    expect(
      within(group).getByRole("radio", { name: "Uploaded picture" }),
    ).toBeChecked();
    expect(screen.getByRole("button", { name: "Replace" })).toBeVisible();
  });

  it("saves the picture pressed and reads the gallery again", async () => {
    const user = userEvent.setup();
    mount();
    await user.click(
      await screen.findByRole("radio", { name: "Example IdP 1" }),
    );
    await waitFor(() => expect(requests("PUT", selectionPath)).toHaveLength(1));
    expect(await requests("PUT", selectionPath)[0]?.json()).toEqual({
      source: "upstream:provider-1",
    });
    await waitFor(() =>
      expect(requests("GET", avatarPath).length).toBeGreaterThan(1),
    );
    await waitFor(() =>
      expect(notifySuccess).toHaveBeenCalledWith(
        expect.objectContaining({ id: "success.avatar.updated" }),
      ),
    );
  });

  it("says the avatar changed when the upload is shown at once", async () => {
    avatar = { activeSource: "none", sources: [] };
    onUpload = () => {
      avatar = { activeSource: "user", sources: [picture("user")] };
    };
    const user = userEvent.setup();
    mount();
    expect(await screen.findByRole("button", { name: "Upload" })).toBeVisible();
    await user.upload(fileInput(), png());
    await waitFor(() =>
      expect(notifySuccess).toHaveBeenCalledWith(
        expect.objectContaining({ id: "success.avatar.updated" }),
      ),
    );
    expect(requests("PUT", selectionPath)).toHaveLength(0);
    expect(
      await screen.findByRole("radio", { name: "Uploaded picture" }),
    ).toBeChecked();
  });

  it("says the upload waits to be chosen when another picture stays in use", async () => {
    avatar = {
      activeSource: "upstream:provider-1",
      sources: [picture("upstream:provider-1", "Example IdP 1")],
    };
    onUpload = () => {
      avatar = {
        activeSource: "upstream:provider-1",
        sources: [
          picture("user"),
          picture("upstream:provider-1", "Example IdP 1"),
        ],
      };
    };
    const user = userEvent.setup();
    mount();
    await screen.findByRole("button", { name: "Upload" });
    await user.upload(fileInput(), png());
    await waitFor(() =>
      expect(notifySuccess).toHaveBeenCalledWith(
        expect.objectContaining({ id: "success.avatar.uploaded" }),
      ),
    );
    expect(
      await screen.findByRole("radio", { name: "Uploaded picture" }),
    ).not.toBeChecked();
    expect(screen.getByRole("radio", { name: "Example IdP 1" })).toBeChecked();
    expect(uploaded).toEqual(["me.png"]);
  });

  it("warns about a file it cannot take and sends nothing", async () => {
    const user = userEvent.setup({ applyAccept: false });
    mount();
    await screen.findByRole("button", { name: "Replace" });
    await user.upload(
      fileInput(),
      new File(["svg"], "me.svg", { type: "image/svg+xml" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "This file type is not supported.",
    );
    expect(uploaded).toEqual([]);
    expect(requests("PUT", avatarPath)).toHaveLength(0);
  });

  it("asks before removing the upload, then removes it", async () => {
    const user = userEvent.setup();
    mount();
    await user.click(
      await screen.findByRole("button", { name: "Remove uploaded picture" }),
    );
    const dialog = await screen.findByRole("alertdialog", {
      name: "Remove the uploaded picture?",
    });
    expect(requests("DELETE", avatarPath)).toHaveLength(0);
    await user.click(within(dialog).getByRole("button", { name: "Remove" }));
    await waitFor(() => expect(requests("DELETE", avatarPath)).toHaveLength(1));
    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
  });

  it("offers no removal without an upload", async () => {
    avatar = {
      activeSource: "upstream:provider-1",
      sources: [picture("upstream:provider-1", "Example IdP 1")],
    };
    mount();
    await screen.findByRole("button", { name: "Upload" });
    expect(
      screen.queryByRole("button", { name: "Remove uploaded picture" }),
    ).not.toBeInTheDocument();
  });
});
