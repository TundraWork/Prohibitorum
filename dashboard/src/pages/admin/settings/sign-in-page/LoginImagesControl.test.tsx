import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/api/errors";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { LoginImagesControl } from "@/pages/admin/settings/sign-in-page/LoginImagesControl";
import { apiError, fakeApi } from "@/test/app";

// jsdom's File cannot be the body of the test environment's Request, so the
// upload stops short of the network and only counts what it was given.
const upload = vi.hoisted(() => ({
  files: [] as string[],
  fail: undefined as undefined | ((name: string) => Error | undefined),
}));
vi.mock("@/api/mutations", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/mutations")>();
  return {
    ...actual,
    uploadLoginImageMutationOptions: (
      ...args: Parameters<typeof actual.uploadLoginImageMutationOptions>
    ) => ({
      ...actual.uploadLoginImageMutationOptions(...args),
      mutationFn: async (file: File) => {
        upload.files.push(file.name);
        const failure = upload.fail?.(file.name);
        if (failure) throw failure;
        return { id: 100 + upload.files.length, url: "/x", etag: "x" };
      },
    }),
  };
});

let queryClient: QueryClient;
let notifyError: ReturnType<typeof vi.fn<(error: unknown) => void>>;

beforeEach(() => {
  i18n.activate("en");
  upload.files.length = 0;
  upload.fail = undefined;
  notifyError = vi.fn<(error: unknown) => void>();
  queryClient = createQueryClient(notifyError);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

function images(count: number) {
  return Array.from({ length: count }, (_, i) => ({
    id: i + 1,
    url: `/branding/login-images/${i + 1}`,
  }));
}

function mount(count: number) {
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <LoginImagesControl images={images(count)} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

function fileInput() {
  const input = document.querySelector<HTMLInputElement>('input[type="file"]');
  if (!input) throw new Error("no file input");
  return input;
}

const png = (name: string) =>
  new File(["x"], `${name}.png`, { type: "image/png" });

describe("the uploaded sign-in images", () => {
  it("asks for the first image with a drop zone, and says what is accepted", () => {
    mount(0);
    expect(
      screen.getByRole("group", { name: "Drop images here, or choose files" }),
    ).toBeVisible();
    expect(
      screen.getByText("PNG, JPEG or WebP, up to 5 MiB each · 0 of 10"),
    ).toBeVisible();
  });

  it("says that adding and removing take effect at once", () => {
    mount(2);
    expect(
      screen.getByText(
        "Adding or removing an image takes effect right away, without Save.",
      ),
    ).toBeVisible();
  });

  it("lists the images in upload order with a removal for each, and a tile to add more", () => {
    mount(2);
    const list = screen.getByRole("list", { name: "Images" });
    expect(list.querySelectorAll("img")).toHaveLength(2);
    expect(
      screen.getByRole("button", { name: "Remove image 1" }),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Remove image 2" }),
    ).toBeVisible();
    expect(
      screen.getByRole("group", { name: "Drop images here, or choose files" }),
    ).toBeVisible();
    expect(
      screen.getByText("PNG, JPEG or WebP, up to 5 MiB each · 2 of 10"),
    ).toBeVisible();
  });

  it("stops offering to add images at ten", () => {
    mount(10);
    expect(
      screen.queryByRole("group", {
        name: "Drop images here, or choose files",
      }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText("10 of 10 · remove one to add another"),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Remove image 10" }),
    ).toBeVisible();
  });

  it("removes an image at once", async () => {
    const user = userEvent.setup();
    const api = fakeApi({
      "DELETE /api/prohibitorum/admin/settings/login-images/2": () =>
        new Response(null, { status: 204 }),
    });
    mount(3);
    await user.click(screen.getByRole("button", { name: "Remove image 2" }));
    await waitFor(() =>
      expect(
        api.sent("DELETE", "/api/prohibitorum/admin/settings/login-images/2"),
      ).toHaveLength(1),
    );
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("reports one failed removal once", async () => {
    const user = userEvent.setup();
    fakeApi({
      "DELETE /api/prohibitorum/admin/settings/login-images/1": () =>
        apiError("internal_error", 500),
    });
    mount(1);
    await user.click(screen.getByRole("button", { name: "Remove image 1" }));
    await waitFor(() => expect(notifyError).toHaveBeenCalledTimes(1));
  });

  it("uploads only as many chosen files as there is room for, and says how many were left", async () => {
    const user = userEvent.setup();
    mount(8);
    await user.upload(fileInput(), ["a", "b", "c", "d"].map(png));

    // The first two, in the order they were chosen.
    await waitFor(() => expect(upload.files).toEqual(["a.png", "b.png"]));
    expect(
      await screen.findByText(
        "2 files weren't uploaded because the 10-image limit was reached.",
      ),
    ).toBeInTheDocument();
  });

  it("says which files were left out for their size or type", async () => {
    const user = userEvent.setup({ applyAccept: false });
    mount(0);
    const big = new File(["x"], "big.png", { type: "image/png" });
    Object.defineProperty(big, "size", { value: 6 * 1024 * 1024 });
    const gif = new File(["x"], "anim.gif", { type: "image/gif" });
    await user.upload(fileInput(), [big, gif, png("ok")]);

    await waitFor(() => expect(upload.files).toEqual(["ok.png"]));
    expect(
      await screen.findByText(
        "1 file wasn't uploaded because it is larger than 5 MiB.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "1 file wasn't uploaded because it isn't PNG, JPEG or WebP.",
      ),
    ).toBeInTheDocument();
  });

  it("reports a failed upload once and carries on with the next file", async () => {
    const user = userEvent.setup();
    upload.fail = (name) =>
      name === "a.png"
        ? new ApiError({ kind: "http", status: 500, code: "internal_error" })
        : undefined;
    mount(0);
    await user.upload(fileInput(), [png("a"), png("b")]);
    await waitFor(() => expect(upload.files).toEqual(["a.png", "b.png"]));
    expect(notifyError).toHaveBeenCalledTimes(1);
  });

  it("stops at the first refusal for a full list", async () => {
    const user = userEvent.setup();
    upload.fail = () =>
      new ApiError({ kind: "http", status: 409, code: "login_images_full" });
    mount(0);
    await user.upload(fileInput(), [png("a"), png("b"), png("c")]);
    await waitFor(() => expect(upload.files).toEqual(["a.png"]));
    expect(notifyError).toHaveBeenCalledTimes(1);
  });
});
