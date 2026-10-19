import { I18nProvider } from "@lingui/react";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { useAppForm } from "@/forms/use-app-form";
import { i18n } from "@/i18n";
import { ProviderChoiceField } from "@/pages/admin/identity-providers/ProviderChoiceField";
import type { ProviderOption } from "@/pages/admin/identity-providers/provider-options";

beforeEach(() => {
  i18n.activate("en");
});

const options: ProviderOption<"a" | "b">[] = [
  {
    value: "a",
    label: { id: "test.choice.a", message: "Alpha" },
    description: { id: "test.choice.a.desc", message: "The first" },
  },
  {
    value: "b",
    label: { id: "test.choice.b", message: "Beta" },
    description: { id: "test.choice.b.desc", message: "The second" },
  },
];

function Harness() {
  const form = useAppForm({ defaultValues: { choice: "b" as "a" | "b" } });
  return (
    <form.AppForm>
      <form.AppField
        name="choice"
        validators={{ onMount: () => "Pick the first one" }}
      >
        {() => (
          <ProviderChoiceField<"a" | "b">
            label="Choice"
            options={options}
            description="About the choice"
            unavailable={{ values: ["b"], reason: "Beta is unavailable" }}
          />
        )}
      </form.AppField>
    </form.AppForm>
  );
}

describe("ProviderChoiceField", () => {
  it("describes the group by its description, its reason and its error", async () => {
    render(
      <I18nProvider i18n={i18n}>
        <Harness />
      </I18nProvider>,
    );
    const group = await screen.findByRole("radiogroup", { name: "Choice" });
    const description = group.getAttribute("aria-describedby") ?? "";
    expect(description.split(" ")).toHaveLength(3);
    expect(group).toHaveAccessibleDescription(
      expect.stringContaining("Pick the first one"),
    );
    expect(group).toHaveAccessibleDescription(
      expect.stringContaining("Beta is unavailable"),
    );
  });
});
