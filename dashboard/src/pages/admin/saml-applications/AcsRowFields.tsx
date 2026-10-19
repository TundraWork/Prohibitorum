import { Trans } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { type ReactNode, useRef } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import { useHostOverlayContainer } from "@/components/custom/host-overlay-container";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";
import {
  type AcsRow,
  bindingPost,
  bindingRedirect,
} from "@/pages/admin/saml-applications/saml-validation";

const bindingNames: Record<string, string> = {
  [bindingPost]: "POST",
  [bindingRedirect]: "Redirect",
};

/**
 * The four inputs of one Assertion Consumer Service endpoint, drawn inside
 * `RowsField`.
 *
 * `RowsField` owns the array and hands each row to a render callback, so the
 * row's inputs address the field by position: this component reads the field
 * from context, writes back a copy with one row replaced, and draws the message
 * the callback was given for that row. The row is the unit of edit and the unit
 * of error, which is why it is not four `AppField`s — an endpoint has no
 * identity until it is saved, and indexing the inputs would mean the count had
 * to be right before the first keystroke.
 */
export function AcsRowFields({
  index,
  error,
}: {
  index: number;
  /** This row's complaint, if the submit or the server raised one. */
  error: ReactNode;
}) {
  const field = useFieldContext<AcsRow[]>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const anchor = useRef<HTMLDivElement>(null);
  const container = useHostOverlayContainer(anchor);

  const row = field.state.value[index];
  if (row === undefined) return null;

  const invalid = error !== undefined && error !== null;

  const update = (patch: Partial<AcsRow>) => {
    if (form.state.isSubmitting) return;
    // Editing a row clears the server's complaint about the whole field, the
    // way a field's own error clears on the first keystroke.
    field.setErrorMap({
      onSubmit: withoutServerErrors(field.state.meta.errorMap.onSubmit),
    });
    field.handleChange(
      field.state.value.map((candidate, at) =>
        at === index ? { ...candidate, ...patch } : candidate,
      ),
    );
  };

  return (
    <div ref={anchor} className="flex flex-col gap-1.5">
      <div className="flex flex-wrap items-end gap-3">
        <Field className="w-36" disabled={submitting}>
          <FieldLabel>
            <Trans id="admin.saml-apps.acs.binding">Binding</Trans>
          </FieldLabel>
          <Select
            value={row.binding}
            onValueChange={(next) => {
              if (typeof next === "string") update({ binding: next });
            }}
          >
            <SelectTrigger>
              <SelectValue>
                {() => bindingNames[row.binding] ?? row.binding}
              </SelectValue>
            </SelectTrigger>
            <SelectPopup
              alignItemWithTrigger={false}
              portalProps={{ container }}
            >
              <SelectItem value={bindingPost}>POST</SelectItem>
              <SelectItem value={bindingRedirect}>Redirect</SelectItem>
            </SelectPopup>
          </Select>
        </Field>

        {/* The address is the row's identity: it is what a service provider
            publishes and what the reader compares against their own metadata,
            so it gets the row's remaining width. */}
        <Field
          className="min-w-48 flex-1"
          disabled={submitting}
          invalid={invalid}
        >
          <FieldLabel>
            <Trans id="admin.saml-apps.acs.location">Address</Trans>
          </FieldLabel>
          <Input
            className="font-mono"
            value={row.location}
            aria-invalid={invalid || undefined}
            autoComplete="off"
            spellCheck={false}
            onChange={(event) => update({ location: event.target.value })}
          />
        </Field>

        <Field className="w-20" disabled={submitting}>
          <FieldLabel>
            <Trans id="admin.saml-apps.acs.index">Index</Trans>
          </FieldLabel>
          <Input
            value={row.index}
            inputMode="numeric"
            onChange={(event) => update({ index: event.target.value })}
          />
        </Field>

        <Field className="h-9 justify-center sm:h-8" disabled={submitting}>
          <div className="flex items-center gap-2">
            <Switch
              checked={row.isDefault}
              onCheckedChange={(isDefault) => update({ isDefault })}
            />
            <FieldLabel>
              <Trans id="admin.saml-apps.acs.default">Default</Trans>
            </FieldLabel>
          </div>
        </Field>
      </div>
      {invalid && (
        <p className="text-destructive-foreground text-xs">
          <FormMessages errors={[error]} />
        </p>
      )}
    </div>
  );
}
