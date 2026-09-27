import {
  Description,
  InputGroup,
  Label,
  TextField,
  Tooltip,
} from "@heroui/react";
import { useLingui } from "@lingui/react/macro";
import { Check, Copy } from "lucide-react";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

/** How long the check and its tooltip stay on screen after a copy. */
const COPIED_MS = 2000;

/**
 * A read-only field on a card whose value the reader will paste elsewhere — a
 * Client ID, an Entity ID, a callback address. It reads like its sibling
 * fields: same label, same surface `secondary` fill, same start padding, so
 * every caller draws it on a card.
 *
 * Clicking anywhere on the field selects the value and copies it; the icon at
 * the trailing edge is decoration that turns into a check with a brief
 * "Copied" tooltip. Keyboard focus only selects, so tabbing through a form
 * never overwrites the clipboard, and a copy by hand confirms the same way.
 * The value is monospace and never truncated: a long one scrolls inside the
 * field and is still copied whole. A refused clipboard shows a warning.
 */
export function CopyValue({
  value,
  label,
  description,
}: {
  value: string;
  label: ReactNode;
  description?: ReactNode;
}) {
  const { t } = useLingui();
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle");
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const inputRef = useRef<HTMLInputElement>(null);
  const iconRef = useRef<HTMLSpanElement>(null);

  useEffect(() => () => clearTimeout(timer.current), []);

  const confirm = () => {
    clearTimeout(timer.current);
    setState("copied");
    timer.current = setTimeout(() => setState("idle"), COPIED_MS);
  };

  const copy = () => {
    inputRef.current?.focus();
    inputRef.current?.select();
    navigator.clipboard.writeText(value).then(confirm, () => {
      clearTimeout(timer.current);
      setState("failed");
    });
  };

  const copied = state === "copied";
  const copiedLabel = t({ id: "copy-value.copied", message: "Copied" });

  return (
    <TextField isReadOnly value={value}>
      <Label>{label}</Label>
      <InputGroup
        variant="secondary"
        className="cursor-copy"
        // A press inside a selection collapses it after `click` has run, so
        // the press never reaches the browser and `copy` places focus itself.
        onMouseDown={(event) => event.preventDefault()}
        onClick={copy}
      >
        <InputGroup.Input
          ref={inputRef}
          className="cursor-copy font-mono"
          onFocus={(event) => event.target.select()}
          onCopy={confirm}
        />
        {/* The suffix and the icon's box are stretched to the field's height,
            so the tooltip hangs from the field's edge rather than overlapping
            it. HeroUI's `height: 100%` does not resolve against the group's
            `min-height`. */}
        <InputGroup.Suffix className="h-auto self-stretch">
          <span
            ref={iconRef}
            className="flex items-center self-stretch"
            aria-hidden="true"
          >
            {copied ? (
              <Check className="size-4 text-success" />
            ) : (
              <Copy className="size-4 text-muted" />
            )}
          </span>
          <Tooltip isOpen={copied}>
            <Tooltip.Content triggerRef={iconRef} placement="bottom">
              {copiedLabel}
            </Tooltip.Content>
          </Tooltip>
        </InputGroup.Suffix>
      </InputGroup>
      {description !== undefined && <Description>{description}</Description>}
      <span role="status" className="sr-only">
        {copied ? copiedLabel : ""}
      </span>
      {state === "failed" && (
        <SurfaceAlert status="warning" role="alert">
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>
              {t({
                id: "copy-value.failed",
                message:
                  "Couldn't copy. Select the value above and copy it yourself.",
              })}
            </SurfaceAlert.Title>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      )}
    </TextField>
  );
}
