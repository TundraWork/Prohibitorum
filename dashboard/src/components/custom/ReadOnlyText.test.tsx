import { I18nProvider } from "@lingui/react";
import { render, screen } from "@testing-library/react";
import { beforeEach, expect, it } from "vitest";
import { ReadOnlyText } from "@/components/custom/ReadOnlyText";
import { i18n } from "@/i18n";

beforeEach(() => {
  i18n.activate("en");
});

it("caps the textarea's own height at maxRows lines and types it in monospace", () => {
  render(
    <I18nProvider i18n={i18n}>
      <ReadOnlyText label="Body" value={"a\n".repeat(30)} maxRows={12} />
    </I18nProvider>,
  );
  const field = screen.getByRole("textbox", { name: "Body" });
  expect(field.tagName).toBe("TEXTAREA");
  expect(field.style.maxBlockSize).toBe("12lh");
  expect(field.parentElement).toHaveClass("*:font-mono", "*:overflow-y-auto");
});
