import {
  Description,
  Dropdown,
  FieldError,
  Input,
  Label,
  Radio,
  RadioGroup,
  TextField,
  Tooltip,
} from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { Plus, Trash2 } from "lucide-react";
import { type ReactNode, useEffect, useRef } from "react";
import { Button } from "@/components/custom/Button";
import { FormMessages } from "@/components/custom/FormMessages";
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
 * away again. A provider without discovery gets the four fields at once, three
 * of them required. Switching between the two keeps whatever was typed, so a
 * reader who tries one mode and comes back has not lost anything; only what the
 * current mode shows is sent (`endpointsBody`).
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
  // The row the menu just added. The menu hands focus back to its button when
  // it closes, after the row is drawn, so the row's input takes focus from
  // the button rather than on mount, where the menu would take it back.
  const added = useRef<EndpointName | null>(null);
  const inputs = useRef<Partial<Record<EndpointName, HTMLInputElement | null>>>(
    {},
  );

  const value = field.state.value;
  const errors: unknown[] = field.state.meta.errors.flat(Infinity);
  const problems = errors.filter(isEndpointProblem);
  const fieldErrors = errors.filter((error) => !isEndpointProblem(error));
  const discovery = value.mode === "discovery";
  const shown = shownEndpoints(value);

  function change(next: EndpointsValue, clearing: EndpointName | "all") {
    if (form.state.isSubmitting) return;
    const remaining = [withoutServerErrors(field.state.meta.errorMap.onSubmit)]
      .flat(Infinity)
      .filter(
        (error) =>
          error !== undefined &&
          clearing !== "all" &&
          !(isEndpointProblem(error) && error.endpoint === clearing),
      );
    field.setErrorMap({
      onSubmit: remaining.length > 0 ? remaining : undefined,
    });
    field.handleChange(next);
  }

  const allOverridden = shown.length === endpointNames.length;
  // The last override takes the menu's own button away with it, so focus has
  // no button to return to; the new row takes it once the menu has gone.
  useEffect(() => {
    if (!allOverridden) return;
    const name = added.current;
    if (name === null) return;
    added.current = null;
    const frame = requestAnimationFrame(() => inputs.current[name]?.focus());
    return () => cancelAnimationFrame(frame);
  }, [allOverridden]);
  const overrideButton = (
    <Button
      size="sm"
      variant="outline"
      isDisabled={submitting || allOverridden}
      onFocus={() => {
        const name = added.current;
        if (name === null) return;
        added.current = null;
        inputs.current[name]?.focus();
      }}
    >
      <Plus size={16} aria-hidden="true" />
      <Trans id="admin.federation.endpoints.override">
        Override an endpoint
      </Trans>
    </Button>
  );
  // A disabled button emits no hover or focus, so the reason sits on a
  // trigger around it, as every other disabled action on the console does.
  // Once every endpoint is overridden the menu has nothing left to offer, so
  // the button is drawn on its own, disabled, with the reason beside it.
  const overrideDisabled = (
    <Tooltip delay={0}>
      <Tooltip.Trigger>{overrideButton}</Tooltip.Trigger>
      <Tooltip.Content>
        <Trans id="admin.federation.endpoints.all-overridden">
          Every endpoint is already overridden.
        </Trans>
      </Tooltip.Content>
    </Tooltip>
  );

  return (
    <div className="flex flex-col gap-4">
      <RadioGroup
        variant="secondary"
        name={`${field.name}.mode`}
        value={value.mode}
        isDisabled={submitting}
        isInvalid={fieldErrors.length > 0}
        onChange={(mode) => {
          added.current = null;
          change({ ...value, mode: mode as EndpointsValue["mode"] }, "all");
        }}
      >
        <Label>
          <Trans id="admin.federation.connection.mode">Endpoints</Trans>
        </Label>
        <Radio value="discovery">
          <Radio.Content>
            <Radio.Control>
              <Radio.Indicator />
            </Radio.Control>
            <Trans id="admin.federation.connection.mode.discovery">
              Discover automatically
            </Trans>
          </Radio.Content>
          <Description>
            <Trans id="admin.federation.connection.mode.discovery.hint">
              Reads the endpoints from the issuer's discovery document.
            </Trans>
          </Description>
        </Radio>
        <Radio value="manual">
          <Radio.Content>
            <Radio.Control>
              <Radio.Indicator />
            </Radio.Control>
            <Trans id="admin.federation.connection.mode.manual">
              Enter the endpoints
            </Trans>
          </Radio.Content>
          <Description>
            <Trans id="admin.federation.connection.mode.manual.hint">
              For a provider that does not publish discovery.
            </Trans>
          </Description>
        </Radio>
        {fieldErrors.length > 0 && (
          <FieldError>
            <FormMessages errors={fieldErrors} />
          </FieldError>
        )}
      </RadioGroup>

      {discovery && shown.length > 0 && (
        <p className="text-sm text-muted">
          <Trans id="admin.federation.endpoints.override.note">
            The addresses entered here replace what discovery finds.
          </Trans>
        </p>
      )}

      {shown.map((name) => (
        <EndpointInput
          key={name}
          name={name}
          value={value.values[name]}
          inputRef={(element) => {
            inputs.current[name] = element;
          }}
          isDisabled={submitting}
          problem={problems.find((problem) => problem.endpoint === name)}
          description={
            !discovery && name === "userinfo" ? (
              <Trans id="admin.federation.connection.userinfo.hint">
                Leave it empty to skip the UserInfo request.
              </Trans>
            ) : undefined
          }
          onChange={(text) =>
            change(
              { ...value, values: { ...value.values, [name]: text } },
              name,
            )
          }
          remove={
            discovery ? (
              <Button
                isIconOnly
                size="sm"
                variant="ghost"
                isDisabled={submitting}
                aria-label={i18n._({
                  ...removeOverrideMessage,
                  values: { endpoint: i18n._(endpointLabel(name)) },
                })}
                onPress={() => {
                  added.current = null;
                  change(
                    {
                      ...value,
                      overridden: value.overridden.filter(
                        (candidate) => candidate !== name,
                      ),
                    },
                    name,
                  );
                }}
              >
                <Trash2 size={16} aria-hidden="true" />
              </Button>
            ) : null
          }
        />
      ))}

      {discovery && (
        <div>
          {allOverridden ? (
            overrideDisabled
          ) : (
            <Dropdown>
              {overrideButton}
              <Dropdown.Popover placement="bottom start">
                <Dropdown.Menu
                  disabledKeys={value.overridden}
                  onAction={(key) => {
                    const name = endpointNames.find(
                      (entry) => entry.name === key,
                    )?.name;
                    if (name === undefined) return;
                    added.current = name;
                    change(
                      { ...value, overridden: [...value.overridden, name] },
                      name,
                    );
                  }}
                >
                  {endpointNames.map((entry) => (
                    <Dropdown.Item
                      id={entry.name}
                      key={entry.name}
                      textValue={i18n._(entry.label)}
                    >
                      <Label>{i18n._(entry.label)}</Label>
                    </Dropdown.Item>
                  ))}
                </Dropdown.Menu>
              </Dropdown.Popover>
            </Dropdown>
          )}
        </div>
      )}
    </div>
  );
}

/**
 * One endpoint: its name over a monospace address input, and under discovery
 * the button that drops the override on the input's own centre line, so the
 * row's message below never pushes the button out of place.
 */
function EndpointInput({
  name,
  value,
  inputRef,
  isDisabled,
  problem,
  description,
  onChange,
  remove,
}: {
  name: EndpointName;
  value: string;
  inputRef: (element: HTMLInputElement | null) => void;
  isDisabled: boolean;
  problem: EndpointProblem | undefined;
  description: ReactNode;
  onChange: (value: string) => void;
  remove: ReactNode;
}) {
  const { i18n } = useLingui();
  const invalid = problem !== undefined;
  return (
    <TextField
      className="flex flex-col gap-1.5"
      value={value}
      isDisabled={isDisabled}
      isInvalid={invalid}
      validationBehavior="aria"
      onChange={onChange}
    >
      <Label>{i18n._(endpointLabel(name))}</Label>
      <div
        className={
          remove === null
            ? undefined
            : "grid grid-cols-[minmax(0,1fr)_2rem] items-center gap-x-2"
        }
      >
        {/* Monospace: the address has to match the provider's own copy, and a
            stray character has to show. */}
        <Input
          className="w-full font-mono"
          inputMode="url"
          autoComplete="off"
          spellCheck={false}
          ref={inputRef}
          placeholder="https://"
          variant="secondary"
        />
        {remove !== null && <div className="flex justify-end">{remove}</div>}
      </div>
      {description !== undefined && <Description>{description}</Description>}
      {invalid && <FieldError>{i18n._(problem.message)}</FieldError>}
    </TextField>
  );
}
