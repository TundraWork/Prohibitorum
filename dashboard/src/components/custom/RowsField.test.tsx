import { msg } from "@lingui/core/macro";
import { I18nProvider } from "@lingui/react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { RowsField, rowProblem } from "@/components/custom/RowsField";
import { useAppForm } from "@/forms/use-app-form";
import { i18n } from "@/i18n";

/**
 * A row that draws two inputs, so a complaint about one of them can be told
 * apart from a complaint about the other.
 *
 * `RowsField` hands each row the raw problem as well as its wording, which is
 * what lets a row with several inputs mark only the one at fault. That is the
 * behaviour the console's attribute map, claim aliases and ACS endpoints all
 * rely on, so it is worth its own test rather than being visible only through
 * whichever page happens to draw a row first.
 */
function TwoInputRow({ index, problem }: { index: number; problem?: Problem }) {
  return (
    <div>
      <input
        aria-label={`first-${index}`}
        aria-invalid={problem?.field === "first"}
        readOnly
      />
      <input
        aria-label={`second-${index}`}
        aria-invalid={problem?.field === "second"}
        readOnly
      />
    </div>
  );
}

type Problem = { field: string } | undefined;

const required = msg({
  id: "test.rows.required",
  message: "Give this row a value.",
});

function Harness() {
  const form = useAppForm({
    defaultValues: { rows: [{ a: "" }, { b: "" }] },
    onSubmit: ({ value }) => {
      form.setFieldMeta("rows", (meta) => ({
        ...meta,
        errorMap: {
          ...meta.errorMap,
          onSubmit: [rowProblem(1, { line: 2, reason: required }, "second")],
        },
      }));
      void value;
    },
  });

  return (
    <I18nProvider i18n={i18n}>
      <form.AppForm>
        <form.Form label="rows">
          <form.AppField name="rows">
            {() => (
              <RowsField<{ a: string }>
                label="Rows"
                renderRow={(_row, index, error, problem) => (
                  <>
                    <TwoInputRow index={index} problem={problem} />
                    <span>{error}</span>
                  </>
                )}
                emptyRow={() => ({ a: "" })}
                addLabel="Add"
              />
            )}
          </form.AppField>
          <form.SubmitButton>Save</form.SubmitButton>
        </form.Form>
      </form.AppForm>
    </I18nProvider>
  );
}

describe("RowsField row problems", () => {
  it("marks only the input the problem names", async () => {
    const user = userEvent.setup();
    i18n.activate("en");
    render(<Harness />);

    await user.click(screen.getByRole("button", { name: "Save" }));

    // The second row's second input is the one at fault, so the first row and
    // the second row's first input stay unmarked.
    expect(screen.getByLabelText("second-1")).toHaveAttribute(
      "aria-invalid",
      "true",
    );
    expect(screen.getByLabelText("first-1")).toHaveAttribute(
      "aria-invalid",
      "false",
    );
    expect(screen.getByLabelText("first-0")).toHaveAttribute(
      "aria-invalid",
      "false",
    );
    expect(screen.getByLabelText("second-0")).toHaveAttribute(
      "aria-invalid",
      "false",
    );
  });

  it("words the row's own message under the row", async () => {
    const user = userEvent.setup();
    i18n.activate("en");
    render(<Harness />);

    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(screen.getByText("Give this row a value.")).toBeTruthy();
  });
});
