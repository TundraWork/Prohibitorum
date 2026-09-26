import { Description, InputGroup } from "@heroui/react";
import { useLingui } from "@lingui/react/macro";
import { Check, ClipboardCopy } from "lucide-react";
import { type ReactNode, useState } from "react";
import { Button } from "@/components/custom/Button";

/**
 * A read-only value the reader will need elsewhere — a Client ID, an Entity ID,
 * a callback address — with a button that puts it on the clipboard.
 *
 * The value is drawn in the monospace face and never truncated: these strings
 * are meant to be compared character by character and pasted elsewhere, so a
 * middle ellipsis would defeat the point. It wraps instead, and the copy button
 * sits at the trailing edge where HeroUI keeps a group's suffix.
 *
 * The copy outcome is announced on the button itself: a clipboard refusal is
 * common enough (an insecure origin, a denied permission) that it has to say so
 * rather than silently doing nothing, and the button is where the reader's
 * attention already is.
 */
export function CopyValue({
  value,
  label,
  description,
  copyLabel,
}: {
  value: string;
  /** Names the field for assistive technology and for the copy button. */
  label: ReactNode;
  description?: ReactNode;
  /** Overrides the button's accessible name; falls back to `label`. */
  copyLabel?: ReactNode;
}) {
  const { t } = useLingui();
  const [copied, setCopied] = useState(false);

  return (
    <div className="flex flex-col gap-1.5">
      <InputGroup>
        <InputGroup.Prefix className="sr-only">{label}</InputGroup.Prefix>
        <InputGroup.Input
          readOnly
          value={value}
          aria-label={
            typeof label === "string"
              ? label
              : t({ id: "copy-value.field", message: "Value" })
          }
          className="font-mono text-sm"
        />
        <InputGroup.Suffix>
          <Button
            isIconOnly
            size="sm"
            variant="ghost"
            aria-label={
              copied
                ? t({ id: "copy-value.copied", message: "Copied" })
                : typeof copyLabel === "string"
                  ? copyLabel
                  : t({ id: "copy-value.copy", message: "Copy" })
            }
            onPress={() => {
              void navigator.clipboard.writeText(value).then(
                () => setCopied(true),
                () => setCopied(false),
              );
            }}
          >
            {copied ? (
              <Check size={14} aria-hidden="true" />
            ) : (
              <ClipboardCopy size={14} aria-hidden="true" />
            )}
          </Button>
        </InputGroup.Suffix>
      </InputGroup>
      {description !== undefined && <Description>{description}</Description>}
    </div>
  );
}
