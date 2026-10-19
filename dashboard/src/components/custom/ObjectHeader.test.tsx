import { I18nProvider } from "@lingui/react";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ObjectHeader } from "@/components/custom/ObjectHeader";
import { i18n } from "@/i18n";

beforeEach(() => i18n.activate("en"));

describe("ObjectHeader", () => {
  it("renders the title as the only h1, with statuses in order", () => {
    render(
      <I18nProvider i18n={i18n}>
        <ObjectHeader
          icon={<span>icon</span>}
          title="GitHub"
          statuses={["enabled", "sign-in-on"]}
          facts={["OIDC", "18 linked accounts"]}
          actions={<button type="button">Test</button>}
        />
      </I18nProvider>,
    );

    const headings = screen.getAllByRole("heading", { level: 1 });
    expect(headings).toHaveLength(1);
    expect(headings[0]?.textContent).toBe("GitHub");

    const statuses = screen.getByRole("list", { name: "Status" });
    expect(
      within(statuses)
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual(["Enabled", "Sign-in on"]);
    expect(screen.getByText("18 linked accounts")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Test" })).toBeTruthy();
  });

  it("names a provider that is not ready in the warning style", () => {
    render(
      <I18nProvider i18n={i18n}>
        <ObjectHeader
          icon={null}
          title="Steam"
          statuses={["disabled", "sign-in-off", "not-ready"]}
        />
      </I18nProvider>,
    );

    const statuses = screen.getByRole("list", { name: "Status" });
    expect(
      within(statuses)
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual(["Disabled", "Sign-in off", "Not ready"]);
  });

  it("draws no separator characters that a wrapped line could end on", () => {
    const { container } = render(
      <I18nProvider i18n={i18n}>
        <ObjectHeader
          icon={null}
          title="GitHub"
          statuses={["enabled"]}
          identifier={{ value: "github", label: "Copy the slug" }}
          facts={["OIDC"]}
        />
      </I18nProvider>,
    );

    expect(container.textContent).not.toContain("|");
  });

  const renderCopyable = () =>
    render(
      <I18nProvider i18n={i18n}>
        <ObjectHeader
          icon={null}
          title="GitHub"
          identifier={{ value: "github", label: "Copy the slug" }}
        />
      </I18nProvider>,
    );

  it("lets an address wrap after its slashes without changing its text", () => {
    const url = "https://saml.example.test/idp/metadata";
    const { container } = render(
      <I18nProvider i18n={i18n}>
        <ObjectHeader
          icon={<span>icon</span>}
          title="Portal"
          identifier={{ value: url, label: "Copy the entity ID" }}
        />
      </I18nProvider>,
    );

    expect(screen.getByText(url)).toBeInTheDocument();
    // A break point after the scheme and after each path segment, none inside one.
    expect(container.querySelectorAll("wbr")).toHaveLength(3);
  });

  it("copies the identifier value and confirms with Copied", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    const user = userEvent.setup();
    // After setup(), which installs its own clipboard stub.
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    renderCopyable();

    expect(screen.getByText("github")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Copy the slug" }));
    expect(writeText).toHaveBeenCalledWith("github");
    expect((await screen.findByRole("status")).textContent).toBe("Copied");
  });

  it("shows the failure message when the clipboard refuses", async () => {
    const writeText = vi.fn().mockRejectedValue(new Error("denied"));
    const user = userEvent.setup();
    // After setup(), which installs its own clipboard stub.
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    renderCopyable();

    await user.click(screen.getByRole("button", { name: "Copy the slug" }));
    expect(
      await screen.findByText(
        "Couldn't copy. Select the value above and copy it yourself.",
      ),
    ).toBeTruthy();
    expect(screen.getByRole("status").textContent).toBe("");
  });
});
