import { I18nProvider } from "@lingui/react";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { useAppForm } from "@/forms/use-app-form";
import { i18n } from "@/i18n";
import { otpSlots } from "@/test/otp";

beforeEach(() => {
  i18n.activate("en");
});

function Harness() {
  const form = useAppForm({
    defaultValues: { code: "" },
    onSubmit: async () => {},
  });
  return (
    <I18nProvider i18n={i18n}>
      <form.AppForm>
        <form.Form label="Test form">
          <form.AppField name="code">
            {(field) => (
              <field.OtpField label="Authenticator code" digits={6} />
            )}
          </form.AppField>
        </form.Form>
      </form.AppForm>
    </I18nProvider>
  );
}

describe("OtpField slot names", () => {
  it("names the first slot by the label and the rest by position, each unique", () => {
    render(<Harness />);
    const slots = otpSlots("Authenticator code");
    expect(slots).toHaveLength(6);
    const names = ["Authenticator code"].concat(
      [2, 3, 4, 5, 6].map((n) => `Character ${n} of 6`),
    );
    names.forEach((name, i) => {
      expect(screen.getByRole("textbox", { name })).toBe(slots[i]);
    });
    expect(new Set(names).size).toBe(6);
  });
});
