import { Checkbox, CheckboxGroup, Description, Label } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useQuery } from "@tanstack/react-query";
import { useId } from "react";
import { federationProvidersQueryOptions } from "@/api/queries";
import { FormMessages } from "@/components/custom/FormMessages";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";
import {
  type AllowedScope,
  identityScope,
  isIdentityScope,
  isIdentityScopeSlug,
  oidcScopes,
} from "@/pages/admin/oidc-applications/oidc-validation";

const identityDescription = msg({
  id: "admin.oidc-apps.scope.identity",
  message: "The user's linked {provider} identities.",
});

const unavailableIdentityDescription = msg({
  id: "admin.oidc-apps.scope.identity.unavailable",
  message:
    "This provider is turned off or no longer exists. Clear it to stop allowing it.",
});

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
 * Under them, one `identity:<slug>` box per enabled provider lets the client
 * read the user's linked identities there. The list comes from the providers
 * the instance offers, which application managers can read as well as
 * administrators. A saved identity scope whose provider is no longer offered
 * stays drawn, so it can be cleared.
 *
 * The value keeps the vocabulary's order whatever order the boxes were ticked
 * in, so a saved record reads the same way the list does.
 */
export function OidcScopesField() {
  const { i18n } = useLingui();
  const field = useFieldContext<AllowedScope[]>();
  const providers = useQuery(federationProvidersQueryOptions());
  // Until the provider list is in, the identity scopes are neither drawn nor
  // touched: drawing them would call every saved one unavailable, and a change
  // to another box must not drop them.
  const loaded = providers.isSuccess;
  const heldIdentities = field.state.value.filter(isIdentityScope);
  const offered = (providers.data ?? [])
    .filter((provider) => isIdentityScopeSlug(provider.slug))
    .map((provider) => ({
      value: identityScope(provider.slug),
      name: provider.displayName,
    }));
  const offeredValues = new Set<string>(offered.map(({ value }) => value));
  const unavailable = loaded
    ? heldIdentities.filter((scope) => !offeredValues.has(scope))
    : [];
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
        field.handleChange([
          ...oidcScopes
            .map((scope) => scope.value)
            .filter((scope) => scope === "openid" || chosen.has(scope)),
          ...(loaded
            ? [
                ...offered
                  .map(({ value }) => value)
                  .filter((scope) => chosen.has(scope)),
                ...unavailable.filter((scope) => chosen.has(scope)),
              ]
            : heldIdentities),
        ]);
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
      {(offered.length > 0 || unavailable.length > 0) && (
        <span className="mt-2 text-sm font-medium">
          <Trans id="admin.oidc-apps.field.scopes.identities">
            Provider identities
          </Trans>
        </span>
      )}
      {offered.map(({ value, name }) => (
        <ScopeCheckbox
          key={value}
          value={value}
          description={i18n._({
            ...identityDescription,
            values: { provider: name },
          })}
        />
      ))}
      {unavailable.map((value) => (
        <ScopeCheckbox
          key={value}
          value={value}
          description={i18n._(unavailableIdentityDescription)}
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
  value: AllowedScope;
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
