import { Form as HeroUIForm } from "@heroui/react";
import { useStore } from "@tanstack/react-form";
import { type ReactNode, useRef } from "react";
import { useFormContext } from "@/forms/context";
import { clearServerErrors } from "@/forms/server-errors";

export function Form({
  children,
  label,
  className = "flex flex-col gap-4",
}: {
  children: ReactNode;
  label: string;
  /**
   * Layout classes for the form element. A dialog that has to keep its footer
   * outside the scrolling area passes the column classes that let it fill and
   * shrink within the dialog.
   */
  className?: string;
}) {
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const pending = useRef(false);

  return (
    <HeroUIForm
      className={className}
      aria-label={label}
      aria-busy={submitting}
      validationBehavior="aria"
      onSubmit={async (event) => {
        event.preventDefault();
        event.stopPropagation();
        if (pending.current || form.state.isSubmitting) return;
        pending.current = true;
        const element = event.currentTarget;
        clearServerErrors(form);
        try {
          await form.handleSubmit();
        } finally {
          pending.current = false;
          requestAnimationFrame(() => {
            // Each invalid field in turn, stopping at the first that takes
            // focus. A browser does not focus an element under `display:
            // none`, which is what puts the caret in the visible one of the
            // attribute map's two layouts rather than the hidden twin before
            // it. Trying rather than testing visibility behaves the same in
            // jsdom, which has no `checkVisibility`.
            const invalidFields = element.querySelectorAll<HTMLElement>(
              'input[aria-invalid="true"], textarea[aria-invalid="true"], select[aria-invalid="true"]',
            );
            for (const invalidField of invalidFields) {
              invalidField.focus();
              if (document.activeElement === invalidField) return;
            }
            element
              .querySelector<HTMLElement>("[data-form-error-summary]")
              ?.focus();
          });
        }
      }}
    >
      {children}
    </HeroUIForm>
  );
}
