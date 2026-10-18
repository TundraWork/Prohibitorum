import { Trans, useLingui } from "@lingui/react/macro";
import { SearchIcon, XIcon } from "lucide-react";
import { type ReactNode, useId } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import { useFieldBinding } from "@/components/custom/use-field-binding";
import {
  Combobox,
  ComboboxClear,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
  ComboboxPopup,
  ComboboxTrigger,
  ComboboxValue,
} from "@/components/ui/combobox";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import { SelectButton } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";

/**
 * Search for one entity and select it.
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
 *
 * `onValueChange` always receives a list of ids, whichever mode the picker is
 * in. HeroUI hands a single-selection picker one key (or `null` once cleared)
 * and a multiple one an array, so the difference is settled here, once, rather
 * than at every call site that reads the first entry.
 *
 * The trigger shows the selection as one line of names — the options' labels
 * joined the way the reader's language joins a list, cut short with an ellipsis
 * when they run past the field. The description under each option belongs to
 * the list, where it tells two similar names apart; repeating it in the trigger
 * is what made several selected groups stack into an unreadable column.
 *
 * Searching is the server's when the caller passes `onSearch`, since the
 * account directory pages and only the server can look past the first page.
 * Without it the options are the whole set, and the picker narrows them by
 * label itself.
 */
export interface EntityOption {
  id: string;
  label: string;
  description?: string;
  /**
   * Why this candidate cannot be chosen. The option is still listed, so a
   * reader looking for it finds it and learns why, but it cannot be selected.
   */
  unavailableReason?: string;
}

/**
 * Presentational core: a controlled select-with-search over a fixed `options`
 * list, with the pending and empty states both drawn.
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
}: {
  label: ReactNode;
  description?: ReactNode;
  /** Shown while nothing is selected, and as the search box's hint. */
  placeholder: string;
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
}) {
  const { i18n, t } = useLingui();
  const searchId = useId();
  // A selection the latest result did not return still has to render, or the
  // control would silently look empty while holding a value.
  const known = new Set(options.map((option) => option.id));
  const items: EntityOption[] = [
    ...options,
    ...value.filter((id) => !known.has(id)).map((id) => ({ id, label: id })),
  ];
  const selected = value.flatMap((id) => {
    const item = items.find((candidate) => candidate.id === id);
    return item === undefined ? [] : [item];
  });
  // Joined in the console's language rather than the browser's.
  const selectedText = new Intl.ListFormat(i18n.locale, {
    type: "conjunction",
  }).format(selected.map((item) => item.label));
  const selection = multiple ? selected : (selected[0] ?? null);

  return (
    <Field className="w-full" disabled={isDisabled} invalid={isInvalid}>
      <FieldLabel>{label}</FieldLabel>
      <Combobox<EntityOption, boolean>
        items={items}
        multiple={multiple}
        value={selection}
        disabled={isDisabled}
        itemToStringLabel={(item) => item.label}
        isItemEqualToValue={(item, picked) => item.id === picked.id}
        // The server did the narrowing when the caller searches; otherwise
        // the default match against each label applies.
        filter={onSearch === undefined ? undefined : null}
        onInputValueChange={(query, details) => {
          if (details.reason === "input-change") onSearch?.(query);
        }}
        onValueChange={(next) => {
          if (Array.isArray(next)) onValueChange(next.map((item) => item.id));
          else onValueChange(next === null ? [] : [(next as EntityOption).id]);
        }}
      >
        <div className="relative w-full">
          <ComboboxTrigger
            aria-invalid={isInvalid || undefined}
            render={<SelectButton />}
          >
            <ComboboxValue>
              {() =>
                selected.length === 0 ? (
                  <span className="text-muted-foreground">{placeholder}</span>
                ) : (
                  selectedText
                )
              }
            </ComboboxValue>
          </ComboboxTrigger>
          {!multiple && selected.length > 0 && !isDisabled && (
            <ComboboxClear
              aria-label={t({
                id: "admin.picker.clear",
                message: "Clear selection",
              })}
              className="absolute end-8 top-1/2 inline-flex size-6 -translate-y-1/2 cursor-pointer items-center justify-center rounded-md opacity-80 outline-none hover:opacity-100 focus-visible:ring-2 focus-visible:ring-ring"
            >
              <XIcon className="size-4" />
            </ComboboxClear>
          )}
        </div>
        <ComboboxPopup>
          <div className="relative border-b p-2">
            {/* The field's label would otherwise name this input too, since
                it is the combobox's control; the search box has its own. */}
            <span id={searchId} className="sr-only">
              {searchLabel}
            </span>
            <ComboboxInput
              aria-labelledby={searchId}
              autoFocus
              placeholder={placeholder}
              showTrigger={false}
              startAddon={<SearchIcon />}
            />
            {/* Mounted only while a lookup runs: a spinner faded out but left
                in place is still a "Loading" status to a screen reader. It
                sits over the input, so nothing shifts. */}
            {loading && (
              <Spinner
                aria-label={t({
                  id: "admin.picker.loading",
                  message: "Loading",
                })}
                className="absolute end-4 top-1/2 size-4 -translate-y-1/2"
              />
            )}
          </div>
          <ComboboxEmpty>
            <Trans id="admin.picker.empty">No matches</Trans>
          </ComboboxEmpty>
          <ComboboxList>
            {(item: EntityOption) => {
              const note = [item.description, item.unavailableReason]
                .filter(Boolean)
                .join(" · ");
              return (
                <ComboboxItem
                  key={item.id}
                  value={item}
                  disabled={item.unavailableReason !== undefined}
                >
                  <span className="flex min-w-0 flex-col">
                    <span className="truncate">{item.label}</span>
                    {note && (
                      <span className="truncate text-muted-foreground text-xs">
                        {note}
                      </span>
                    )}
                  </span>
                </ComboboxItem>
              );
            }}
          </ComboboxList>
        </ComboboxPopup>
      </Combobox>
      {description !== undefined && (
        <FieldDescription>{description}</FieldDescription>
      )}
      {isInvalid && errorMessage !== undefined && (
        <FieldError match>{errorMessage}</FieldError>
      )}
    </Field>
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
  const { field, errors, invalid, disabled, onChange } = useFieldBinding<
    string | string[]
  >(false);
  const selected = props.multiple
    ? ((field.state.value as string[]) ?? [])
    : field.state.value
      ? [field.state.value as string]
      : [];

  return (
    <EntityPicker
      {...props}
      errorMessage={<FormMessages errors={errors} />}
      isDisabled={disabled}
      isInvalid={invalid}
      value={selected}
      onValueChange={(next) =>
        onChange(props.multiple ? next : (next[0] ?? ""))
      }
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
