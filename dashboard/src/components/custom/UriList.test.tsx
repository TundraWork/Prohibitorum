import { I18nProvider } from "@lingui/react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it } from "vitest";
import { UriList, uriRowProblem } from "@/components/custom/UriList";
import { useAppForm } from "@/forms/use-app-form";
import { i18n } from "@/i18n";
import { uriListProblem } from "@/pages/admin/oidc-applications/oidc-validation";

beforeEach(() => {
  i18n.activate("en");
});

function Harness({ initial }: { initial: string[] }) {
  const form = useAppForm({
    defaultValues: { uris: initial },
    onSubmit: ({ value }) => {
      const problem = uriListProblem(value.uris);
      if (problem === undefined) return;
      form.setFieldMeta("uris", (meta) => ({
        ...meta,
        errorMap: { ...meta.errorMap, onSubmit: [uriRowProblem(problem)] },
      }));
    },
  });
  return (
    <I18nProvider i18n={i18n}>
      <form.AppForm>
        <form.Form label="addresses">
          <form.AppField name="uris">
            {() => (
              <UriList
                label="Redirect URIs"
                addLabel="Add address"
                placeholder="https://app.example.com/callback"
                minRows={1}
                minRowsReason="A client needs at least one redirect URI."
              />
            )}
          </form.AppField>
          <form.SubmitButton>Save</form.SubmitButton>
        </form.Form>
      </form.AppForm>
    </I18nProvider>
  );
}

describe("UriList", () => {
  it("names each input by the list and its row", () => {
    render(
      <Harness initial={["https://a.example/cb", "https://b.example/cb"]} />,
    );
    expect(
      screen.getByRole("textbox", { name: "Redirect URIs 1" }),
    ).toHaveValue("https://a.example/cb");
    expect(
      screen.getByRole("textbox", { name: "Redirect URIs 2" }),
    ).toHaveValue("https://b.example/cb");
  });

  it("adds an empty row and keeps what is typed in it", async () => {
    const user = userEvent.setup();
    render(<Harness initial={["https://a.example/cb"]} />);

    await user.click(screen.getByRole("button", { name: "Add address" }));
    const added = screen.getByRole("textbox", { name: "Redirect URIs 2" });
    expect(added).toHaveValue("");

    await user.type(added, "https://c.example/cb");
    expect(added).toHaveValue("https://c.example/cb");
  });

  it("removes the row whose button was pressed", async () => {
    const user = userEvent.setup();
    render(
      <Harness initial={["https://a.example/cb", "https://b.example/cb"]} />,
    );

    await user.click(screen.getByRole("button", { name: "Remove row 1" }));

    expect(
      screen.getByRole("textbox", { name: "Redirect URIs 1" }),
    ).toHaveValue("https://b.example/cb");
    expect(
      screen.queryByRole("textbox", { name: "Redirect URIs 2" }),
    ).toBeNull();
  });

  it("keeps the last required row and says why it cannot go", () => {
    render(<Harness initial={["https://a.example/cb"]} />);
    const remove = screen.getByRole("button", { name: "Remove row 1" });
    expect(remove).toBeDisabled();
    expect(remove).toHaveAccessibleDescription(
      "A client needs at least one redirect URI.",
    );
  });

  it("marks the row at fault and words the problem under it", async () => {
    const user = userEvent.setup();
    render(
      <Harness initial={["https://a.example/cb", "https://b.example/cb "]} />,
    );

    await user.click(screen.getByRole("button", { name: "Save" }));

    const bad = screen.getByRole("textbox", { name: "Redirect URIs 2" });
    expect(bad).toHaveAttribute("aria-invalid", "true");
    expect(bad).toHaveAccessibleDescription(/space/i);
    expect(
      screen.getByRole("textbox", { name: "Redirect URIs 1" }),
    ).not.toHaveAttribute("aria-invalid");
  });
});
