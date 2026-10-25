import { I18nProvider } from "@lingui/react";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import type { PrincipalSource } from "@/api/raw-admin-paths";
import { useAppForm } from "@/forms/use-app-form";
import { i18n } from "@/i18n";

beforeEach(() => {
  i18n.activate("en");
});

function Harness({ initial }: { initial: PrincipalSource }) {
  const form = useAppForm({ defaultValues: { source: initial } });
  return (
    <I18nProvider i18n={i18n}>
      <form.AppForm>
        <form.AppField name="source">
          {(field) => <field.PrincipalSourceField label="Subject source" />}
        </form.AppField>
      </form.AppForm>
    </I18nProvider>
  );
}

describe("PrincipalSourceField", () => {
  it("names the field and shows the chosen source with what it costs", () => {
    render(<Harness initial="username" />);
    expect(
      screen.getByRole("combobox", { name: "Subject source" }),
    ).toHaveTextContent("Username");
    expect(
      screen.getByText("Human-readable. An administrator can change it."),
    ).toBeInTheDocument();
  });
});
