import { I18nProvider } from "@lingui/react";
import {
  QueryClient,
  QueryClientProvider,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { ItemList } from "@/components/custom/ItemList";
import { AsyncSection, Section } from "@/components/custom/Section";
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
      expect(
        document.querySelector("[data-slot='frame-panel']"),
      ).not.toBeNull(),
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

describe("Section", () => {
  it("is a frame with a level-2 title, its action in the header, and a footer after the panel", () => {
    const { container } = render(
      <Section
        id="passkeys"
        title="Passkeys"
        action={<button type="button">Add</button>}
        footer={<p>Footer note</p>}
      >
        <p>Body</p>
      </Section>,
    );

    const section = container.querySelector("section");
    expect(section).toHaveAttribute("id", "passkeys");
    expect(section?.querySelector("[data-slot='frame']")).not.toBeNull();
    const heading = screen.getByRole("heading", { level: 2, name: "Passkeys" });
    const header = section?.querySelector("[data-slot='frame-panel-header']");
    expect(header).toContainElement(heading);
    expect(header).toContainElement(
      screen.getByRole("button", { name: "Add" }),
    );
    const panel = section?.querySelector("[data-slot='frame-panel']");
    expect(panel).toContainElement(screen.getByText("Body"));
    const footer = section?.querySelector("[data-slot='frame-panel-footer']");
    expect(footer).toContainElement(screen.getByText("Footer note"));
    expect(panel?.compareDocumentPosition(footer as Node)).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    );
  });

  it("defaults the anchor to a slug of a text title", () => {
    const { container } = render(
      <Section title="Sign-in page">
        <p>Body</p>
      </Section>,
    );

    expect(container.querySelector("section")).toHaveAttribute(
      "id",
      "sign-in-page",
    );
  });

  it("puts content that brings no panel of its own on one panel", () => {
    const { container } = render(
      <Section title="Raw">
        <p>Plain content</p>
      </Section>,
    );

    const panels = container.querySelectorAll("[data-slot='frame-panel']");
    expect(panels).toHaveLength(1);
    expect(panels[0]).toContainElement(screen.getByText("Plain content"));
  });

  it("lets an untitled card or a list be the section's panel instead of nesting one", () => {
    const { container } = render(
      <Section title="Mixed">
        <ConsoleCard>
          <p>Card content</p>
        </ConsoleCard>
        <ItemList label="Rows" empty={<p>No rows</p>} />
      </Section>,
    );

    expect(container.querySelectorAll("[data-slot='frame']")).toHaveLength(1);
    const panels = container.querySelectorAll("[data-slot='frame-panel']");
    expect(panels).toHaveLength(2);
    for (const panel of panels) {
      expect(
        panel.parentElement?.closest("[data-slot='frame-panel']"),
      ).toBeNull();
    }
  });

  it("draws a titled card standing alone as its own frame", () => {
    const { container } = render(
      <ConsoleCard title="Profile">
        <p>Fields</p>
      </ConsoleCard>,
    );

    expect(container.querySelectorAll("[data-slot='frame']")).toHaveLength(1);
    expect(
      screen.getByRole("heading", { level: 2, name: "Profile" }),
    ).toBeInTheDocument();
    expect(
      container.querySelectorAll("[data-slot='frame-panel']"),
    ).toHaveLength(1);
  });
});
