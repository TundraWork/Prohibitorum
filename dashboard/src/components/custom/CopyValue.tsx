import { useLingui } from "@lingui/react/macro";
import { CheckIcon, CircleAlertIcon, CopyIcon } from "lucide-react";
import { type ReactNode, useRef } from "react";
import { Button } from "@/components/custom/Button";
import { useCopyValue } from "@/components/custom/use-copy-value";
import { Alert, AlertTitle } from "@/components/ui/alert";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  InputGroupTextarea,
} from "@/components/ui/input-group";

/**
 * A read-only field whose value the reader will paste elsewhere — a Client ID,
 * an Entity ID, a callback address. The button at the trailing edge copies the
 * whole value and turns into a check named "Copied" for a moment.
 *
 * Focusing the field only selects the value, so tabbing through a form never
 * overwrites the clipboard, and a copy by hand confirms the same way. The
 * value is monospace and never truncated: a long one scrolls inside the field
 * (or wraps, with `wrap`) and is still copied whole. A refused clipboard shows a warning.
 */
export function CopyValue({
  value,
  label,
  description,
  wrap = false,
}: {
  value: string;
  label: ReactNode;
  description?: ReactNode;
  /**
   * Breaks a long value across lines instead of scrolling it, for one the
   * reader may have to read out and type by hand, such as an authenticator's
   * setup key.
   */
  wrap?: boolean;
}) {
  const { t } = useLingui();
  const { state, copy: copyValue, confirm } = useCopyValue();
  const inputRef = useRef<HTMLInputElement & HTMLTextAreaElement>(null);

  const copy = () => {
    inputRef.current?.select();
    copyValue(value);
  };

  const copied = state === "copied";
  const copiedLabel = t({ id: "copy-value.copied", message: "Copied" });

  return (
    <Field>
      <FieldLabel>{label}</FieldLabel>
      <InputGroup
        className={
          wrap
            ? "**:[textarea]:min-h-0 **:[textarea]:max-sm:min-h-0"
            : undefined
        }
      >
        {wrap ? (
          <InputGroupTextarea
            ref={inputRef}
            readOnly
            rows={2}
            value={value}
            className="break-all font-mono"
            onFocus={(event) => event.target.select()}
            onCopy={confirm}
          />
        ) : (
          <InputGroupInput
            ref={inputRef}
            readOnly
            value={value}
            className="font-mono"
            onFocus={(event) => event.target.select()}
            onCopy={confirm}
          />
        )}
        <InputGroupAddon align="inline-end">
          <Button
            aria-label={
              copied
                ? copiedLabel
                : t({ id: "copy-value.copy", message: "Copy" })
            }
            onClick={copy}
            variant="ghost"
            size="icon-sm"
          >
            {copied ? (
              <CheckIcon aria-hidden="true" className="text-success" />
            ) : (
              <CopyIcon aria-hidden="true" />
            )}
          </Button>
        </InputGroupAddon>
      </InputGroup>
      {description !== undefined && (
        <FieldDescription>{description}</FieldDescription>
      )}
      <span role="status" className="sr-only">
        {copied ? copiedLabel : ""}
      </span>
      {state === "failed" && (
        <Alert variant="warning">
          <CircleAlertIcon aria-hidden="true" />
          <AlertTitle>
            {t({
              id: "copy-value.failed",
              message:
                "Couldn't copy. Select the value above and copy it yourself.",
            })}
          </AlertTitle>
        </Alert>
      )}
    </Field>
  );
}
