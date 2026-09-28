import { I18nProvider } from "@lingui/react";
import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { loadDocument } from "@/app/load-document";
import { ProviderButtons } from "@/components/custom/ProviderButtons";
import { i18n } from "@/i18n";

vi.mock("@/app/load-document", () => ({ loadDocument: vi.fn() }));

const providers = [
  { slug: "gitlab", displayName: "GitLab", protocol: "oidc" },
  { slug: "vrchat", displayName: "VRChat", protocol: "vrchat" },
];

function setup(list = providers) {
  return render(
    <I18nProvider i18n={i18n}>
      <ProviderButtons
        providers={list}
        href={(provider) => `/leave/${provider.slug}`}
      />
    </I18nProvider>,
  );
}

beforeEach(() => {
  i18n.activate("en");
});

afterEach(() => {
  vi.mocked(loadDocument).mockReset();
});

describe("ProviderButtons", () => {
  it("draws nothing without providers", () => {
    const { container } = setup([]);
    expect(container).toBeEmptyDOMElement();
  });

  it("leaves for the pressed provider's address, holding the others until the page has gone", async () => {
    const user = userEvent.setup();
    setup();
    const gitlab = screen.getByRole("button", { name: "Continue with GitLab" });
    const vrchat = screen.getByRole("button", { name: "Continue with VRChat" });
    await user.click(gitlab);
    expect(loadDocument).toHaveBeenCalledExactlyOnceWith("/leave/gitlab");
    expect(gitlab).toHaveAttribute("data-pending", "true");
    expect(vrchat).toBeDisabled();
    await user.click(vrchat);
    expect(loadDocument).toHaveBeenCalledOnce();
  });

  it("comes back when the page is shown again from the back-forward cache", async () => {
    const user = userEvent.setup();
    setup();
    await user.click(
      screen.getByRole("button", { name: "Continue with VRChat" }),
    );
    const gitlab = screen.getByRole("button", { name: "Continue with GitLab" });
    expect(gitlab).toBeDisabled();
    act(() => {
      window.dispatchEvent(
        new PageTransitionEvent("pageshow", { persisted: false }),
      );
    });
    expect(gitlab).toBeDisabled();
    act(() => {
      window.dispatchEvent(
        new PageTransitionEvent("pageshow", { persisted: true }),
      );
    });
    expect(gitlab).toBeEnabled();
    expect(
      screen.getByRole("button", { name: "Continue with VRChat" }),
    ).not.toHaveAttribute("data-pending", "true");
  });
});
