import {
  Autocomplete,
  EmptyState,
  Label,
  ListBox,
  SearchField,
  Spinner,
} from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import type { ComponentProps, ReactNode } from "react";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";

/**
 * Search the server for one entity and select it.
 *
 * Accounts, user groups and identity providers are three vocabularies with one
 * shape: a keyword goes to the server, a list of candidates comes back, and one
 * of them becomes the value. Each picker below supplies only its query and how
 * a candidate reads; the searching, the pending and empty states, and the
 * single/multiple handling live here, so a fourth entity would not need a
 * fourth implementation.
 *
 * The candidate set is never stored in the form: the selected id is the value,
 * and the label is looked up from the current result. The field degrades
 * honestly when it cannot resolve one — a saved id whose entity the current
 * query did not return renders as the bare id rather than as a blank control.
 */
export interface EntityOption {
  id: string;
  label: string;
  description?: string;
}

/**
 * Presentational core: a controlled autocomplete over a fixed `options` list
 * with the pending and empty states both drawn.
 */
export function EntityPicker({
  label,
  description,
  placeholder,
  searchLabel,
  value,
  onValueChange,
  onSearch,
  options,
  loading,
  multiple = false,
  isDisabled = false,
  isInvalid = false,
  errorMessage,
  variant,
}: {
  label: ReactNode;
  description?: ReactNode;
  placeholder?: string;
  searchLabel: string;
  value: readonly string[];
  onValueChange: (value: string[]) => void;
  onSearch?: (query: string) => void;
  options: readonly EntityOption[];
  loading: boolean;
  multiple?: boolean;
  isDisabled?: boolean;
  isInvalid?: boolean;
  errorMessage?: ReactNode;
  variant?: ComponentProps<typeof Autocomplete>["variant"];
}) {
  // A selection the latest result did not return still has to render, or the
  // control would silently look empty while holding a value.
  const known = new Set(options.map((option) => option.id));
  const items: EntityOption[] = [
    ...options,
    ...value.filter((id) => !known.has(id)).map((id) => ({ id, label: id })),
  ];

  return (
    <Autocomplete
      allowsEmptyCollection
      className="w-full"
      isDisabled={isDisabled}
      isInvalid={isInvalid}
      placeholder={placeholder}
      selectionMode={multiple ? "multiple" : "single"}
      value={multiple ? [...value] : (value[0] ?? null)}
      variant={variant}
      onChange={(keys) => onValueChange(keys as string[])}
    >
      <Label>{label}</Label>
      <Autocomplete.Trigger>
        <Autocomplete.Value />
        <Autocomplete.ClearButton />
        <Autocomplete.Indicator />
      </Autocomplete.Trigger>
      {description !== undefined && (
        <span className="text-xs text-muted">{description}</span>
      )}
      <Autocomplete.Popover>
        <Autocomplete.Filter onInputChange={(next) => onSearch?.(next)}>
          <SearchField
            aria-label={searchLabel}
            autoFocus
            className="sticky top-0 z-10"
            name="search"
            variant="secondary"
          >
            <SearchField.Group>
              <SearchField.SearchIcon />
              <SearchField.Input placeholder={placeholder} />
              <Spinner
                size="sm"
                className={
                  loading
                    ? "absolute end-2 top-1/2 -translate-y-1/2"
                    : "pointer-events-none absolute end-2 top-1/2 -translate-y-1/2 opacity-0"
                }
              />
            </SearchField.Group>
          </SearchField>
          <ListBox
            className="max-h-[420px] overflow-y-auto"
            items={items}
            renderEmptyState={() => (
              <EmptyState>
                <Trans id="admin.picker.empty">No matches</Trans>
              </EmptyState>
            )}
          >
            {(item: EntityOption) => (
              <ListBox.Item id={item.id} textValue={item.label}>
                <span className="flex min-w-0 flex-col">
                  <span className="truncate">{item.label}</span>
                  {item.description && (
                    <span className="truncate text-xs text-muted">
                      {item.description}
                    </span>
                  )}
                </span>
                <ListBox.ItemIndicator />
              </ListBox.Item>
            )}
          </ListBox>
        </Autocomplete.Filter>
      </Autocomplete.Popover>
      {isInvalid && errorMessage !== undefined && (
        <span className="text-sm text-danger" role="alert">
          {errorMessage}
        </span>
      )}
    </Autocomplete>
  );
}

/**
 * The pickers as TanStack Form fields: same search, same rendering, wired to
 * the field context so a form can register them like any other input. A
 * single-value picker stores a string, a multiple one an array of strings.
 */
function FieldEntityPicker(
  props: Omit<
    Parameters<typeof EntityPicker>[0],
    "value" | "onValueChange" | "isDisabled" | "isInvalid" | "errorMessage"
  > & { multiple: boolean },
) {
  const field = useFieldContext<string | string[]>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const errors: unknown[] = field.state.meta.errors.flat(Infinity);
  const selected = props.multiple
    ? ((field.state.value as string[]) ?? [])
    : field.state.value
      ? [field.state.value as string]
      : [];

  return (
    <EntityPicker
      {...props}
      errorMessage={
        <Trans id="admin.picker.invalid">Choose a valid option.</Trans>
      }
      isDisabled={submitting}
      isInvalid={errors.length > 0}
      value={selected}
      onValueChange={(next) => {
        field.setErrorMap({
          onSubmit: withoutServerErrors(field.state.meta.errorMap.onSubmit),
        });
        if (props.multiple) field.handleChange(next);
        else field.handleChange(next[0] ?? "");
      }}
    />
  );
}

/**
 * One account, chosen by username. Used wherever a policy names a person — the
 * manual-group decisions here, the application manager assignments in the
 * federation work.
 */
export function AccountPicker(
  props: Omit<
    Parameters<typeof FieldEntityPicker>[0],
    "multiple" | "searchLabel"
  >,
) {
  const { t } = useLingui();
  return (
    <FieldEntityPicker
      {...props}
      multiple={false}
      searchLabel={t({ id: "admin.picker.search", message: "Search" })}
    />
  );
}

/**
 * Manual user groups. Multiple by default, because an invitation may grant
 * several at once.
 */
export function GroupPicker(
  props: Omit<
    Parameters<typeof FieldEntityPicker>[0],
    "multiple" | "searchLabel" | "label"
  > & { label?: ReactNode; multiple?: boolean },
) {
  const { t } = useLingui();
  const { label, multiple = true, ...rest } = props;
  return (
    <FieldEntityPicker
      {...rest}
      label={label ?? <Trans id="admin.picker.group">User groups</Trans>}
      multiple={multiple}
      searchLabel={t({ id: "admin.picker.search", message: "Search" })}
    />
  );
}

/**
 * The upstream identity provider an invitation insists on. Only providers that
 * can actually authenticate someone belong here: a disabled one would produce
 * an invitation nobody can redeem.
 */
export function IdentityProviderPicker(
  props: Omit<
    Parameters<typeof FieldEntityPicker>[0],
    "multiple" | "searchLabel" | "label"
  > & { label?: ReactNode },
) {
  const { t } = useLingui();
  const { label, ...rest } = props;
  return (
    <FieldEntityPicker
      {...rest}
      label={
        label ?? <Trans id="admin.picker.provider">Upstream provider</Trans>
      }
      multiple={false}
      searchLabel={t({ id: "admin.picker.search", message: "Search" })}
    />
  );
}
