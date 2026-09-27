import {
  Description,
  FieldError,
  InputGroup,
  Label,
  Tag,
  TagGroup,
  TextField,
} from "@heroui/react";
import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import { useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { type ReactNode, useId, useState } from "react";
import { Button } from "@/components/custom/Button";
import { FormMessages } from "@/components/custom/FormMessages";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";

// React Aria names the button by this and the tag's own text together, so it
// reads "Remove profile" without naming the tag twice.
const removeTagMessage = msg({
  id: "form.tags.remove",
  message: "Remove",
});

const draftLabelMessage = msg({
  id: "form.tags.draft",
  message: "Add to {label}",
});

/**
 * A short list of literal values — an identity provider's scopes, its allowed
 * email domains — edited as tags.
 *
 * Each value is a tag with its own remove button, so the list reads as the set
 * it is rather than as lines in a box; the input under it adds one value at a
 * time, on Enter or with the button. A value is checked by the caller's
 * `check` before it joins the list, and one it refuses stays in the input,
 * marked, with the reason under it, so nothing typed is lost to a mistake.
 * Values are kept exactly as typed: a scope and a domain are both compared
 * literally by the server, so trimming or lower-casing would save something
 * other than what the reader sees.
 *
 * The field's value holds the input's text as well as the tags. That is so a
 * submit can see it: text typed and never added is a list the reader thinks
 * they have saved, and the form cannot refuse what it does not hold. The
 * caller's submit check decides what to say about it.
 *
 * Backspace in the empty input does not remove the last tag. A removal is a
 * press on the tag's own button, or Delete on a focused tag through the tag
 * group's own keyboard handling, so a stray key never takes a value away.
 * A `lockedTags` value is drawn without a remove button and ignored by the
 * keyboard removal, for a value the list cannot do without.
 */
export function TagListField({
  label,
  description,
  placeholder,
  addLabel,
  check,
  lockedTags = [],
  isLabelHidden = false,
}: {
  /** The list's name, also used to name the input for assistive technology. */
  label: string;
  description?: ReactNode;
  placeholder?: string;
  addLabel: ReactNode;
  /** Why a value cannot join the list, or undefined when it can. */
  check: (
    value: string,
    tags: readonly string[],
  ) => MessageDescriptor | undefined;
  lockedTags?: readonly string[];
  isLabelHidden?: boolean;
}) {
  const { i18n } = useLingui();
  const field = useFieldContext<{ tags: string[]; draft: string }>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const [refused, setRefused] = useState<MessageDescriptor | null>(null);
  const descriptionId = useId();
  const errorId = useId();

  const { tags, draft } = field.state.value;
  // A refusal already says why the text is still in the input, so it stands
  // in for the submit's "add it or clear it" rather than doubling it.
  const errors: unknown[] =
    refused === null ? field.state.meta.errors.flat(Infinity) : [refused];
  const invalid = errors.length > 0;

  function change(next: { tags: string[]; draft: string }) {
    if (form.state.isSubmitting) return;
    field.setErrorMap({
      onSubmit: withoutServerErrors(field.state.meta.errorMap.onSubmit),
    });
    field.handleChange(next);
  }

  function add() {
    if (draft === "") return;
    const problem = check(draft, tags);
    if (problem !== undefined) {
      setRefused(problem);
      return;
    }
    setRefused(null);
    change({ tags: [...tags, draft], draft: "" });
  }

  return (
    <div className="flex flex-col gap-2">
      {/* No removal while the form submits: the tag group takes no disabled
          state, and without `onRemove` it draws no remove buttons. */}
      <TagGroup
        onRemove={
          submitting
            ? undefined
            : (keys) => {
                const removed = new Set(
                  [...keys].filter((key) => !lockedTags.includes(String(key))),
                );
                if (removed.size === 0) return;
                change({
                  tags: tags.filter((tag) => !removed.has(tag)),
                  draft,
                });
              }
        }
      >
        <Label className={isLabelHidden ? "sr-only" : undefined}>{label}</Label>
        {tags.length > 0 && (
          <TagGroup.List items={tags.map((tag) => ({ id: tag }))}>
            {(item) => (
              <Tag id={item.id} textValue={item.id} className="font-mono">
                {(renderProps) => (
                  <>
                    {item.id}
                    {renderProps.allowsRemoving &&
                      !lockedTags.includes(item.id) && (
                        <Tag.RemoveButton
                          aria-label={i18n._(removeTagMessage)}
                        />
                      )}
                  </>
                )}
              </Tag>
            )}
          </TagGroup.List>
        )}
      </TagGroup>

      <TextField
        className="w-full"
        name={field.name}
        value={draft}
        isDisabled={submitting}
        isInvalid={invalid}
        validationBehavior="aria"
        onChange={(next) => {
          setRefused(null);
          change({ tags, draft: next });
        }}
        onBlur={() => {
          if (!form.state.isSubmitting) field.handleBlur();
        }}
      >
        <Label className="sr-only">
          {i18n._({ ...draftLabelMessage, values: { label } })}
        </Label>
        <InputGroup variant="secondary">
          <InputGroup.Input
            className="font-mono"
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            placeholder={placeholder}
            aria-invalid={invalid || undefined}
            aria-describedby={
              [description !== undefined && descriptionId, invalid && errorId]
                .filter(Boolean)
                .join(" ") || undefined
            }
            onKeyDown={(event) => {
              // Enter adds the value; it must not submit the form around it.
              if (event.key === "Enter") {
                event.preventDefault();
                add();
              }
            }}
          />
          <InputGroup.Suffix className="pe-1">
            <Button
              size="sm"
              variant="ghost"
              isDisabled={submitting || draft === ""}
              onPress={add}
            >
              {addLabel}
            </Button>
          </InputGroup.Suffix>
        </InputGroup>
        {description !== undefined && (
          <Description id={descriptionId}>{description}</Description>
        )}
        {invalid && (
          <FieldError id={errorId}>
            <FormMessages errors={errors} />
          </FieldError>
        )}
      </TextField>
    </div>
  );
}
