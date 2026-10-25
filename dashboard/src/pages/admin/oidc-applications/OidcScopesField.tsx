import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useQuery } from "@tanstack/react-query";
import { useId } from "react";
import { federationProvidersQueryOptions } from "@/api/queries";
import { quietQuery } from "@/app/query-client";
import { FormMessages } from "@/components/custom/FormMessages";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { CheckboxGroup } from "@/components/ui/checkbox-group";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";
import { cn } from "@/lib/utils";
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
    "This provider is disabled or no longer exists. Clear it to stop allowing it.",
});

/**
 * The scopes a client may request, as a field of scope names.
 *
 * The instance knows a closed set, so every one is drawn as a row with a line
 * saying what it gives the client, and the reader chooses rather than types.
 * The name is monospace because it is the string the client sends. `openid` is
 * read-only and always held: a request without it is refused, so offering to
 * leave it out would only offer a client that cannot sign in. It is read-only
 * rather than disabled, which would fade it into looking unavailable.
 *
 * Under them, one `identity:<slug>` row per enabled provider lets the client
 * read the user's linked identities there. The list comes from the providers
 * the instance offers, which application managers can read as well as
 * administrators. A saved identity scope whose provider is no longer offered
 * stays drawn, with the reason, so it can be cleared. `saved` is what the
 * record holds now: a saved scope the reader has cleared keeps its row, marked
 * "Will be removed", until the change is saved or discarded.
 *
 * The value keeps the vocabulary's order whatever order the rows were ticked
 * in, so a saved record reads the same way the list does.
 */
export function OidcScopesField({
  saved = [],
}: {
  /** What the record holds now; the create page has none. */
  saved?: readonly string[];
}) {
  const { i18n } = useLingui();
  const field = useFieldContext<AllowedScope[]>();
  const providers = useQuery(quietQuery(federationProvidersQueryOptions()));
  const labelId = useId();
  // Until the provider list is in, the identity scopes are neither drawn nor
  // touched: drawing them would call every saved one unavailable, and a change
  // to another row must not drop them.
  const loaded = providers.isSuccess;
  const heldIdentities = field.state.value.filter(isIdentityScope);
  const offered = (providers.data ?? [])
    .filter((provider) => isIdentityScopeSlug(provider.slug))
    .map((provider) => ({
      value: identityScope(provider.slug),
      name: provider.displayName,
    }));
  const offeredValues = new Set<string>(offered.map(({ value }) => value));
  // A cleared one stays in the list through `saved`, so it can be ticked again.
  const unavailable = loaded
    ? [
        ...new Set([...saved.filter(isIdentityScope), ...heldIdentities]),
      ].filter((scope) => !offeredValues.has(scope))
    : [];
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const errors: unknown[] = field.state.meta.errors.flat(Infinity);
  const invalid = errors.length > 0;

  const row = (
    value: AllowedScope,
    description: string,
    warn = false,
  ): React.ReactElement => (
    <ScopeRow
      key={value}
      value={value}
      description={description}
      warn={warn}
      removed={saved.includes(value) && !field.state.value.includes(value)}
    />
  );

  return (
    <div className="flex flex-col gap-1">
      <div id={labelId} className="mb-1 font-medium text-sm">
        <Trans id="admin.oidc-apps.field.scopes">Scopes</Trans>
      </div>
      <CheckboxGroup
        aria-labelledby={labelId}
        className="-mx-3 items-stretch gap-0"
        value={field.state.value}
        disabled={submitting}
        onValueChange={(next) => {
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
        {oidcScopes.map((scope) => row(scope.value, i18n._(scope.description)))}
        {(offered.length > 0 || unavailable.length > 0) && (
          <div className="mt-3 mb-1 px-3 font-medium text-sm">
            <Trans id="admin.oidc-apps.field.scopes.identities">
              Provider identities
            </Trans>
          </div>
        )}
        {offered.map(({ value, name }) =>
          row(
            value,
            i18n._({ ...identityDescription, values: { provider: name } }),
          ),
        )}
        {unavailable.map((value) =>
          row(value, i18n._(unavailableIdentityDescription), true),
        )}
      </CheckboxGroup>
      {providers.isError && (
        <p className="text-muted-foreground text-sm">
          <Trans id="admin.oidc-apps.scopes.providers-failed">
            Could not load provider identities. The saved selection is kept.
          </Trans>
        </p>
      )}
      {invalid && (
        <p className="text-destructive-foreground text-caption">
          <FormMessages errors={errors} />
        </p>
      )}
    </div>
  );
}

/**
 * One scope. The whole row is the label, so pressing anywhere on it toggles the
 * box. The box is named by the scope alone; the line under it describes it
 * rather than becoming part of its name.
 */
function ScopeRow({
  value,
  description,
  removed,
  warn,
}: {
  value: AllowedScope;
  description: string;
  removed: boolean;
  warn: boolean;
}) {
  const nameId = useId();
  const descriptionId = useId();
  return (
    // biome-ignore lint/a11y/noLabelWithoutControl: the checkbox inside is its control
    <label
      className={cn(
        "flex items-start gap-3 rounded-lg px-3 py-2.5",
        removed && "bg-warning/8",
      )}
    >
      <Checkbox
        value={value}
        aria-labelledby={nameId}
        aria-describedby={descriptionId}
        readOnly={value === "openid"}
        className="mt-0.5"
      />
      <span className="min-w-0 flex-1">
        <span id={nameId} className="block font-mono text-[13px]">
          {value}
        </span>
        <span
          id={descriptionId}
          className={cn(
            "block text-sm",
            warn ? "text-warning-foreground" : "text-muted-foreground",
          )}
        >
          {description}
        </span>
      </span>
      {removed && (
        <Badge variant="warning">
          <Trans id="admin.oidc-apps.scope.removed">Will be removed</Trans>
        </Badge>
      )}
    </label>
  );
}
