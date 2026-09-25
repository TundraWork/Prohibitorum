import { I18nProvider } from "@lingui/react";
import {
  QueryClient,
  QueryClientProvider,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ConsoleTabs } from "@/components/custom/ConsoleTabs";
import { i18n } from "@/i18n";

beforeEach(() => {
  i18n.activate("en");
});

function Slow({ read }: { read: () => Promise<string> }) {
  const { data } = useSuspenseQuery({ queryKey: ["slow"], queryFn: read });
  return <p>{data}</p>;
}

function Harness({ read }: { read: () => Promise<string> }) {
  const [tab, setTab] = useState<"first" | "second">("first");
  return (
    <ConsoleTabs
      label="Sections"
      selected={tab}
      onSelectionChange={setTab}
      tabs={[
        { id: "first", title: "First", panel: () => <p>First panel</p> },
        { id: "second", title: "Second", panel: () => <Slow read={read} /> },
      ]}
    />
  );
}

function mount(read: () => Promise<string>) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <Harness read={read} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("ConsoleTabs", () => {
  it("waits for a panel's data inside the panel, with the tabs still on screen", async () => {
    let resolve: (value: string) => void = () => undefined;
    mount(() => new Promise<string>((done) => (resolve = done)));
    const user = userEvent.setup();

    await user.click(screen.getByRole("tab", { name: "Second" }));
    expect(screen.getByRole("tab", { name: "Second" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(screen.getByRole("tab", { name: "First" })).toBeInTheDocument();
    expect(screen.queryByText("Loaded")).not.toBeInTheDocument();

    resolve("Loaded");
    expect(await screen.findByText("Loaded")).toBeInTheDocument();
  });

  it("reports a failed panel in its place, keeps the other tabs usable, and retries", async () => {
    vi.spyOn(console, "error").mockImplementation(() => undefined);
    const read = vi
      .fn<() => Promise<string>>()
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce("Loaded");
    mount(read);
    const user = userEvent.setup();

    await user.click(screen.getByRole("tab", { name: "Second" }));
    expect(
      await screen.findByText("This section could not be loaded"),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("tab", { name: "First" }));
    expect(await screen.findByText("First panel")).toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: "Second" }));

    await user.click(await screen.findByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.getByText("Loaded")).toBeInTheDocument());
    expect(read).toHaveBeenCalledTimes(2);
    vi.restoreAllMocks();
  });
});
