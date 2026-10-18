import { I18nProvider } from "@lingui/react";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, expect, it } from "vitest";
import { OrSeparator } from "@/components/custom/OrSeparator";
import { i18n } from "@/i18n";

beforeEach(() => i18n.activate("en"));
const draw = (node: ReactNode) =>
  render(<I18nProvider i18n={i18n}>{node}</I18nProvider>);

it("says or by default", () => {
  draw(<OrSeparator />);
  expect(screen.getByText("or")).toBeVisible();
});

it("shows a custom label", () => {
  draw(<OrSeparator label="or with your password" />);
  expect(screen.getByText("or with your password")).toBeVisible();
});
