import type { ReactNode } from "react";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Textarea } from "@/components/ui/textarea";

/**
 * Text to read and copy from, in a monospace box that wraps long unbroken
 * values and scrolls past `maxRows` lines. For a secret, a response body or
 * anything the reader selects rather than edits.
 */
export function ReadOnlyText({
  label,
  description,
  value,
  maxRows = 12,
}: {
  label: ReactNode;
  description?: ReactNode;
  value: string;
  maxRows?: number;
}) {
  return (
    <Field>
      <FieldLabel>{label}</FieldLabel>
      <Textarea
        readOnly
        value={value}
        style={
          Number.isFinite(maxRows)
            ? { maxBlockSize: `${maxRows}lh` }
            : undefined
        }
        autoComplete="off"
        spellCheck={false}
        // coss sizes the box to its content (field-sizing), so `rows` does nothing;
        // the cap is a max height, and the type applies to the textarea itself.
        className="*:min-h-0 *:overflow-y-auto *:resize-none *:font-mono *:text-xs *:break-all *:text-balance max-sm:*:min-h-0"
      />
      {description !== undefined && (
        <FieldDescription>{description}</FieldDescription>
      )}
    </Field>
  );
}
