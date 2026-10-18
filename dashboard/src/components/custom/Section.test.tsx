import { I18nProvider } from "@lingui/react";
import { Trans } from "@lingui/react/macro";
import {
  QueryClient,
  QueryClientProvider,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { type ReactNode, useEffect, useState } from "react";
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
  sectionProps: {
    id?: string;
    footer?: string;
    dirty?: boolean;
    icon?: ReactNode;
    description?: string;
  } = {},
): ReturnType<typeof render> {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <AsyncSection resetKey="probe" title={title} {...sectionProps}>
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

  it("passes id, footer and dirty to the section drawn while loading", () => {
    vi.spyOn(console, "error").mockImplementation(() => undefined);
    const { container } = mount(
      () => new Promise<string>(() => undefined),
      "Sessions",
      {
        id: "sessions-block",
        footer: "Footer note",
        dirty: true,
      },
    );
    const section = container.querySelector("section");
    expect(section).toHaveAttribute("id", "sessions-block");
    expect(section).toHaveAttribute("data-dirty", "true");
    expect(screen.getByText("Footer note")).toBeInTheDocument();
  });

  it("passes them through the error state too", async () => {
    vi.spyOn(console, "error").mockImplementation(() => undefined);
    const { container } = mount(
      () => Promise.reject(new Error("offline")),
      "Sessions",
      { id: "sessions-block", footer: "Footer note", dirty: true },
    );
    await screen.findByText("This section could not be loaded");
    const section = container.querySelector("section");
    expect(section).toHaveAttribute("id", "sessions-block");
    expect(section).toHaveAttribute("data-dirty", "true");
    expect(screen.getByText("Footer note")).toBeInTheDocument();
    vi.restoreAllMocks();
  });

  it("draws the icon and description in the loading and error states", async () => {
    vi.spyOn(console, "error").mockImplementation(() => undefined);
    const props = {
      icon: <svg data-testid="probe-icon" />,
      description: "What this block is for.",
    };
    const { unmount } = mount(
      () => new Promise<string>(() => undefined),
      "Sessions",
      props,
    );
    expect(screen.getByText("What this block is for.")).toBeInTheDocument();
    expect(screen.getByTestId("probe-icon")).toBeInTheDocument();
    unmount();

    mount(() => Promise.reject(new Error("offline")), "Sessions", props);
    await screen.findByText("This section could not be loaded");
    expect(screen.getByText("What this block is for.")).toBeInTheDocument();
    expect(screen.getByTestId("probe-icon")).toBeInTheDocument();
    vi.restoreAllMocks();
  });

  it("draws no heading where the boundary is not a section", async () => {
    mount(async () => "Loaded");

    expect(await screen.findByText("Loaded")).toBeInTheDocument();
    expect(screen.queryByRole("heading")).not.toBeInTheDocument();
  });

  it("draws no empty heading where an untitled boundary fails", async () => {
    vi.spyOn(console, "error").mockImplementation(() => undefined);
    mount(() => Promise.reject(new Error("offline")));

    expect(
      await screen.findByText("This section could not be loaded"),
    ).toBeInTheDocument();
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

  it("draws a leading icon in the header, before the title", () => {
    const { container } = render(
      <Section title="Passkeys" icon={<svg aria-label="fingerprint" />}>
        <p>Body</p>
      </Section>,
    );

    const header = container.querySelector("[data-slot='frame-panel-header']");
    const icon = header?.querySelector("[data-slot='section-icon']");
    expect(icon).toContainElement(screen.getByLabelText("fingerprint"));
    const heading = screen.getByRole("heading", { level: 2, name: "Passkeys" });
    expect(icon?.compareDocumentPosition(heading)).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    );
  });

  it("draws no icon slot without an icon", () => {
    const { container } = render(
      <Section title="Passkeys">
        <p>Body</p>
      </Section>,
    );

    expect(container.querySelector("[data-slot='section-icon']")).toBeNull();
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

  it("defaults the anchor to a slug of a translated title's message id", () => {
    const { container } = render(
      <I18nProvider i18n={i18n}>
        <Section title={<Trans id="security.section.passkeys">Passkeys</Trans>}>
          <p>Body</p>
        </Section>
      </I18nProvider>,
    );

    expect(container.querySelector("section")).toHaveAttribute(
      "id",
      "security-section-passkeys",
    );
  });

  it("has no anchor when the title gives nothing to slug", () => {
    const { container } = render(
      <Section title={<span>Plain</span>}>
        <p>Body</p>
      </Section>,
    );

    expect(container.querySelector("section")).not.toHaveAttribute("id");
  });

  it("does not remount a child when an untitled card claims the panel", () => {
    const mounted = vi.fn();
    function Probe() {
      useEffect(() => {
        mounted();
      }, []);
      return <p>Probe</p>;
    }
    render(
      <Section title="Keep">
        <ConsoleCard>
          <Probe />
        </ConsoleCard>
        <ItemList label="Rows" empty={<Probe />} />
      </Section>,
    );

    // One mount per probe: the panel claim restyles the body in place.
    expect(mounted).toHaveBeenCalledTimes(2);
  });

  it("lets an untitled card or list be the panel, without a panel inside a panel", () => {
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

  it("keeps a titled list or card as its own frame under the section's panel", () => {
    const { container } = render(
      <Section title="Mixed">
        <ItemList label="Rows" title="Rows title" empty={<p>No rows</p>} />
        <ConsoleCard title="Card title">
          <p>Card content</p>
        </ConsoleCard>
      </Section>,
    );

    const panels = container.querySelectorAll("[data-slot='frame-panel']");
    const outer = panels[0] as Element;
    expect(outer.querySelectorAll("[data-slot='frame']")).toHaveLength(2);
    expect(outer.querySelectorAll("[data-slot='frame-panel']")).toHaveLength(2);
    expect(
      screen
        .getByRole("heading", { level: 2, name: "Rows title" })
        .closest("[data-slot='frame-panel-header']"),
    ).not.toBeNull();
  });

  it("hands the panel back to the section when a registered child goes away", async () => {
    function Toggle() {
      const [shown, setShown] = useState(true);
      return (
        <Section title="Toggle">
          {shown && <ConsoleCard>Card</ConsoleCard>}
          <button type="button" onClick={() => setShown(false)}>
            Remove
          </button>
        </Section>
      );
    }
    const { container } = render(<Toggle />);
    expect(
      container.querySelectorAll("[data-slot='frame-panel']"),
    ).toHaveLength(1);
    expect(
      container.querySelector("[data-slot='frame-panel']"),
    ).not.toContainElement(screen.getByRole("button", { name: "Remove" }));

    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "Remove" }));

    const panels = container.querySelectorAll("[data-slot='frame-panel']");
    expect(panels).toHaveLength(1);
    expect(panels[0]).toContainElement(
      screen.getByRole("button", { name: "Remove" }),
    );
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
