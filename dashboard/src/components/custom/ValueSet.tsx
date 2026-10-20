import { Trans, useLingui } from "@lingui/react/macro";
import { PlusIcon, XIcon } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/custom/Button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverPopup, PopoverTrigger } from "@/components/ui/popover";

interface ValueSetProps {
  values: string[];
  onRemove: (value: string) => void;
  /** Called with the trimmed draft. */
  onAdd: (value: string) => void;
  /** "Add scope", "Add domain". */
  addLabel: string;
  placeholder: string;
  /** Shown when `values` is empty. */
  emptyText?: string;
  /** Only when the source publishes them. */
  suggestions?: { value: string; description: string }[];
  /** Shown in the popover when `suggestions` is undefined. */
  noSuggestionsNote?: string;
  /** A message shown under the popover input; it blocks the add. */
  validate?: (draft: string) => string | undefined;
  /** Values drawn without a remove button. */
  lockedValues?: readonly string[];
  /** Hold the input's text in the caller, so a submit can see it; `onAdd` then clears it. */
  draft?: string;
  onDraftChange?: (draft: string) => void;
  isDisabled?: boolean;
}

/**
 * A short persisted set of identifiers, drawn as raised value chips. Adding is
 * a deliberate step in a popover, so a stray keystroke never edits the set.
 */
export function ValueSet({
  values,
  onRemove,
  onAdd,
  addLabel,
  placeholder,
  emptyText,
  suggestions,
  noSuggestionsNote,
  validate,
  lockedValues = [],
  draft: controlledDraft,
  onDraftChange,
  isDisabled = false,
}: ValueSetProps): React.ReactElement {
  const { i18n } = useLingui();
  const [ownDraft, setOwnDraft] = useState("");
  const [problem, setProblem] = useState<string>();
  const [open, setOpen] = useState(false);
  const draft = controlledDraft ?? ownDraft;
  const setDraft = (next: string) => {
    setProblem(undefined);
    if (onDraftChange === undefined) setOwnDraft(next);
    else onDraftChange(next);
  };

  // Returns whether the value joined the set.
  const add = (raw: string, fromDraft: boolean) => {
    const value = raw.trim();
    if (value === "") return;
    const message = validate?.(value);
    if (message !== undefined) {
      setProblem(message);
      return;
    }
    onAdd(value);
    // A caller holding the draft clears it in the same update as the add.
    if (fromDraft && onDraftChange === undefined) setOwnDraft("");
  };

  const offered = suggestions?.filter(
    (suggestion) => !values.includes(suggestion.value),
  );

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {values.length === 0 && emptyText !== undefined && (
        <span className="me-1 text-muted-foreground text-sm">{emptyText}</span>
      )}
      {values.map((value) => (
        <span
          key={value}
          className="relative inline-flex h-7 items-center gap-1 rounded-md border bg-background ps-2.5 pe-1 font-mono text-[13px] shadow-xs/5 before:pointer-events-none before:absolute before:inset-0 before:rounded-[calc(var(--radius-md)-1px)] before:shadow-[0_1px_--theme(--color-black/4%)] dark:bg-input/32"
        >
          {value}
          {!lockedValues.includes(value) && (
            <button
              type="button"
              disabled={isDisabled}
              aria-label={i18n._({
                id: "value-set.remove",
                message: "Remove {value}",
                values: { value },
              })}
              onClick={() => onRemove(value)}
              className="flex size-5 cursor-pointer items-center justify-center rounded text-muted-foreground opacity-56 outline-none hover:bg-accent hover:text-foreground hover:opacity-100 focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none"
            >
              <XIcon aria-hidden="true" className="size-3.5" />
            </button>
          )}
        </span>
      ))}
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          disabled={isDisabled}
          className="inline-flex h-7 cursor-pointer items-center gap-1 rounded-md border border-dashed px-2.5 text-muted-foreground text-sm outline-none hover:border-solid hover:bg-accent/50 hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-64 data-popup-open:border-solid data-popup-open:bg-accent/50 data-popup-open:text-foreground"
        >
          <PlusIcon aria-hidden="true" className="size-3.5" />
          {addLabel}
        </PopoverTrigger>
        <PopoverPopup align="end" side="bottom" className="w-80 p-0">
          <div className="flex flex-col">
            <div className="flex gap-1.5 p-2">
              <Input
                size="sm"
                className="font-mono"
                autoComplete="off"
                autoCapitalize="off"
                spellCheck={false}
                autoFocus
                aria-label={addLabel}
                aria-invalid={problem !== undefined || undefined}
                placeholder={placeholder}
                value={draft}
                onChange={(event) => setDraft(event.target.value)}
                onKeyDown={(event) => {
                  // Enter adds; it must not submit a form around the popover.
                  if (event.key === "Enter") {
                    event.preventDefault();
                    add(draft, true);
                  }
                }}
              />
              <Button
                disabled={draft.trim() === ""}
                onClick={() => add(draft, true)}
                size="sm"
              >
                <Trans id="form.tags.add">Add</Trans>
              </Button>
            </div>
            {problem !== undefined && (
              <p
                role="alert"
                className="px-3 pb-2 text-destructive-foreground text-caption"
              >
                {problem}
              </p>
            )}
            {suggestions === undefined && noSuggestionsNote !== undefined && (
              <p className="border-t px-3 py-2.5 text-muted-foreground text-caption">
                {noSuggestionsNote}
              </p>
            )}
            {offered !== undefined && offered.length > 0 && (
              <div className="border-t p-1">
                <div className="px-2 pt-1.5 pb-1 text-muted-foreground text-xs">
                  <Trans id="value-set.offered">Offered by this provider</Trans>
                </div>
                {offered.map((suggestion) => (
                  <button
                    key={suggestion.value}
                    type="button"
                    onClick={() => add(suggestion.value, false)}
                    className="flex w-full cursor-pointer items-baseline gap-2.5 rounded-md px-2 py-1.5 text-start outline-none hover:bg-accent focus-visible:bg-accent"
                  >
                    <span className="font-mono text-[13px]">
                      {suggestion.value}
                    </span>
                    <span className="truncate text-muted-foreground text-xs">
                      {suggestion.description}
                    </span>
                  </button>
                ))}
              </div>
            )}
          </div>
        </PopoverPopup>
      </Popover>
    </div>
  );
}
