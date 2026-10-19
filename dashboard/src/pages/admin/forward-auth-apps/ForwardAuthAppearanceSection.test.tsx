import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { ForwardAuthAppearanceSection } from "@/pages/admin/forward-auth-apps/ForwardAuthAppearanceSection";
import { forwardAuthAppFixture } from "@/test/forward-auth-app";

let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  queryClient = createQueryClient(
    vi.fn<(error: unknown) => void>(),
    vi.fn<(message: MessageDescriptor) => void>(),
  );
});

afterEach(() => {
  queryClient.clear();
});

describe("the appearance section", () => {
  it("offers the icon controls under its own heading", () => {
    render(
      <I18nProvider i18n={i18n}>
        <QueryClientProvider client={queryClient}>
          <ForwardAuthAppearanceSection app={forwardAuthAppFixture()} />
        </QueryClientProvider>
      </I18nProvider>,
    );

    expect(
      screen.getByRole("heading", { level: 2, name: "Appearance" }),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Upload an icon" }),
    ).toBeVisible();
  });
});
