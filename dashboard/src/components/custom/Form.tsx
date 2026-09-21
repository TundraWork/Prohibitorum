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
            const invalidField = element.querySelector<HTMLElement>(
              'input[aria-invalid="true"], textarea[aria-invalid="true"], select[aria-invalid="true"]',
            );
            const summary = element.querySelector<HTMLElement>(
              "[data-form-error-summary]",
            );
            (invalidField ?? summary)?.focus();
          });
        }
      }}
    >
      {children}
    </HeroUIForm>
  );
}
