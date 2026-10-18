import { I18nProvider } from "@lingui/react";
import { act, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { loadDocument } from "@/app/load-document";
import { ProviderGrid } from "@/components/custom/ProviderGrid";
import { i18n } from "@/i18n";

vi.mock("@/app/load-document", () => ({ loadDocument: vi.fn() }));

const names = [
  "Discord",
  "GitHub",
  "GitLab",
  "Google",
  "Microsoft Entra",
  "Steam",
  "Twitch",
  "University SSO",
  "Zulip",
];

function make(count: number) {
  return names.slice(0, count).map((displayName) => ({
    slug: displayName.toLowerCase().replace(/\s+/g, "-"),
    displayName,
    protocol: "oidc",
    allowSignIn: true,
  }));
}

function setup(
  list = make(2),
  props: { disabled?: boolean; onLeavingChange?: (v: boolean) => void } = {},
) {
  return render(
    <I18nProvider i18n={i18n}>
      <ProviderGrid
        providers={list}
        href={(provider) => `/leave/${provider.slug}`}
        {...props}
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

describe("ProviderGrid", () => {
  it("draws nothing without providers", () => {
    const { container } = setup([]);
    expect(container).toBeEmptyDOMElement();
  });

  it("offers a single provider as one button", () => {
    setup(make(3).slice(2));
    expect(screen.getAllByRole("button")).toHaveLength(1);
    expect(
      screen.getByRole("button", { name: "Continue with GitLab" }),
    ).toBeInTheDocument();
  });

  it("lays four providers out in a grid of named buttons", () => {
    setup(make(4));
    const buttons = screen.getAllByRole("button");
    expect(buttons).toHaveLength(4);
    expect(buttons[0]?.parentElement).toHaveClass("grid", "grid-cols-2");
    for (const name of names.slice(0, 4)) {
      expect(
        screen.getByRole("button", { name: `Continue with ${name}` }),
      ).toBeInTheDocument();
    }
  });

  it("uses the initials of a provider's name as its tile", () => {
    setup([
      ...make(1),
      {
        slug: "uni",
        displayName: "University SSO",
        protocol: "oidc",
        allowSignIn: true,
      },
    ]);
    expect(screen.getByText("US")).toBeInTheDocument();
  });

  it("keeps five providers and folds the rest into a searchable popover", async () => {
    const user = userEvent.setup();
    setup(make(9));
    expect(screen.getAllByRole("button")).toHaveLength(6);
    const more = screen.getByRole("button", { name: /More providers/ });
    expect(more).toHaveTextContent("+4");

    await user.click(more);
    const popup = await screen.findByRole("dialog");
    for (const name of names.slice(5)) {
      expect(within(popup).getByRole("button", { name })).toBeInTheDocument();
    }

    await user.type(
      within(popup).getByPlaceholderText("Find a provider"),
      "ste",
    );
    expect(
      within(popup).getByRole("button", { name: "Steam" }),
    ).toBeInTheDocument();
    expect(
      within(popup).queryByRole("button", { name: "Twitch" }),
    ).not.toBeInTheDocument();

    await user.clear(within(popup).getByPlaceholderText("Find a provider"));
    await user.type(
      within(popup).getByPlaceholderText("Find a provider"),
      "zzz",
    );
    expect(within(popup).getByText("No provider matches")).toBeInTheDocument();
  });

  it("leaves from the popover too", async () => {
    const user = userEvent.setup();
    setup(make(9));
    await user.click(screen.getByRole("button", { name: /More providers/ }));
    const popup = await screen.findByRole("dialog");
    await user.click(within(popup).getByRole("button", { name: "Zulip" }));
    expect(loadDocument).toHaveBeenCalledExactlyOnceWith("/leave/zulip");
  });

  it("leaves for the pressed provider, holding every other control", async () => {
    const user = userEvent.setup();
    const onLeavingChange = vi.fn();
    setup(make(9), { onLeavingChange });
    const github = screen.getByRole("button", { name: "Continue with GitHub" });
    await user.click(github);
    expect(loadDocument).toHaveBeenCalledExactlyOnceWith("/leave/github");
    expect(onLeavingChange).toHaveBeenCalledWith(true);
    expect(github).toHaveAttribute("aria-busy", "true");
    expect(
      screen.getByRole("button", { name: "Continue with Discord" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: /More providers/ }),
    ).toBeDisabled();
    await user.click(
      screen.getByRole("button", { name: "Continue with Discord" }),
    );
    expect(loadDocument).toHaveBeenCalledOnce();
  });

  it("comes back when the page is shown again from the back-forward cache", async () => {
    const user = userEvent.setup();
    const onLeavingChange = vi.fn();
    setup(make(2), { onLeavingChange });
    await user.click(
      screen.getByRole("button", { name: "Continue with GitHub" }),
    );
    const discord = screen.getByRole("button", {
      name: "Continue with Discord",
    });
    expect(discord).toBeDisabled();
    act(() => {
      window.dispatchEvent(
        new PageTransitionEvent("pageshow", { persisted: false }),
      );
    });
    expect(discord).toBeDisabled();
    act(() => {
      window.dispatchEvent(
        new PageTransitionEvent("pageshow", { persisted: true }),
      );
    });
    expect(discord).toBeEnabled();
    expect(
      screen.getByRole("button", { name: "Continue with GitHub" }),
    ).not.toHaveAttribute("aria-busy", "true");
    expect(onLeavingChange).toHaveBeenLastCalledWith(false);
  });

  it("disables everything when told to", () => {
    setup(make(9), { disabled: true });
    for (const button of screen.getAllByRole("button")) {
      expect(button).toBeDisabled();
    }
  });
});
