import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AppStrip } from "@/components/custom/AppStrip";

// Base UI mounts the image only once it has loaded, which jsdom never does.
function loadImagesInstantly() {
  vi.stubGlobal(
    "Image",
    class {
      onload: (() => void) | null = null;
      onerror: (() => void) | null = null;
      referrerPolicy = "";
      crossOrigin: string | null = null;
      set src(_: string) {
        queueMicrotask(() => this.onload?.());
      }
    },
  );
}

describe("AppStrip", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("draws the icon image once it has loaded", async () => {
    loadImagesInstantly();
    render(<AppStrip lead="Lead" name="Wiki" iconUrl="/icon.png" />);
    await waitFor(() =>
      expect(
        document.querySelector('[data-slot="avatar-image"]'),
      ).toHaveAttribute("src", "/icon.png"),
    );
  });

  it("draws the lead, the name and the host", () => {
    render(<AppStrip lead="Asking for access" name="Wiki" host="wiki.test" />);
    expect(screen.getByText("Asking for access")).toBeInTheDocument();
    expect(screen.getByText("Wiki")).toBeInTheDocument();
    expect(screen.getByText("wiki.test")).toBeInTheDocument();
  });

  it("leaves the host out when there is none", () => {
    render(<AppStrip lead="Lead" name="Wiki" />);
    expect(screen.queryByText("wiki.test")).not.toBeInTheDocument();
  });

  it("shows the initials of the name without an icon", () => {
    render(<AppStrip lead="Lead" name="Northlight Wiki" />);
    expect(screen.getByText("NW")).toBeInTheDocument();
    expect(
      document.querySelector('[data-slot="avatar-image"]'),
    ).not.toBeInTheDocument();
  });

  it("keeps the whole name as text and title when it is long", () => {
    const name =
      "A very long application name that will be truncated on screen";
    render(<AppStrip lead="Lead" name={name} iconUrl="/icon.png" />);
    expect(screen.getByText(name)).toHaveAttribute("title", name);
  });
});
