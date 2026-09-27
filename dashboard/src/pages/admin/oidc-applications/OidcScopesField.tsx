import { Checkbox, CheckboxGroup, Description, Label } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useId } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";
import {
  type OidcScope,
  oidcScopes,
} from "@/pages/admin/oidc-applications/oidc-validation";

/**
 * The scopes a client may request, as a field of scope names.
 *
 * The instance knows a closed set, so every one is drawn with a line saying
 * what it gives the client, and the reader chooses rather than types. The name
 * is monospace because it is the string the client sends. `openid` is read-only
 * and always held: a request without it is refused, so offering to leave it
 * out would only offer a client that cannot sign in. It is read-only rather
 * than disabled, which would fade it into looking unavailable.
 *
 * The value keeps the vocabulary's order whatever order the boxes were ticked
 * in, so a saved record reads the same way the list does.
 */
export function OidcScopesField() {
  const { i18n } = useLingui();
  const field = useFieldContext<OidcScope[]>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const errors: unknown[] = field.state.meta.errors.flat(Infinity);
  const invalid = errors.length > 0;

  return (
    <CheckboxGroup
      variant="secondary"
      name={field.name}
      value={field.state.value}
      isDisabled={submitting}
      isInvalid={invalid}
      onChange={(next) => {
        if (form.state.isSubmitting) return;
        field.setErrorMap({
          onSubmit: withoutServerErrors(field.state.meta.errorMap.onSubmit),
        });
        const chosen = new Set(next);
        field.handleChange(
          oidcScopes
            .map((scope) => scope.value)
            .filter((scope) => scope === "openid" || chosen.has(scope)),
        );
      }}
    >
      <Label>
        <Trans id="admin.oidc-apps.field.scopes">Scopes</Trans>
      </Label>
      {oidcScopes.map((scope) => (
        <ScopeCheckbox
          key={scope.value}
          value={scope.value}
          description={i18n._(scope.description)}
        />
      ))}
      {invalid && (
        <span className="text-sm text-danger">
          <FormMessages errors={errors} />
        </span>
      )}
    </CheckboxGroup>
  );
}

/**
 * One scope. The description sits inside the label, stacked under the name as
 * in HeroUI's add-ons demo, and the label spans the row, so the whole row
 * toggles the box rather than only the square and the name. The checkbox is
 * named by the scope alone; the line under it stays its description rather
 * than becoming part of its name.
 */
function ScopeCheckbox({
  value,
  description,
}: {
  value: OidcScope;
  description: string;
}) {
  const nameId = useId();
  return (
    <Checkbox
      className="w-full"
      value={value}
      aria-labelledby={nameId}
      isReadOnly={value === "openid"}
    >
      <Checkbox.Content className="w-full items-start">
        <Checkbox.Control className="mt-0.5">
          <Checkbox.Indicator />
        </Checkbox.Control>
        <div className="flex min-w-0 flex-col">
          <span id={nameId} className="font-mono">
            {value}
          </span>
          <Description>{description}</Description>
        </div>
      </Checkbox.Content>
    </Checkbox>
  );
}
