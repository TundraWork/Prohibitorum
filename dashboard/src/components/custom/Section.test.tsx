import { I18nProvider } from "@lingui/react";
import {
  QueryClient,
  QueryClientProvider,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AsyncSection } from "@/components/custom/Section";
import { i18n } from "@/i18n";

beforeEach(() => {
  i18n.activate("en");
});

function Slow({ read }: { read: () => Promise<string> }) {
  const { data } = useSuspenseQuery({ queryKey: ["slow"], queryFn: read });
  return <p>{data}</p>;
}

function mount(
  read: () => Promise<string>,
  title?: string,
): ReturnType<typeof render> {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <AsyncSection resetKey="probe" title={title}>
          <Slow read={read} />
        </AsyncSection>
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("AsyncSection", () => {
  it("keeps the heading on screen while the content is still loading", async () => {
    let resolve: (value: string) => void = () => undefined;
    mount(() => new Promise<string>((done) => (resolve = done)), "Sessions");

    // The heading is up on the first frame, before the read has resolved, so
    // the page does not jump down when the content arrives. Once the panel
    // draws its own `Section`, the boundary hands the heading back to it —
    // this one is here only for the wait.
    expect(
      screen.getByRole("heading", { name: "Sessions" }),
    ).toBeInTheDocument();
    await waitFor(() =>
      expect(document.querySelector("[data-slot='card']")).not.toBeNull(),
    );
    expect(
      screen.getByRole("heading", { name: "Sessions" }),
    ).toBeInTheDocument();

    resolve("Loaded");
    expect(await screen.findByText("Loaded")).toBeInTheDocument();
  });

  it("draws no heading where the boundary is not a section", async () => {
    mount(async () => "Loaded");

    expect(await screen.findByText("Loaded")).toBeInTheDocument();
    expect(screen.queryByRole("heading")).not.toBeInTheDocument();
  });

  it("keeps the heading when the read fails, and retries from it", async () => {
    vi.spyOn(console, "error").mockImplementation(() => undefined);
    const read = vi
      .fn<() => Promise<string>>()
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce("Loaded");
    mount(read, "Sessions");

    expect(
      await screen.findByText("This section could not be loaded"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Sessions" }),
    ).toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.getByText("Loaded")).toBeInTheDocument());
    expect(read).toHaveBeenCalledTimes(2);
    vi.restoreAllMocks();
  });
});
