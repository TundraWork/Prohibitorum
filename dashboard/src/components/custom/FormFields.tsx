import {
  Description,
  Input,
  Label,
  Switch,
  TextArea,
  TextField,
} from "@heroui/react";
import { useStore } from "@tanstack/react-form";
import { type ComponentProps, type ReactNode, useId } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";

/**
 * The handful of field shapes the management forms need beyond `FormField`:
 * a multi-line text field, a switch, and a text area whose value is a JSON
 * document rather than prose.
 *
 * They live with the other form field components rather than in the pages that
 * needed them first, so a second page gets the same label and error wiring
 * instead of drawing its own.
 */

const errorClass = "text-sm text-danger";

/** A `TextArea` wrapped like `FormField`, for values too long to sit on one line. */
export function TextAreaField({
  label,
  description,
  rows = 3,
  isDisabled = false,
  spellCheck = true,
  variant,
  className,
}: {
  label: ReactNode;
  description?: ReactNode;
  rows?: number;
  isDisabled?: boolean;
  spellCheck?: boolean;
  variant?: ComponentProps<typeof TextArea>["variant"];
  className?: string;
}) {
  const field = useFieldContext<string>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const id = useId();
  const errors: unknown[] = field.state.meta.errors.flat(Infinity);
  const invalid = errors.length > 0;

  return (
    <TextField
      isDisabled={submitting || isDisabled}
      isInvalid={invalid}
      name={field.name}
      validationBehavior="aria"
      value={field.state.value}
      onBlur={() => {
        if (!form.state.isSubmitting && !isDisabled) field.handleBlur();
      }}
      onChange={(value) => {
        if (form.state.isSubmitting || isDisabled) return;
        field.setErrorMap({
          onSubmit: withoutServerErrors(field.state.meta.errorMap.onSubmit),
        });
        field.handleChange(value);
      }}
    >
      <Label htmlFor={id}>{label}</Label>
      <TextArea
        className={className}
        id={id}
        rows={rows}
        spellCheck={spellCheck}
        variant={variant}
      />
      {description !== undefined && <Description>{description}</Description>}
      {invalid && (
        <span className={errorClass}>
          <FormMessages errors={errors} />
        </span>
      )}
    </TextField>
  );
}

/**
 * A boolean field. HeroUI's `Switch` is checked rather than typed, so the
 * errors it can carry are only the ones the server sends back for it.
 */
export function SwitchField({
  label,
  description,
  isDisabled = false,
}: {
  label: ReactNode;
  description?: ReactNode;
  isDisabled?: boolean;
}) {
  const field = useFieldContext<boolean>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const errors: unknown[] = field.state.meta.errors.flat(Infinity);
  const invalid = errors.length > 0;

  return (
    <div className="flex flex-col gap-1">
      <Switch
        isDisabled={submitting || isDisabled}
        isInvalid={invalid}
        isSelected={field.state.value}
        name={field.name}
        onChange={(selected) => {
          if (form.state.isSubmitting || isDisabled) return;
          field.setErrorMap({
            onSubmit: withoutServerErrors(field.state.meta.errorMap.onSubmit),
          });
          field.handleChange(selected);
        }}
      >
        <Switch.Content>
          <Switch.Control>
            <Switch.Thumb />
          </Switch.Control>
          {label}
        </Switch.Content>
      </Switch>
      {description !== undefined && (
        <Description className="text-xs text-muted">{description}</Description>
      )}
      {invalid && (
        <span className={errorClass}>
          <FormMessages errors={errors} />
        </span>
      )}
    </div>
  );
}

/** A single-line field whose value is a number, kept as text while editing. */
export function NumberField({
  label,
  description,
  isDisabled = false,
}: {
  label: ReactNode;
  description?: ReactNode;
  isDisabled?: boolean;
}) {
  const field = useFieldContext<string>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const id = useId();
  const errors: unknown[] = field.state.meta.errors.flat(Infinity);
  const invalid = errors.length > 0;

  return (
    <TextField
      isDisabled={submitting || isDisabled}
      isInvalid={invalid}
      name={field.name}
      validationBehavior="aria"
      value={field.state.value}
      onBlur={() => {
        if (!form.state.isSubmitting && !isDisabled) field.handleBlur();
      }}
      onChange={(value) => {
        if (form.state.isSubmitting || isDisabled) return;
        field.setErrorMap({
          onSubmit: withoutServerErrors(field.state.meta.errorMap.onSubmit),
        });
        field.handleChange(value);
      }}
    >
      <Label htmlFor={id}>{label}</Label>
      <Input id={id} inputMode="numeric" />
      {description !== undefined && <Description>{description}</Description>}
      {invalid && (
        <span className={errorClass}>
          <FormMessages errors={errors} />
        </span>
      )}
    </TextField>
  );
}

/** A read-only field, for a value the server decides. */
export function ReadOnlyField({
  label,
  value,
  description,
}: {
  label: ReactNode;
  value: string;
  description?: ReactNode;
}) {
  return (
    <TextField isReadOnly value={value}>
      <Label>{label}</Label>
      <Input readOnly />
      {description !== undefined && <Description>{description}</Description>}
    </TextField>
  );
}
