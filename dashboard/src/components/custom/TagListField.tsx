import type { MessageDescriptor } from "@lingui/core";
import { useLingui } from "@lingui/react/macro";
import { type ReactNode, useId } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import { useFieldBinding } from "@/components/custom/use-field-binding";
import { ValueSet } from "@/components/custom/ValueSet";
import { Fieldset, FieldsetLegend } from "@/components/ui/fieldset";
import { cn } from "@/lib/utils";

/**
 * A short list of literal values — an identity provider's scopes, its allowed
 * email domains — edited as a `ValueSet`.
 *
 * Each value is a chip with its own remove button; adding is a step in a
 * popover. A value is checked by the caller's `check` before it joins the list,
 * and one it refuses stays in the popover's input with the reason under it.
 * Values are kept as typed apart from surrounding space: a scope and a domain
 * are compared literally by the server, so lower-casing would save something
 * other than what the reader sees.
 *
 * The field's value holds the input's text as well as the chips. That is so a
 * submit can see it: text typed and never added is a list the reader thinks
 * they have saved, and the form cannot refuse what it does not hold. The
 * caller's submit check decides what to say about it, and it is shown under
 * the chips, where it is visible with the popover closed.
 *
 * A `lockedTags` value is drawn without a remove button, for a value the list
 * cannot do without.
 */
export function TagListField({
  label,
  description,
  placeholder,
  addLabel,
  check,
  lockedTags = [],
  emptyText,
  isLabelHidden = false,
}: {
  /** The list's name. */
  label: string;
  description?: ReactNode;
  placeholder?: string;
  /** Names the add chip, e.g. "Add scope". */
  addLabel: string;
  /** Why a value cannot join the list, or undefined when it can. */
  check: (
    value: string,
    tags: readonly string[],
  ) => MessageDescriptor | undefined;
  lockedTags?: readonly string[];
  /** What an empty list means, for a list where empty is a setting. */
  emptyText?: string;
  isLabelHidden?: boolean;
}) {
  const { i18n } = useLingui();
  const { field, errors, invalid, disabled, onChange } = useFieldBinding<{
    tags: string[];
    draft: string;
  }>(false);
  const [descriptionId, errorId] = [useId(), useId()];
  const { tags, draft } = field.state.value;

  // A fieldset, not a `Field`: the popover's input must not become the
  // field's control and take its label as its own name.
  return (
    <Fieldset
      disabled={disabled}
      aria-describedby={
        [description !== undefined && descriptionId, invalid && errorId]
          .filter(Boolean)
          .join(" ") || undefined
      }
      className="flex w-full min-w-0 flex-col gap-2"
    >
      <FieldsetLegend
        className={cn(
          "font-medium text-base/4.5 sm:text-sm/4",
          isLabelHidden && "sr-only",
        )}
      >
        {label}
      </FieldsetLegend>
      <ValueSet
        values={tags}
        lockedValues={lockedTags}
        isDisabled={disabled}
        addLabel={addLabel}
        emptyText={emptyText}
        placeholder={placeholder ?? ""}
        draft={draft}
        onDraftChange={(next) => onChange({ tags, draft: next })}
        validate={(value) => {
          const problem = check(value, tags);
          return problem === undefined ? undefined : i18n._(problem);
        }}
        onAdd={(value) => onChange({ tags: [...tags, value], draft: "" })}
        onRemove={(value) =>
          onChange({ tags: tags.filter((tag) => tag !== value), draft })
        }
      />
      {description !== undefined && (
        <p id={descriptionId} className="text-muted-foreground text-xs">
          {description}
        </p>
      )}
      {invalid && (
        <p id={errorId} className="text-destructive-foreground text-xs">
          <FormMessages errors={errors} />
        </p>
      )}
    </Fieldset>
  );
}
