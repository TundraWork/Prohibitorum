import { I18nProvider } from "@lingui/react";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppForm } from "@/forms/use-app-form";
import { i18n } from "@/i18n";
import { firstOtpSlot, otpSlots } from "@/test/otp";

beforeEach(() => {
  i18n.activate("en");
});

function Harness({
  onSubmit = () => new Promise<void>(() => {}),
}: {
  onSubmit?: () => Promise<void>;
}) {
  const form = useAppForm({
    defaultValues: { name: "" },
    onSubmit,
  });
  return (
    <I18nProvider i18n={i18n}>
      <form.AppForm>
        <form.Form label="Test form">
          <form.AppField
            name="name"
            validators={{
              onBlur: ({ value }) =>
                value.length > 0 ? undefined : "Enter a name.",
            }}
          >
            {(field) => (
              <field.FormField
                label="Display name"
                description="Shown to other people."
              />
            )}
          </form.AppField>
          <form.SubmitButton>Save</form.SubmitButton>
        </form.Form>
      </form.AppForm>
    </I18nProvider>
  );
}

describe("FormField", () => {
  it("associates the label and the description with the input", () => {
    render(<Harness />);
    const input = screen.getByLabelText("Display name");
    expect(input.tagName).toBe("INPUT");
    const describedBy = input.getAttribute("aria-describedby") ?? "";
    expect(
      describedBy
        .split(" ")
        .map((id) => document.getElementById(id)?.textContent),
    ).toContain("Shown to other people.");
  });

  it("marks the input invalid and shows the error once it blurs empty", async () => {
    const user = userEvent.setup();
    render(<Harness />);
    const input = screen.getByLabelText("Display name");
    expect(input).not.toHaveAttribute("aria-invalid", "true");

    await user.click(input);
    await user.tab();

    const message = await screen.findByText("Enter a name.");
    expect(message).toBeVisible();
    expect(message.closest("[data-slot=field-error]")).not.toBeNull();
    expect(input).toHaveAttribute("aria-invalid", "true");
    const describedBy = input.getAttribute("aria-describedby") ?? "";
    expect(
      describedBy
        .split(" ")
        .map((id) => document.getElementById(id)?.textContent),
    ).toContain("Enter a name.");

    await user.type(input, "Ada");
    await user.tab();
    await waitFor(() =>
      expect(screen.queryByText("Enter a name.")).not.toBeInTheDocument(),
    );
    expect(input).not.toHaveAttribute("aria-invalid", "true");
  });

  it("is disabled while the form submits", async () => {
    const user = userEvent.setup();
    render(<Harness />);
    await user.type(screen.getByLabelText("Display name"), "Ada");
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(screen.getByLabelText("Display name")).toBeDisabled(),
    );
  });
});

function Shapes({ onValues }: { onValues: (values: unknown) => void }) {
  const form = useAppForm({
    defaultValues: { on: false, minutes: "", code: "" },
    onSubmit: ({ value }) => onValues(value),
  });
  return (
    <I18nProvider i18n={i18n}>
      <form.AppForm>
        <form.Form label="Shapes">
          <form.AppField name="on">
            {(field) => (
              <field.SwitchField
                label="Allow sign-in"
                description="When off, nobody can sign in."
              />
            )}
          </form.AppField>
          <form.AppField name="minutes">
            {(field) => <field.NumberField label="Lifetime" />}
          </form.AppField>
          <form.AppField name="code">
            {(field) => <field.OtpField label="Code" digits={6} />}
          </form.AppField>
          <form.SubmitButton>Save</form.SubmitButton>
        </form.Form>
      </form.AppForm>
    </I18nProvider>
  );
}

describe("the other field shapes", () => {
  it("toggles a switch by its label, described beside it", async () => {
    const user = userEvent.setup();
    const onValues = vi.fn();
    render(<Shapes onValues={onValues} />);
    const toggle = screen.getByRole("switch", { name: "Allow sign-in" });
    expect(toggle).toHaveAccessibleDescription("When off, nobody can sign in.");

    await user.click(screen.getByText("Allow sign-in"));
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(onValues).toHaveBeenCalledWith(
        expect.objectContaining({ on: true }),
      ),
    );
  });

  it("keeps a number as text in the form", async () => {
    const user = userEvent.setup();
    const onValues = vi.fn();
    render(<Shapes onValues={onValues} />);

    await user.type(screen.getByRole("textbox", { name: "Lifetime" }), "45");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(onValues).toHaveBeenCalledWith(
        expect.objectContaining({ minutes: "45" }),
      ),
    );
  });

  it("splits a six-digit code after the third digit and refuses letters", async () => {
    const user = userEvent.setup();
    const onValues = vi.fn();
    render(<Shapes onValues={onValues} />);
    const slots = otpSlots("Code");
    expect(slots).toHaveLength(6);
    expect(slots[2]?.nextElementSibling?.getAttribute("data-slot")).toBe(
      "separator",
    );

    await user.type(firstOtpSlot("Code"), "4a1b9c2d");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(onValues).toHaveBeenCalledWith(
        expect.objectContaining({ code: "4192" }),
      ),
    );
  });
});
