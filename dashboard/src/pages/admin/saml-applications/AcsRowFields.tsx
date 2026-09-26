import { Input, Label, ListBox, Select, Switch } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { type ReactNode, useId } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";
import {
  type AcsRow,
  bindingPost,
  bindingRedirect,
} from "@/pages/admin/saml-applications/saml-validation";

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
  const { t } = useLingui();
  const field = useFieldContext<AcsRow[]>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const locationId = useId();
  const indexId = useId();

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
    <div className="flex flex-col gap-1.5">
      <div className="flex flex-wrap items-end gap-3">
        <Select
          className="w-32"
          variant="secondary"
          isDisabled={submitting}
          value={row.binding}
          onChange={(key) => {
            if (typeof key === "string") update({ binding: key });
          }}
        >
          <Label>
            <Trans id="admin.saml-apps.acs.binding">Binding</Trans>
          </Label>
          <Select.Trigger>
            <Select.Value />
            <Select.Indicator />
          </Select.Trigger>
          <Select.Popover>
            <ListBox>
              <ListBox.Item id={bindingPost} textValue="POST">
                POST
                <ListBox.ItemIndicator />
              </ListBox.Item>
              <ListBox.Item id={bindingRedirect} textValue="Redirect">
                Redirect
                <ListBox.ItemIndicator />
              </ListBox.Item>
            </ListBox>
          </Select.Popover>
        </Select>

        {/* The address is the row's identity: it is what a service provider
            publishes and what the reader compares against their own metadata,
            so it gets the row's remaining width. */}
        <div className="flex min-w-48 flex-1 flex-col gap-1">
          <Label htmlFor={locationId}>
            <Trans id="admin.saml-apps.acs.location">Address</Trans>
          </Label>
          <Input
            id={locationId}
            value={row.location}
            disabled={submitting}
            aria-invalid={invalid}
            autoComplete="off"
            spellCheck={false}
            placeholder={t({
              id: "admin.saml-apps.acs.location.placeholder",
              message: "https://sp.example.com/saml/acs",
            })}
            variant="secondary"
            onChange={(event) => update({ location: event.target.value })}
          />
        </div>

        <div className="flex w-20 flex-col gap-1">
          <Label htmlFor={indexId}>
            <Trans id="admin.saml-apps.acs.index">Index</Trans>
          </Label>
          <Input
            id={indexId}
            value={row.index}
            disabled={submitting}
            inputMode="numeric"
            variant="secondary"
            onChange={(event) => update({ index: event.target.value })}
          />
        </div>

        <div className="pb-2.5">
          <Switch
            isSelected={row.isDefault}
            isDisabled={submitting}
            onChange={(selected) => update({ isDefault: selected })}
          >
            <Switch.Content>
              <Switch.Control>
                <Switch.Thumb />
              </Switch.Control>
              <Trans id="admin.saml-apps.acs.default">Default</Trans>
            </Switch.Content>
          </Switch>
        </div>
      </div>
      {error !== undefined && error !== null && (
        <span className="text-sm text-danger">
          <FormMessages errors={[error]} />
        </span>
      )}
    </div>
  );
}
