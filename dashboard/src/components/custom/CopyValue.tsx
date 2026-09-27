import {
  Description,
  InputGroup,
  Label,
  TextField,
  Tooltip,
} from "@heroui/react";
import { useLingui } from "@lingui/react/macro";
import { Check, Copy } from "lucide-react";
import { type ReactNode, useEffect, useId, useRef, useState } from "react";
import { Button } from "@/components/custom/Button";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

/** How long the copied state stays on screen before the button resets. */
const COPIED_MS = 2000;

/**
 * A read-only field on a card whose value the reader will paste elsewhere — a
 * Client ID, an Entity ID, a callback address — with a copy button in place of
 * editing. It reads like its sibling fields: same label, same surface
 * `secondary` fill, same start padding, so every caller draws it on a card.
 * The button is inset from the trailing edge by the same 2px the field leaves
 * above and below it.
 *
 * The value is monospace and never truncated; a long one scrolls inside the
 * field and the button always copies the whole of it. A copy is confirmed by
 * the icon, the tooltip and a status message; a refused clipboard shows a
 * warning, and focusing the value selects it for copying by hand.
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
  const labelId = useId();
  const buttonId = useId();
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle");
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);

  useEffect(() => () => clearTimeout(timer.current), []);

  const copy = () => {
    clearTimeout(timer.current);
    navigator.clipboard.writeText(value).then(
      () => {
        setState("copied");
        timer.current = setTimeout(() => setState("idle"), COPIED_MS);
      },
      () => setState("failed"),
    );
  };

  const copied = state === "copied";
  const action = copied
    ? t({ id: "copy-value.copied", message: "Copied" })
    : t({ id: "copy-value.copy", message: "Copy" });

  return (
    <TextField isReadOnly value={value}>
      <Label id={labelId}>{label}</Label>
      <InputGroup variant="secondary">
        <InputGroup.Input
          className="font-mono"
          onFocus={(event) => event.target.select()}
        />
        <InputGroup.Suffix className="pe-0.5">
          <Tooltip delay={0}>
            <Button
              id={buttonId}
              isIconOnly
              size="sm"
              variant="ghost"
              aria-label={action}
              aria-labelledby={`${buttonId} ${labelId}`}
              onPress={copy}
            >
              {copied ? (
                <Check className="size-4" aria-hidden="true" />
              ) : (
                <Copy className="size-4" aria-hidden="true" />
              )}
            </Button>
            <Tooltip.Content>{action}</Tooltip.Content>
          </Tooltip>
        </InputGroup.Suffix>
      </InputGroup>
      {description !== undefined && <Description>{description}</Description>}
      <span role="status" className="sr-only">
        {copied ? action : ""}
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
