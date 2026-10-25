import { I18nProvider } from "@lingui/react";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/i18n";
import { ForwardAuthProxySection } from "@/pages/admin/forward-auth-apps/ForwardAuthProxySection";
import { forwardAuthSnippet } from "@/pages/admin/forward-auth-apps/forward-auth-snippet";
import { forwardAuthAppFixture } from "@/test/forward-auth-app";

const writeText = vi.fn<(text: string) => Promise<void>>();

beforeEach(() => {
  i18n.activate("en");
  writeText.mockReset();
  writeText.mockResolvedValue(undefined);
});

/** After `userEvent.setup()`, which installs a clipboard of its own. */
function stubClipboard() {
  Object.defineProperty(navigator, "clipboard", {
    value: { writeText },
    configurable: true,
  });
}

afterEach(() => {
  vi.useRealTimers();
});

function mount(host = "grafana.example.test") {
  render(
    <I18nProvider i18n={i18n}>
      <ForwardAuthProxySection
        app={forwardAuthAppFixture({ forwardAuthHost: host })}
      />
    </I18nProvider>,
  );
}

const expected = (host = "grafana.example.test") =>
  forwardAuthSnippet({ baseUrl: window.location.origin, host });

describe("the Traefik configuration", () => {
  it("shows the generated configuration for the saved hostname", () => {
    mount();

    const block = screen.getByRole("region", {
      name: "Generated Traefik configuration",
    });
    expect(block).toHaveTextContent("grafana.example.test");
    expect(block.textContent).toBe(expected());
  });

  it("links the manual and says it belongs behind a trusted proxy", () => {
    mount();

    expect(
      screen.getByText(/Use only behind a trusted reverse proxy/),
    ).toBeVisible();
    expect(
      screen.getByRole("link", { name: "the forward-auth guide" }),
    ).toHaveAttribute("href", expect.stringContaining("forward-auth.md"));
  });

  it("copies the whole configuration and says it was copied", async () => {
    const user = userEvent.setup();
    stubClipboard();
    mount();

    await user.click(
      screen.getByRole("button", { name: "Copy the Traefik configuration" }),
    );

    await waitFor(() => expect(writeText).toHaveBeenCalledWith(expected()));
    expect(await screen.findByRole("status")).toHaveTextContent("Copied");
    expect(
      screen.getByRole("button", { name: "Copy the Traefik configuration" }),
    ).toBeVisible();
    expect(screen.queryByText(/Could not copy/)).toBeNull();
  });

  it("says so when the clipboard refuses", async () => {
    const user = userEvent.setup();
    stubClipboard();
    writeText.mockRejectedValue(new Error("denied"));
    mount();

    await user.click(
      screen.getByRole("button", { name: "Copy the Traefik configuration" }),
    );

    expect(await screen.findByText(/Could not copy/)).toBeVisible();
    expect(screen.getByRole("status")).toBeEmptyDOMElement();
  });
});
