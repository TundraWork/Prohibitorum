import { I18nProvider } from "@lingui/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { EntityIconCard } from "@/components/custom/EntityIconCard";
import { i18n } from "@/i18n";

beforeEach(() => {
  i18n.activate("en");
});

function mount(iconUrl?: string) {
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={new QueryClient()}>
        <EntityIconCard
          target={{ kind: "oidc", appId: "grafana" }}
          iconUrl={iconUrl}
          name="Grafana"
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("EntityIconCard", () => {
  it("offers an upload and the formats when there is no icon yet", () => {
    mount();
    expect(
      screen.getByRole("button", { name: "Upload an icon" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Remove icon" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText("PNG, JPEG, WebP or GIF, up to 5 MiB."),
    ).toBeInTheDocument();
  });

  it("offers to replace or remove an icon that is there", () => {
    mount("/icons/grafana.png");
    expect(
      screen.getByRole("button", { name: "Replace icon" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Remove icon" }),
    ).toBeInTheDocument();
  });

  it("shows the first letter of the name in place of a missing icon", () => {
    mount();
    expect(screen.getByText("G")).toBeInTheDocument();
  });
});
