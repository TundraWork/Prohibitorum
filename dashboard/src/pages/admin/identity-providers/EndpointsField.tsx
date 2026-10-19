import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { PlusIcon, Trash2Icon } from "lucide-react";
import { type ReactNode, useId, useRef } from "react";
import { AddressInput } from "@/components/custom/AddressInput";
import { Button } from "@/components/custom/Button";
import { ChoiceTile } from "@/components/custom/ChoiceTile";
import { FormMessages } from "@/components/custom/FormMessages";
import { getTopLayerContainer } from "@/components/custom/top-layer";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import { Label } from "@/components/ui/label";
import { Menu, MenuItem, MenuPopup, MenuTrigger } from "@/components/ui/menu";
import { Radio, RadioGroup } from "@/components/ui/radio-group";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";
import {
  type EndpointName,
  endpointLabel,
  endpointNames,
} from "@/pages/admin/identity-providers/provider-options";
import {
  type EndpointProblem,
  type EndpointsValue,
  isEndpointProblem,
  jwksNone,
  shownEndpoints,
} from "@/pages/admin/identity-providers/provider-validation";

const removeOverrideMessage = msg({
  id: "admin.federation.endpoints.remove",
  message: "Remove the {endpoint} override",
});

/**
 * Where the provider's endpoints come from, as one form field.
 *
 * Most providers publish a discovery document, and then the four endpoints are
 * not the reader's business: under discovery none is drawn until the reader
 * adds an override from the menu, and each override is a row that can be taken
 * away again. A provider without discovery gets the four rows at once, three of
 * them required. Switching between the two keeps whatever was typed, so a
 * reader who tries one mode and comes back has not lost anything; only what the
 * current mode shows is sent (`endpointsBody`).
 *
 * The JWKS endpoint is a choice rather than a field: an address to check the ID
 * token with, or none, which skips the ID token and identifies people through
 * UserInfo. "None" is its own option, so it is never a word typed into an
 * address box.
 *
 * A problem with one endpoint arrives as an `EndpointProblem` among the field's
 * errors and is drawn under that endpoint's input, which is the only one marked;
 * editing it clears that one complaint and leaves the others.
 */
export function EndpointsField() {
  const { i18n } = useLingui();
  const field = useFieldContext<EndpointsValue>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const labelId = useId();
  const modeErrorId = useId();
  // The row the menu just added, so focus lands in its input once the menu has
  // handed focus back and closed.
  const added = useRef<EndpointName | null>(null);
  const inputs = useRef<
    Partial<Record<EndpointName, HTMLTextAreaElement | null>>
  >({});

  const value = field.state.value;
  const errors: unknown[] = field.state.meta.errors.flat(Infinity);
  const problems = errors.filter(isEndpointProblem);
  const fieldErrors = errors.filter((error) => !isEndpointProblem(error));
  const discovery = value.mode === "discovery";
  const shown = shownEndpoints(value);
  const remaining = endpointNames.filter(
    (entry) => !shown.includes(entry.name),
  );

  function change(next: EndpointsValue, clearing: EndpointName | "all") {
    if (form.state.isSubmitting) return;
    const kept = [withoutServerErrors(field.state.meta.errorMap.onSubmit)]
      .flat(Infinity)
      .filter(
        (error) =>
          error !== undefined &&
          clearing !== "all" &&
          !(isEndpointProblem(error) && error.endpoint === clearing),
      );
    field.setErrorMap({ onSubmit: kept.length > 0 ? kept : undefined });
    field.handleChange(next);
  }

  function edit(name: EndpointName, text: string) {
    change({ ...value, values: { ...value.values, [name]: text } }, name);
  }

  function removeOverride(name: EndpointName) {
    added.current = null;
    change(
      {
        ...value,
        overridden: value.overridden.filter((candidate) => candidate !== name),
      },
      name,
    );
  }

  const rowProps = (name: EndpointName) => ({
    name,
    fieldName: `${field.name}.${name}`,
    value: value.values[name],
    disabled: submitting,
    problem: problems.find((problem) => problem.endpoint === name),
    inputRef: (element: HTMLTextAreaElement | null) => {
      inputs.current[name] = element;
    },
    onChange: (text: string) => edit(name, text),
    remove: discovery ? (
      <Button
        disabled={submitting}
        aria-label={i18n._({
          ...removeOverrideMessage,
          values: { endpoint: i18n._(endpointLabel(name)) },
        })}
        onClick={() => removeOverride(name)}
        variant="ghost"
        size="icon-sm"
      >
        <Trash2Icon aria-hidden="true" />
      </Button>
    ) : null,
  });

  return (
    <Field
      name={field.name}
      invalid={fieldErrors.length > 0}
      className="items-stretch gap-3"
    >
      <span id={labelId} className="font-medium text-sm">
        <Trans id="admin.federation.connection.mode">Endpoints</Trans>
      </span>
      <RadioGroup
        aria-labelledby={labelId}
        aria-invalid={fieldErrors.length > 0 || undefined}
        aria-describedby={fieldErrors.length > 0 ? modeErrorId : undefined}
        name={`${field.name}.mode`}
        value={value.mode}
        disabled={submitting}
        onValueChange={(mode) => {
          added.current = null;
          change({ ...value, mode: mode as EndpointsValue["mode"] }, "all");
        }}
      >
        <ChoiceTile
          layout="list"
          value="discovery"
          label={
            <Trans id="admin.federation.connection.mode.discovery">
              Discover automatically
            </Trans>
          }
          description={
            <Trans id="admin.federation.connection.mode.discovery.hint">
              Reads the endpoints from the issuer's discovery document.
            </Trans>
          }
        />
        <ChoiceTile
          layout="list"
          value="manual"
          label={
            <Trans id="admin.federation.connection.mode.manual">
              Enter the endpoints
            </Trans>
          }
          description={
            <Trans id="admin.federation.connection.mode.manual.hint">
              For a provider that does not publish discovery.
            </Trans>
          }
        />
      </RadioGroup>
      {fieldErrors.length > 0 && (
        <FieldError match id={modeErrorId}>
          <FormMessages errors={fieldErrors} />
        </FieldError>
      )}

      {discovery && shown.length > 0 && (
        <p className="text-muted-foreground text-xs">
          <Trans id="admin.federation.endpoints.override.note">
            The addresses entered here replace what discovery finds.
          </Trans>
        </p>
      )}

      {shown.length > 0 && (
        <div
          data-slot="endpoint-rows"
          className="flex flex-col gap-4 rounded-xl bg-muted/60 p-3 ring-1 ring-border/60"
        >
          {shown.map((name) =>
            name === "jwks" ? (
              <JwksEndpoint key={name} {...rowProps(name)} />
            ) : (
              <EndpointRow
                key={name}
                {...rowProps(name)}
                description={
                  !discovery && name === "userinfo" ? (
                    <Trans id="admin.federation.connection.userinfo.hint">
                      Leave it empty to skip the UserInfo request.
                    </Trans>
                  ) : undefined
                }
              />
            ),
          )}
        </div>
      )}

      {discovery && (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <Menu>
            <MenuTrigger
              render={
                <Button
                  variant="outline"
                  size="sm"
                  disabled={submitting || remaining.length === 0}
                />
              }
            >
              <PlusIcon aria-hidden="true" />
              <Trans id="admin.federation.endpoints.override">
                Override an endpoint
              </Trans>
            </MenuTrigger>
            <MenuPopup
              align="start"
              portalProps={{ container: getTopLayerContainer() }}
              finalFocus={() => {
                const name = added.current;
                added.current = null;
                return (name !== null && inputs.current[name]) || true;
              }}
            >
              {remaining.map((entry) => (
                <MenuItem
                  key={entry.name}
                  onClick={() => {
                    added.current = entry.name;
                    change(
                      {
                        ...value,
                        overridden: [...value.overridden, entry.name],
                      },
                      entry.name,
                    );
                  }}
                >
                  {i18n._(entry.label)}
                </MenuItem>
              ))}
            </MenuPopup>
          </Menu>
          {remaining.length === 0 && (
            <span className="text-muted-foreground text-xs">
              <Trans id="admin.federation.endpoints.all-overridden">
                Every endpoint is already overridden.
              </Trans>
            </span>
          )}
        </div>
      )}
    </Field>
  );
}

interface EndpointRowProps {
  name: EndpointName;
  fieldName: string;
  value: string;
  disabled: boolean;
  problem: EndpointProblem | undefined;
  inputRef: (element: HTMLTextAreaElement | null) => void;
  onChange: (value: string) => void;
  /** The button that drops an override; null when the row is not one. */
  remove: ReactNode;
}

/**
 * One endpoint: its name over a monospace address input, with the button that
 * drops an override beside the name so the row's message below never pushes it
 * out of place.
 */
function EndpointRow({
  name,
  fieldName,
  value,
  disabled,
  problem,
  inputRef,
  onChange,
  remove,
  description,
}: EndpointRowProps & { description?: ReactNode }) {
  const { i18n } = useLingui();
  return (
    <Field name={fieldName} disabled={disabled} invalid={problem !== undefined}>
      <div className="flex w-full items-center justify-between gap-2">
        <FieldLabel>{i18n._(endpointLabel(name))}</FieldLabel>
        {remove}
      </div>
      {/* Monospace: the address has to match the provider's own copy, and a
          stray character has to show. */}
      <AddressInput
        ref={inputRef}
        value={value}
        onValueChange={onChange}
        placeholder="https://"
        aria-invalid={problem !== undefined || undefined}
      />
      {description !== undefined && (
        <FieldDescription>{description}</FieldDescription>
      )}
      {problem !== undefined && (
        <FieldError match>{i18n._(problem.message)}</FieldError>
      )}
    </Field>
  );
}

/**
 * The JWKS endpoint as two explicit choices: an address, or none. The address
 * the reader last typed is kept while "none" is chosen, so changing their mind
 * back does not lose it.
 */
function JwksEndpoint({
  name,
  fieldName,
  value,
  disabled,
  problem,
  inputRef,
  onChange,
  remove,
}: EndpointRowProps) {
  const { i18n } = useLingui();
  const labelId = useId();
  const errorId = useId();
  const [addressId, addressHintId, noneId, noneHintId] = [
    useId(),
    useId(),
    useId(),
    useId(),
  ];
  const none = value === jwksNone;
  // The address typed before "none" was chosen; read when the choice goes back.
  const lastAddress = useRef(none ? "" : value);

  return (
    <div className="flex flex-col gap-2">
      <div className="flex w-full items-center justify-between gap-2">
        <span id={labelId} className="font-medium text-sm">
          {i18n._(endpointLabel(name))}
        </span>
        {remove}
      </div>
      <RadioGroup
        aria-labelledby={labelId}
        name={fieldName}
        value={none ? "none" : "address"}
        disabled={disabled}
        onValueChange={(next) => {
          if (next === "none") {
            if (!none) lastAddress.current = value;
            onChange(jwksNone);
          } else {
            onChange(lastAddress.current);
          }
        }}
      >
        <div className="flex flex-col gap-2">
          <Label className="items-start gap-2 font-normal text-sm">
            <Radio
              value="address"
              aria-labelledby={addressId}
              aria-describedby={addressHintId}
              className="mt-0.5"
            />
            <span className="flex flex-col gap-0.5">
              <span id={addressId} className="font-medium">
                <Trans id="admin.federation.connection.jwks.address">
                  Check the ID token with a JWKS address
                </Trans>
              </span>
              <span
                id={addressHintId}
                className="text-muted-foreground text-xs"
              >
                <Trans id="admin.federation.connection.jwks.address.hint">
                  The provider's signing keys are read from this address.
                </Trans>
              </span>
            </span>
          </Label>
          {!none && (
            <div className="ps-6">
              <AddressInput
                ref={inputRef}
                value={value}
                disabled={disabled}
                onValueChange={(text) => {
                  lastAddress.current = text;
                  onChange(text);
                }}
                placeholder="https://"
                aria-label={i18n._(endpointLabel(name))}
                aria-invalid={problem !== undefined || undefined}
                aria-describedby={problem === undefined ? undefined : errorId}
              />
              {problem !== undefined && (
                <p
                  id={errorId}
                  className="mt-1.5 text-destructive-foreground text-xs"
                >
                  {i18n._(problem.message)}
                </p>
              )}
            </div>
          )}
        </div>
        <Label className="items-start gap-2 font-normal text-sm">
          <Radio
            value="none"
            aria-labelledby={noneId}
            aria-describedby={noneHintId}
            className="mt-0.5"
          />
          <span className="flex flex-col gap-0.5">
            <span id={noneId} className="font-medium">
              <Trans id="admin.federation.connection.jwks.none">
                None — skip the ID token
              </Trans>
            </span>
            <span id={noneHintId} className="text-muted-foreground text-xs">
              <Trans id="admin.federation.connection.jwks.none.hint">
                People are identified through the UserInfo endpoint instead.
              </Trans>
            </span>
          </span>
        </Label>
      </RadioGroup>
    </div>
  );
}
